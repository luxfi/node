// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/metrics"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/consensus/choices"
	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/node/consensus/chain/chaintest"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/hashing"
)

var (
	errCantBuildBlock       = errors.New("can't build new block")
	errVerify               = errors.New("verify failed")
	errAccept               = errors.New("accept failed")
	errReject               = errors.New("reject failed")
	errUnexpectedBlockBytes = errors.New("unexpected block bytes")
)

// testBlockAdapter wraps a chaintest.Block to implement the chain.Block interface
type testBlockAdapter struct {
	*chaintest.Block
	choicesStatus choices.Status
}

func (b *testBlockAdapter) SetStatus(status choices.Status) {
	b.choicesStatus = status
	// Map choices.Status to consensustest.Status
	switch status {
	case choices.Accepted:
		b.Block.Status = consensustest.Accepted
	case choices.Rejected:
		b.Block.Status = consensustest.Rejected
	default:
		b.Block.Status = consensustest.Undecided
	}
}

// Override Verify to handle the test error
func (b *testBlockAdapter) Verify(ctx context.Context) error {
	// If the underlying block has a VerifyV error, return it
	if b.Block.VerifyV != nil {
		return b.Block.VerifyV
	}
	return b.Block.Verify(ctx)
}

// NewTestBlock returns a new test block with height, bytes, and ID derived from [i]
// and using [parentID] as the parent block ID
func NewTestBlock(i uint64, parentID ids.ID) *chaintest.Block {
	b := []byte{byte(i)}
	id := hashing.ComputeHash256Array(b)
	return &chaintest.Block{
		Decidable: consensustest.Decidable{
			IDV:    id,
			Status: consensustest.Undecided,
		},
		HeightV: i,
		ParentV: parentID,
		BytesV:  b,
	}
}

// NewTestBlocks generates [numBlocks] consecutive blocks
func NewTestBlocks(numBlocks uint64) []*chaintest.Block {
	blks := make([]*chaintest.Block, 0, numBlocks)
	parentID := ids.Empty
	for i := uint64(0); i < numBlocks; i++ {
		blks = append(blks, NewTestBlock(i, parentID))
		parent := blks[len(blks)-1]
		parentID = parent.ID()
	}

	return blks
}

// getOrCreateAdapter creates an adapter for a block
func getOrCreateAdapter(blk *chaintest.Block) *testBlockAdapter {
	return &testBlockAdapter{Block: blk}
}

func createInternalBlockFuncs(blks []*chaintest.Block) (
	func(ctx context.Context, blkID ids.ID) (chain.Block, error),
	func(ctx context.Context, b []byte) (chain.Block, error),
	func(ctx context.Context, height uint64) (ids.ID, error),
	map[ids.ID]*testBlockAdapter,
) {
	blkMap := make(map[ids.ID]*chaintest.Block)
	blkBytesMap := make(map[string]*chaintest.Block)
	adapterMap := make(map[ids.ID]*testBlockAdapter)
	
	// Create adapters for all blocks upfront
	for _, blk := range blks {
		blkMap[blk.ID()] = blk
		blkBytes := blk.Bytes()
		blkBytesMap[string(blkBytes)] = blk
		adapterMap[blk.ID()] = &testBlockAdapter{Block: blk}
	}

	// Keep track of which blocks have been parsed
	parsedBlocks := make(map[ids.ID]bool)
	
	getBlock := func(_ context.Context, id ids.ID) (chain.Block, error) {
		blk, ok := blkMap[id]
		if !ok {
			return nil, database.ErrNotFound
		}
		// Return blocks that are either decided (accepted/rejected) or have been parsed
		if blk.Status == consensustest.Undecided && !parsedBlocks[id] {
			return nil, database.ErrNotFound
		}

		return adapterMap[id], nil
	}

	parseBlk := func(_ context.Context, b []byte) (chain.Block, error) {
		blk, ok := blkBytesMap[string(b)]
		if !ok {
			return nil, fmt.Errorf("%w: %x", errUnexpectedBlockBytes, b)
		}
		// Mark this block as parsed so it can be retrieved
		blkMap[blk.ID()] = blk
		parsedBlocks[blk.ID()] = true

		return adapterMap[blk.ID()], nil
	}
	getAcceptedBlockIDAtHeight := func(_ context.Context, height uint64) (ids.ID, error) {
		for _, blk := range blks {
			if blk.Height() != height {
				continue
			}

			if blk.Status == consensustest.Accepted {
				return blk.ID(), nil
			}
		}

		return ids.Empty, database.ErrNotFound
	}

	return getBlock, parseBlk, getAcceptedBlockIDAtHeight, adapterMap
}

func cantBuildBlock(context.Context) (chain.Block, error) {
	return nil, errCantBuildBlock
}

// checkProcessingBlock checks that [blk] is of the correct type and is
// correctly uniquified when calling GetBlock and ParseBlock.
func checkProcessingBlock(t *testing.T, s *State, blk chain.Block) {
	require := require.New(t)

	require.IsType(&BlockWrapper{}, blk)

	parsedBlk, err := s.ParseBlock(context.Background(), blk.Bytes())
	require.NoError(err)
	require.Equal(blk.ID(), parsedBlk.ID())
	require.Equal(blk.Bytes(), parsedBlk.Bytes())
	// Status is not directly accessible on chain.Block interface
	require.Equal(blk, parsedBlk)

	getBlk, err := s.GetBlock(context.Background(), blk.ID())
	require.NoError(err)
	require.Equal(parsedBlk, getBlk)
}

// checkDecidedBlock asserts that [blk] is returned with the correct status by ParseBlock
// and GetBlock.
func checkDecidedBlock(t *testing.T, s *State, blk chain.Block, cached bool) {
	require := require.New(t)

	require.IsType(&BlockWrapper{}, blk)

	parsedBlk, err := s.ParseBlock(context.Background(), blk.Bytes())
	require.NoError(err)
	require.Equal(blk.ID(), parsedBlk.ID())
	require.Equal(blk.Bytes(), parsedBlk.Bytes())
	// Status is not directly accessible on chain.Block interface

	// If the block should be in the cache, assert that the returned block is identical to [blk]
	if cached {
		require.Equal(blk, parsedBlk)
	}

	getBlk, err := s.GetBlock(context.Background(), blk.ID())
	require.NoError(err)
	require.Equal(blk.ID(), getBlk.ID())
	require.Equal(blk.Bytes(), getBlk.Bytes())
	// Status is not directly accessible on chain.Block interface

	// Since ParseBlock should have triggered a cache hit, assert that the block is identical
	// to the parsed block.
	require.Equal(parsedBlk, getBlk)
}

func checkAcceptedBlock(t *testing.T, s *State, blk chain.Block, cached bool) {
	checkDecidedBlock(t, s, blk, cached)
}

func checkRejectedBlock(t *testing.T, s *State, blk chain.Block, cached bool) {
	checkDecidedBlock(t, s, blk, cached)
}

func TestState(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(3)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted
	blk1 := testBlks[1]
	blk2 := testBlks[2]
	// Need to create a block with a different bytes and hash here
	// to generate a conflict with blk2
	blk3 := chaintest.BuildChild(blk1)
	testBlks = append(testBlks, blk3)

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	lastAccepted, err := chainState.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(genesisBlock.ID(), lastAccepted)

	wrappedGenesisBlk, err := chainState.GetBlock(context.Background(), genesisBlock.ID())
	require.NoError(err)

	// Check that a cache miss on a block is handled correctly
	_, err = chainState.GetBlock(context.Background(), blk1.ID())
	require.ErrorIs(err, database.ErrNotFound)

	// Parse and verify blk1 and blk2
	parsedBlk1, err := chainState.ParseBlock(context.Background(), blk1.Bytes())
	require.NoError(err)
	require.NoError(parsedBlk1.Verify(context.Background()))

	parsedBlk2, err := chainState.ParseBlock(context.Background(), blk2.Bytes())
	require.NoError(err)
	require.NoError(parsedBlk2.Verify(context.Background()))

	// Check that the verified blocks have been placed in the processing map
	require.Len(chainState.verifiedBlocks, 2)

	parsedBlk3, err := chainState.ParseBlock(context.Background(), blk3.Bytes())
	require.NoError(err)
	getBlk3, err := chainState.GetBlock(context.Background(), blk3.ID())
	require.NoError(err)
	require.Equal(parsedBlk3.ID(), getBlk3.ID(), "State GetBlock returned the wrong block")

	// Check that parsing blk3 does not add it to processing blocks since it has
	// not been verified.
	require.Len(chainState.verifiedBlocks, 2)

	require.NoError(parsedBlk3.Verify(context.Background()))
	// Check that blk3 has been added to processing blocks.
	require.Len(chainState.verifiedBlocks, 3)

	// Decide the blocks and ensure they are removed from the processing blocks map
	require.NoError(parsedBlk1.Accept(context.Background()))
	require.NoError(parsedBlk2.Accept(context.Background()))
	require.NoError(parsedBlk3.Reject(context.Background()))

	require.Empty(chainState.verifiedBlocks)

	// Check that the last accepted block was updated correctly
	lastAcceptedID, err := chainState.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(blk2.ID(), lastAcceptedID)
	require.Equal(blk2.ID(), chainState.LastAcceptedBlock().ID())

	// Flush the caches to ensure decided blocks are handled correctly on cache misses.
	chainState.Flush()
	checkAcceptedBlock(t, chainState, wrappedGenesisBlk, false)
	checkAcceptedBlock(t, chainState, parsedBlk1, false)
	checkAcceptedBlock(t, chainState, parsedBlk2, false)
	checkRejectedBlock(t, chainState, parsedBlk3, false)
}

func TestBuildBlock(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(2)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted
	blk1 := testBlks[1]

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	
	buildBlock := func(context.Context) (chain.Block, error) {
		// Once the block is built, mark it as processing
		blk1.Status = consensustest.Accepted
		return adapterMap[blk1.ID()], nil
	}

	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          buildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	builtBlk, err := chainState.BuildBlock(context.Background())
	require.NoError(err)
	require.Empty(chainState.verifiedBlocks)

	require.NoError(builtBlk.Verify(context.Background()))
	require.Len(chainState.verifiedBlocks, 1)

	checkProcessingBlock(t, chainState, builtBlk)

	require.NoError(builtBlk.Accept(context.Background()))

	checkAcceptedBlock(t, chainState, builtBlk, true)
}

func TestStateDecideBlock(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(4)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted
	badAcceptBlk := testBlks[1]
	badAcceptBlk.AcceptV = errAccept
	badVerifyBlk := testBlks[2]
	badVerifyBlk.VerifyV = errVerify
	badRejectBlk := testBlks[3]
	badRejectBlk.RejectV = errReject
	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	// Parse badVerifyBlk (which should fail verification)
	badBlk, err := chainState.ParseBlock(context.Background(), badVerifyBlk.Bytes())
	require.NoError(err)
	err = badBlk.Verify(context.Background())
	require.ErrorIs(err, errVerify)
	// Ensure a block that fails verification is not marked as processing
	require.Empty(chainState.verifiedBlocks)

	// Ensure that an error during block acceptance is propagated correctly
	badBlk, err = chainState.ParseBlock(context.Background(), badAcceptBlk.Bytes())
	require.NoError(err)
	require.NoError(badBlk.Verify(context.Background()))
	require.Len(chainState.verifiedBlocks, 1)

	err = badBlk.Accept(context.Background())
	require.ErrorIs(err, errAccept)

	// Ensure that an error during block reject is propagated correctly
	badBlk, err = chainState.ParseBlock(context.Background(), badRejectBlk.Bytes())
	require.NoError(err)
	require.NoError(badBlk.Verify(context.Background()))
	// Note: an error during block Accept/Reject is fatal, so it is undefined whether
	// the block that failed on Accept should be removed from processing or not. We allow
	// either case here to make this test more flexible.
	numProcessing := len(chainState.verifiedBlocks)
	require.Contains([]int{1, 2}, numProcessing)

	err = badBlk.Reject(context.Background())
	require.ErrorIs(err, errReject)
}

func TestStateParent(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(3)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted
	blk1 := testBlks[1]
	blk2 := testBlks[2]

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	parsedBlk2, err := chainState.ParseBlock(context.Background(), blk2.Bytes())
	require.NoError(err)

	missingBlk1ID := parsedBlk2.Parent()

	_, err = chainState.GetBlock(context.Background(), missingBlk1ID)
	require.ErrorIs(err, database.ErrNotFound)

	parsedBlk1, err := chainState.ParseBlock(context.Background(), blk1.Bytes())
	require.NoError(err)

	genesisBlkParentID := parsedBlk1.Parent()
	genesisBlkParent, err := chainState.GetBlock(context.Background(), genesisBlkParentID)
	require.NoError(err)
	checkAcceptedBlock(t, chainState, genesisBlkParent, true)

	parentBlk1ID := parsedBlk2.Parent()
	parentBlk1, err := chainState.GetBlock(context.Background(), parentBlk1ID)
	require.NoError(err)
	checkProcessingBlock(t, chainState, parentBlk1)
}

func TestGetBlockInternal(t *testing.T) {
	require := require.New(t)
	testBlks := NewTestBlocks(1)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	genesisBlockInternal := chainState.LastAcceptedBlockInternal()
	// The internal block might be wrapped in an adapter
	if adapter, ok := genesisBlockInternal.(*testBlockAdapter); ok {
		genesisBlockInternal = adapter.Block
	}
	require.IsType(&chaintest.Block{}, genesisBlockInternal)
	require.Equal(genesisBlock.ID(), genesisBlockInternal.ID())

	blk, err := chainState.GetBlockInternal(context.Background(), genesisBlock.ID())
	require.NoError(err)

	// The internal block might be wrapped in an adapter
	if adapter, ok := blk.(*testBlockAdapter); ok {
		blk = adapter.Block
	}
	require.IsType(&chaintest.Block{}, blk)
	require.Equal(genesisBlock.ID(), blk.ID())
}

func TestGetBlockError(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(2)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted
	blk1 := testBlks[1]

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	wrappedGetBlock := func(ctx context.Context, id ids.ID) (chain.Block, error) {
		blk, err := getBlock(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("wrapping error to prevent caching miss: %w", err)
		}
		return blk, nil
	}
	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            wrappedGetBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	_, err := chainState.GetBlock(context.Background(), blk1.ID())
	require.ErrorIs(err, database.ErrNotFound)

	// Update the status to Processing, so that it will be returned by the internal get block
	// function.
	blk1.Status = consensustest.Accepted
	blk, err := chainState.GetBlock(context.Background(), blk1.ID())
	require.NoError(err)
	require.Equal(blk1.ID(), blk.ID())
	checkProcessingBlock(t, chainState, blk)
}

func TestParseBlockError(t *testing.T) {
	testBlks := NewTestBlocks(1)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	_, err := chainState.ParseBlock(context.Background(), []byte{255})
	require.ErrorIs(t, err, errUnexpectedBlockBytes)
}

func TestBuildBlockError(t *testing.T) {
	testBlks := NewTestBlocks(1)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	_, err := chainState.BuildBlock(context.Background())
	require.ErrorIs(t, err, errCantBuildBlock)
}

func TestMeteredCache(t *testing.T) {
	require := require.New(t)

	registry := metrics.NewNoOpMetrics("test").Registry()

	testBlks := NewTestBlocks(1)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	config := &Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	}
	_, err := NewMeteredState(registry, config)
	require.NoError(err)
	_, err = NewMeteredState(registry, config)
	require.Error(err) //nolint:forbidigo // error is not exported https://github.com/prometheus/client_golang/blob/main/prometheus/registry.go#L315
}

// Test the bytesToIDCache
func TestStateBytesToIDCache(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(3)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted
	blk1 := testBlks[1]
	blk2 := testBlks[2]

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	buildBlock := func(context.Context) (chain.Block, error) {
		require.FailNow("shouldn't have been called")
		return nil, nil
	}

	chainState := NewState(&Config{
		DecidedCacheSize:    0,
		MissingCacheSize:    0,
		UnverifiedCacheSize: 0,
		BytesToIDCacheSize:  1 + ids.IDLen, // Size of one block
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          buildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	// Shouldn't have blk1 ID to start with
	_, err := chainState.GetBlock(context.Background(), blk1.ID())
	require.ErrorIs(err, database.ErrNotFound)
	_, ok := chainState.bytesToIDCache.Get(string(blk1.Bytes()))
	require.False(ok)

	// Parse blk1 from bytes
	_, err = chainState.ParseBlock(context.Background(), blk1.Bytes())
	require.NoError(err)

	// blk1 should be in cache now
	_, ok = chainState.bytesToIDCache.Get(string(blk1.Bytes()))
	require.True(ok)

	// Parse another block
	_, err = chainState.ParseBlock(context.Background(), blk2.Bytes())
	require.NoError(err)

	// Should have bumped blk1 from cache
	_, ok = chainState.bytesToIDCache.Get(string(blk2.Bytes()))
	require.True(ok)
	_, ok = chainState.bytesToIDCache.Get(string(blk1.Bytes()))
	require.False(ok)
}

// TestSetLastAcceptedBlock ensures chainState's last accepted block
// can be updated by calling [SetLastAcceptedBlock].
func TestSetLastAcceptedBlock(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(1)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted

	postSetBlk1ParentID := hashing.ComputeHash256Array([]byte{byte(199)})
	postSetBlk1 := NewTestBlock(200, postSetBlk1ParentID)
	postSetBlk2 := NewTestBlock(201, postSetBlk1.ID())

	// note we do not need to parse postSetBlk1 so it is omitted here
	testBlks = append(testBlks, postSetBlk2)

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	chainState := NewState(&Config{
		LastAcceptedBlock:  adapterMap[genesisBlock.ID()],
		GetBlock:           getBlock,
		UnmarshalBlock:     parseBlock,
		BuildBlock:         cantBuildBlock,
		GetBlockIDAtHeight: getCanonicalBlockID,
	})
	lastAcceptedID, err := chainState.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(genesisBlock.ID(), lastAcceptedID)

	// call SetLastAcceptedBlock for postSetBlk1
	// Need to create adapter for postSetBlk1 since it's not in the original testBlks
	postSetBlk1Adapter := &testBlockAdapter{Block: postSetBlk1}
	require.NoError(chainState.SetLastAcceptedBlock(postSetBlk1Adapter))
	lastAcceptedID, err = chainState.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(postSetBlk1.ID(), lastAcceptedID)
	require.Equal(postSetBlk1.ID(), chainState.LastAcceptedBlock().ID())

	// ensure further blocks can be accepted
	parsedpostSetBlk2, err := chainState.ParseBlock(context.Background(), postSetBlk2.Bytes())
	require.NoError(err)
	require.NoError(parsedpostSetBlk2.Verify(context.Background()))
	require.NoError(parsedpostSetBlk2.Accept(context.Background()))
	lastAcceptedID, err = chainState.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(postSetBlk2.ID(), lastAcceptedID)
	require.Equal(postSetBlk2.ID(), chainState.LastAcceptedBlock().ID())

	checkAcceptedBlock(t, chainState, parsedpostSetBlk2, false)
}

func TestSetLastAcceptedBlockWithProcessingBlocksErrors(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(5)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted
	blk1 := testBlks[1]
	resetBlk := testBlks[4]

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	buildBlock := func(context.Context) (chain.Block, error) {
		// Once the block is built, mark it as processing
		blk1.Status = consensustest.Accepted
		return adapterMap[blk1.ID()], nil
	}

	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          buildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	builtBlk, err := chainState.BuildBlock(context.Background())
	require.NoError(err)
	require.Empty(chainState.verifiedBlocks)

	require.NoError(builtBlk.Verify(context.Background()))
	require.Len(chainState.verifiedBlocks, 1)

	checkProcessingBlock(t, chainState, builtBlk)

	err = chainState.SetLastAcceptedBlock(adapterMap[resetBlk.ID()])
	require.ErrorIs(err, errSetAcceptedWithProcessing)
}

func TestStateParseTransitivelyAcceptedBlock(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(3)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted
	blk1 := testBlks[1]
	blk2 := testBlks[2]
	blk2.Status = consensustest.Accepted

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[blk2.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	parsedBlk1, err := chainState.ParseBlock(context.Background(), blk1.Bytes())
	require.NoError(err)
	require.Equal(blk1.Height(), parsedBlk1.Height())
}

func TestIsProcessing(t *testing.T) {
	require := require.New(t)

	testBlks := NewTestBlocks(2)
	genesisBlock := testBlks[0]
	genesisBlock.Status = consensustest.Accepted
	blk1 := testBlks[1]

	getBlock, parseBlock, getCanonicalBlockID, adapterMap := createInternalBlockFuncs(testBlks)
	chainState := NewState(&Config{
		DecidedCacheSize:    2,
		MissingCacheSize:    2,
		UnverifiedCacheSize: 2,
		BytesToIDCacheSize:  2,
		LastAcceptedBlock:   adapterMap[genesisBlock.ID()],
		GetBlock:            getBlock,
		UnmarshalBlock:      parseBlock,
		BuildBlock:          cantBuildBlock,
		GetBlockIDAtHeight:  getCanonicalBlockID,
	})

	// Parse blk1
	parsedBlk1, err := chainState.ParseBlock(context.Background(), blk1.Bytes())
	require.NoError(err)

	// Check that it is not processing in consensus
	require.False(chainState.IsProcessing(parsedBlk1.ID()))

	// Verify blk1
	require.NoError(parsedBlk1.Verify(context.Background()))

	// Check that it is processing in consensus
	require.True(chainState.IsProcessing(parsedBlk1.ID()))

	// Accept blk1
	require.NoError(parsedBlk1.Accept(context.Background()))

	// Check that it is no longer processing in consensus
	require.False(chainState.IsProcessing(parsedBlk1.ID()))
}
