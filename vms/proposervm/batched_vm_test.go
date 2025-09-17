// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/interfaces"
	"github.com/luxfi/metric"

	"github.com/luxfi/consensus/consensustest"

	"github.com/luxfi/consensus/engine/chain/block"

	"github.com/luxfi/node/vms/components/chain/blocktest"

	"github.com/luxfi/consensus/validators"

	"github.com/luxfi/consensus/validators/validatorstest"

	"github.com/luxfi/database"

	"github.com/luxfi/database/memdb"

	"github.com/luxfi/database/prefixdb"

	"github.com/luxfi/ids"

	"github.com/luxfi/consensus/core"

	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/vms/components/chain"
)

// testChainBlockAdapter adapts block.Block to chain.Block for test compatibility
type testChainBlockAdapter struct {
	block.Block
}

func (tcba *testChainBlockAdapter) SetStatus(status choices.Status) {
	// For testing, we don't need to actually set status
	// This method is required by the chain.Block interface
}

func adaptToChainBlock(b block.Block) chain.Block {
	return &testChainBlockAdapter{Block: b}
}

// testValidatorStateAdapter adapts validators.State to consensus.ValidatorState
type testValidatorStateAdapter struct {
	valState *validatorstest.State
}

func (t *testValidatorStateAdapter) GetChainID(chainID ids.ID) (ids.ID, error) {
	if t.valState.GetChainIDF != nil {
		return t.valState.GetChainIDF(chainID)
	}
	return chainID, nil
}

func (t *testValidatorStateAdapter) GetNetID(chainID ids.ID) (ids.ID, error) {
	if t.valState.GetNetIDF != nil {
		return t.valState.GetNetIDF(chainID)
	}
	return ids.Empty, nil
}

func (t *testValidatorStateAdapter) GetSubnetID(chainID ids.ID) (ids.ID, error) {
	if t.valState.GetSubnetIDF != nil {
		return t.valState.GetSubnetIDF(chainID)
	}
	return ids.Empty, nil
}

func (t *testValidatorStateAdapter) GetValidatorSet(height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	if t.valState.GetValidatorSetSimpleF != nil {
		return t.valState.GetValidatorSetSimpleF(height, netID)
	}
	return nil, nil
}

func (t *testValidatorStateAdapter) GetCurrentHeight() (uint64, error) {
	if t.valState.GetCurrentHeightF != nil {
		return t.valState.GetCurrentHeightF(context.Background())
	}
	return 0, nil
}

func (t *testValidatorStateAdapter) GetMinimumHeight(ctx context.Context) (uint64, error) {
	if t.valState.GetMinimumHeightF != nil {
		return t.valState.GetMinimumHeightF(ctx)
	}
	return 0, nil
}

func TestCoreVMNotRemote(t *testing.T) {
	// if coreVM is not remote VM, a specific error is returned
	require := require.New(t)
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	_, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	blkID := ids.Empty
	maxBlocksNum := 1000               // a high value to get all built blocks
	maxBlocksSize := 1000000           // a high value to get all built blocks
	maxBlocksRetrivalTime := time.Hour // a high value to get all built blocks
	_, err := proVM.GetAncestors(
		context.Background(),
		blkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.ErrorIs(err, block.ErrRemoteVMNotImplemented)

	var blks [][]byte
	_, err = proVM.BatchedParseBlock(context.Background(), blks)
	require.ErrorIs(err, block.ErrRemoteVMNotImplemented)
}

func TestGetAncestorsPreForkOnly(t *testing.T) {
	require := require.New(t)
	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, activationTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some prefork blocks....
	coreBlk1 := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk2 := blocktest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case coreBlk2.ID():
			return coreBlk2, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk3 := blocktest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// ...Call GetAncestors on them ...
	// Note: we assumed that if blkID is not known, that's NOT an error.
	// Simply return an empty result
	coreVM.BatchedVM.GetAncestorsF = func(_ context.Context, blkID ids.ID, _, _ int, _ time.Duration) ([][]byte, error) {
		res := make([][]byte, 0, 3)
		switch blkID {
		case coreBlk3.ID():
			res = append(res, coreBlk3.Bytes())
			res = append(res, coreBlk2.Bytes())
			res = append(res, coreBlk1.Bytes())
			return res, nil
		case coreBlk2.ID():
			res = append(res, coreBlk2.Bytes())
			res = append(res, coreBlk1.Bytes())
			return res, nil
		case coreBlk1.ID():
			res = append(res, coreBlk1.Bytes())
			return res, nil
		default:
			return res, nil
		}
	}

	reqBlkID := builtBlk3.ID()
	maxBlocksNum := 1000               // a high value to get all built blocks
	maxBlocksSize := 1000000           // a high value to get all built blocks
	maxBlocksRetrivalTime := time.Hour // a high value to get all built blocks
	res, err := proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk3.Bytes(), res[0])
	require.Equal(builtBlk2.Bytes(), res[1])
	require.Equal(builtBlk1.Bytes(), res[2])

	// another good call
	reqBlkID = builtBlk1.ID()
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Len(res, 1)
	require.Equal(builtBlk1.Bytes(), res[0])

	// a faulty call
	reqBlkID = ids.Empty
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Empty(res)
}

func TestGetAncestorsPostForkOnly(t *testing.T) {
	require := require.New(t)
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, activationTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some post-Fork blocks....
	coreBlk1 := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk1.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, adaptToChainBlock(builtBlk1), 0))

	coreBlk2 := blocktest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk2.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, adaptToChainBlock(builtBlk2), 0))

	coreBlk3 := blocktest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(builtBlk3.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk3.ID()))

	// ...Call GetAncestors on them ...
	// Note: we assumed that if blkID is not known, that's NOT an error.
	// Simply return an empty result
	coreVM.BatchedVM.GetAncestorsF = func(_ context.Context, blkID ids.ID, _, _ int, _ time.Duration) ([][]byte, error) {
		res := make([][]byte, 0, 3)
		switch blkID {
		case coreBlk3.ID():
			res = append(res, coreBlk3.Bytes())
			res = append(res, coreBlk2.Bytes())
			res = append(res, coreBlk1.Bytes())
			return res, nil
		case coreBlk2.ID():
			res = append(res, coreBlk2.Bytes())
			res = append(res, coreBlk1.Bytes())
			return res, nil
		case coreBlk1.ID():
			res = append(res, coreBlk1.Bytes())
			return res, nil
		default:
			return res, nil
		}
	}

	coreVM.VM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		default:
			return nil, errUnknownBlock
		}
	}

	reqBlkID := builtBlk3.ID()
	maxBlocksNum := 1000               // a high value to get all built blocks
	maxBlocksSize := 1000000           // a high value to get all built blocks
	maxBlocksRetrivalTime := time.Hour // a high value to get all built blocks
	res, err := proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk3.Bytes(), res[0])
	require.Equal(builtBlk2.Bytes(), res[1])
	require.Equal(builtBlk1.Bytes(), res[2])

	// another good call
	reqBlkID = builtBlk1.ID()
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Len(res, 1)
	require.Equal(builtBlk1.Bytes(), res[0])

	// a faulty call
	reqBlkID = ids.Empty
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Empty(res)
}

func TestGetAncestorsAtSnomanPlusPlusFork(t *testing.T) {
	require := require.New(t)

	var (
		currentTime  = time.Now().Truncate(time.Second)
		preForkTime  = currentTime.Add(5 * time.Minute)
		forkTime     = currentTime.Add(10 * time.Minute)
		postForkTime = currentTime.Add(15 * time.Minute)

		durangoTime = forkTime
	)

	// enable ProBlks in next future
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, forkTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some prefork blocks....
	proRemoteVM.Set(preForkTime)
	coreBlk1 := blocktest.BuildChild(blocktest.Genesis)
	coreBlk1.TimestampV = preForkTime
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk1)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch {
		case blkID == coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk2 := blocktest.BuildChild(coreBlk1)
	coreBlk2.TimestampV = postForkTime
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk2)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch {
		case blkID == coreBlk2.ID():
			return coreBlk2, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// .. and some post-fork
	proRemoteVM.Set(postForkTime)
	coreBlk3 := blocktest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk3)

	// prepare build of next block
	require.NoError(builtBlk3.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk3.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, adaptToChainBlock(builtBlk3), builtBlk3.(*postForkBlock).PChainHeight()))

	coreBlk4 := blocktest.BuildChild(coreBlk3)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk4, nil
	}
	builtBlk4, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk4)
	require.NoError(builtBlk4.Verify(context.Background()))

	// ...Call GetAncestors on them ...
	// Note: we assumed that if blkID is not known, that's NOT an error.
	// Simply return an empty result
	coreVM.GetAncestorsF = func(_ context.Context, blkID ids.ID, maxBlocksNum, _ int, _ time.Duration) ([][]byte, error) {
		sortedBlocks := [][]byte{
			coreBlk4.Bytes(),
			coreBlk3.Bytes(),
			coreBlk2.Bytes(),
			coreBlk1.Bytes(),
		}
		var startIndex int
		switch blkID {
		case coreBlk4.ID():
			startIndex = 0
		case coreBlk3.ID():
			startIndex = 1
		case coreBlk2.ID():
			startIndex = 2
		case coreBlk1.ID():
			startIndex = 3
		default:
			return nil, nil // unknown blockID
		}

		endIndex := min(startIndex+maxBlocksNum, len(sortedBlocks))
		return sortedBlocks[startIndex:endIndex], nil
	}

	// load all known blocks
	reqBlkID := builtBlk4.ID()
	maxBlocksNum := 1000                      // an high value to get all built blocks
	maxBlocksSize := 1000000                  // an high value to get all built blocks
	maxBlocksRetrivalTime := 10 * time.Minute // an high value to get all built blocks
	res, err := proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err)
	require.Len(res, 4)
	require.Equal(builtBlk4.Bytes(), res[0])
	require.Equal(builtBlk3.Bytes(), res[1])
	require.Equal(builtBlk2.Bytes(), res[2])
	require.Equal(builtBlk1.Bytes(), res[3])

	// Regression case: load some prefork and some postfork blocks.
	reqBlkID = builtBlk4.ID()
	maxBlocksNum = 3
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk4.Bytes(), res[0])
	require.Equal(builtBlk3.Bytes(), res[1])
	require.Equal(builtBlk2.Bytes(), res[2])

	// another good call
	reqBlkID = builtBlk1.ID()
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Len(res, 1)
	require.Equal(builtBlk1.Bytes(), res[0])

	// a faulty call
	reqBlkID = ids.Empty
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Empty(res)
}

func TestBatchedParseBlockPreForkOnly(t *testing.T) {
	require := require.New(t)
	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, activationTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some prefork blocks....
	coreBlk1 := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk2 := blocktest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch {
		case blkID == coreBlk2.ID():
			return coreBlk2, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk3 := blocktest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	coreVM.VM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreVM.BatchedParseBlockF = func(_ context.Context, blks [][]byte) ([]chain.Block, error) {
		res := make([]chain.Block, 0, len(blks))
		for _, blkBytes := range blks {
			switch {
			case bytes.Equal(blkBytes, coreBlk1.Bytes()):
				res = append(res, coreBlk1)
			case bytes.Equal(blkBytes, coreBlk2.Bytes()):
				res = append(res, coreBlk2)
			case bytes.Equal(blkBytes, coreBlk3.Bytes()):
				res = append(res, coreBlk3)
			default:
				return nil, errUnknownBlock
			}
		}
		return res, nil
	}

	bytesToParse := [][]byte{
		builtBlk1.Bytes(),
		builtBlk2.Bytes(),
		builtBlk3.Bytes(),
	}
	res, err := proRemoteVM.BatchedParseBlock(context.Background(), bytesToParse)
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk1.ID(), res[0].ID())
	require.Equal(builtBlk2.ID(), res[1].ID())
	require.Equal(builtBlk3.ID(), res[2].ID())
}

func TestBatchedParseBlockPostForkOnly(t *testing.T) {
	require := require.New(t)
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, activationTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some post-Fork blocks....
	coreBlk1 := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk1.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, adaptToChainBlock(builtBlk1), 0))

	coreBlk2 := blocktest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk2.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, adaptToChainBlock(builtBlk2), builtBlk2.(*postForkBlock).PChainHeight()))

	coreBlk3 := blocktest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	coreVM.VM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreVM.BatchedParseBlockF = func(_ context.Context, blks [][]byte) ([]chain.Block, error) {
		res := make([]chain.Block, 0, len(blks))
		for _, blkBytes := range blks {
			switch {
			case bytes.Equal(blkBytes, coreBlk1.Bytes()):
				res = append(res, coreBlk1)
			case bytes.Equal(blkBytes, coreBlk2.Bytes()):
				res = append(res, coreBlk2)
			case bytes.Equal(blkBytes, coreBlk3.Bytes()):
				res = append(res, coreBlk3)
			default:
				return nil, errUnknownBlock
			}
		}
		return res, nil
	}

	bytesToParse := [][]byte{
		builtBlk1.Bytes(),
		builtBlk2.Bytes(),
		builtBlk3.Bytes(),
	}
	res, err := proRemoteVM.BatchedParseBlock(context.Background(), bytesToParse)
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk1.ID(), res[0].ID())
	require.Equal(builtBlk2.ID(), res[1].ID())
	require.Equal(builtBlk3.ID(), res[2].ID())
}

func TestBatchedParseBlockAtSnomanPlusPlusFork(t *testing.T) {
	require := require.New(t)

	var (
		currentTime  = time.Now().Truncate(time.Second)
		preForkTime  = currentTime.Add(5 * time.Minute)
		forkTime     = currentTime.Add(10 * time.Minute)
		postForkTime = currentTime.Add(15 * time.Minute)

		durangoTime = forkTime
	)

	// enable ProBlks in next future
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, forkTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some prefork blocks....
	proRemoteVM.Set(preForkTime)
	coreBlk1 := blocktest.BuildChild(blocktest.Genesis)
	coreBlk1.TimestampV = preForkTime
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk1)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch {
		case blkID == coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk2 := blocktest.BuildChild(coreBlk1)
	coreBlk2.TimestampV = postForkTime
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk2)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch {
		case blkID == coreBlk2.ID():
			return coreBlk2, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// .. and some post-fork
	proRemoteVM.Set(postForkTime)
	coreBlk3 := blocktest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk3)

	// prepare build of next block
	require.NoError(builtBlk3.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk3.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, adaptToChainBlock(builtBlk3), builtBlk3.(*postForkBlock).PChainHeight()))

	coreBlk4 := blocktest.BuildChild(coreBlk3)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk4, nil
	}
	builtBlk4, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk4)
	require.NoError(builtBlk4.Verify(context.Background()))

	coreVM.VM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		case bytes.Equal(b, coreBlk4.Bytes()):
			return coreBlk4, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreVM.BatchedParseBlockF = func(_ context.Context, blks [][]byte) ([]chain.Block, error) {
		res := make([]chain.Block, 0, len(blks))
		for _, blkBytes := range blks {
			switch {
			case bytes.Equal(blkBytes, coreBlk1.Bytes()):
				res = append(res, coreBlk1)
			case bytes.Equal(blkBytes, coreBlk2.Bytes()):
				res = append(res, coreBlk2)
			case bytes.Equal(blkBytes, coreBlk3.Bytes()):
				res = append(res, coreBlk3)
			case bytes.Equal(blkBytes, coreBlk4.Bytes()):
				res = append(res, coreBlk4)
			default:
				return nil, errUnknownBlock
			}
		}
		return res, nil
	}

	bytesToParse := [][]byte{
		builtBlk4.Bytes(),
		builtBlk3.Bytes(),
		builtBlk2.Bytes(),
		builtBlk1.Bytes(),
	}

	res, err := proRemoteVM.BatchedParseBlock(context.Background(), bytesToParse)
	require.NoError(err)
	require.Len(res, 4)
	require.Equal(builtBlk4.ID(), res[0].ID())
	require.Equal(builtBlk3.ID(), res[1].ID())
	require.Equal(builtBlk2.ID(), res[2].ID())
	require.Equal(builtBlk1.ID(), res[3].ID())
}

type TestRemoteProposerVM struct {
	*blocktest.BatchedVM
	*blocktest.VM
}

// chainToEngineBlockAdapter adapts chain.Block to block.Block
type chainToEngineBlockAdapter struct {
	chain.Block
}

func (cb *chainToEngineBlockAdapter) Status() uint8 {
	return uint8(cb.Block.Status())
}

// Implement ChainVM interface methods directly
func (vm TestRemoteProposerVM) BuildBlock(ctx context.Context) (block.Block, error) {
	if vm.VM.BuildBlockF != nil {
		chainBlock, err := vm.VM.BuildBlockF(ctx)
		if err != nil {
			return nil, err
		}
		return &chainToEngineBlockAdapter{Block: chainBlock}, nil
	}
	return nil, fmt.Errorf("BuildBlockF not set")
}

func (vm TestRemoteProposerVM) ParseBlock(ctx context.Context, bytes []byte) (block.Block, error) {
	if vm.VM.ParseBlockF != nil {
		chainBlock, err := vm.VM.ParseBlockF(ctx, bytes)
		if err != nil {
			return nil, err
		}
		return &chainToEngineBlockAdapter{Block: chainBlock}, nil
	}
	return nil, fmt.Errorf("ParseBlockF not set")
}

func (vm TestRemoteProposerVM) GetBlock(ctx context.Context, id ids.ID) (block.Block, error) {
	if vm.VM.GetBlockF != nil {
		chainBlock, err := vm.VM.GetBlockF(ctx, id)
		if err != nil {
			return nil, err
		}
		return &chainToEngineBlockAdapter{Block: chainBlock}, nil
	}
	return nil, fmt.Errorf("GetBlockF not set")
}

func (vm TestRemoteProposerVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if vm.VM.GetBlockIDAtHeightF != nil {
		return vm.VM.GetBlockIDAtHeightF(ctx, height)
	}
	return ids.Empty, nil
}

func (vm TestRemoteProposerVM) SetPreference(ctx context.Context, id ids.ID) error {
	if vm.VM.SetPreferenceF != nil {
		return vm.VM.SetPreferenceF(ctx, id)
	}
	return nil
}

func (vm TestRemoteProposerVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	if vm.VM.LastAcceptedF != nil {
		return vm.VM.LastAcceptedF(ctx)
	}
	return ids.Empty, nil
}

func (vm TestRemoteProposerVM) Initialize(
	ctx context.Context,
	chainCtx interface{},
	db interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	msgChan interface{},
	fxs []interface{},
	appSender interface{},
) error {
	// Convert interface{} parameters to proper types for InitializeF
	if vm.VM.InitializeF != nil {
		// We need to convert the interface parameters
		// For testing, we'll just return nil
		return nil
	}
	return nil
}

func initTestRemoteProposerVM(
	t *testing.T,
	activationTime,
	durangoTime time.Time,
) (
	TestRemoteProposerVM,
	*VM,
) {
	require := require.New(t)

	initialState := []byte("genesis state")
	coreVM := TestRemoteProposerVM{
		VM:        &blocktest.VM{},
		BatchedVM: &blocktest.BatchedVM{},
	}
	coreVM.VM.T = t
	coreVM.BatchedVM.T = t

	coreVM.InitializeF = func(
		context.Context,
		context.Context,
		database.Database,
		[]byte,
		[]byte,
		[]byte,
		[]*core.Fx,
		core.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blocktest.GenesisID, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.VM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(
		coreVM,
		Config{
			ActivationTime:      activationTime,
			DurangoTime:         durangoTime,
			MinimumPChainHeight: 0,
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          metric.NewNoOpMetrics("test").Registry(),
		},
	)

	valState := &validatorstest.State{}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return blocktest.GenesisHeight, nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}

	// Get the node ID from the context that will be created
	thisNode := ids.NodeIDFromCert(&ids.Certificate{
		Raw:       pTestCert.Raw,
		PublicKey: pTestCert.PublicKey,
	})

	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		var (
			nodeID1  = ids.BuildTestNodeID([]byte{1})
			nodeID2  = ids.BuildTestNodeID([]byte{2})
			nodeID3  = ids.BuildTestNodeID([]byte{3})
		)
		return map[ids.NodeID]*validators.GetValidatorOutput{
			thisNode: {
				NodeID: thisNode,
				Weight: 10,
			},
			nodeID1: {
				NodeID: nodeID1,
				Weight: 5,
			},
			nodeID2: {
				NodeID: nodeID2,
				Weight: 6,
			},
			nodeID3: {
				NodeID: nodeID3,
				Weight: 7,
			},
		}, nil
	}

	ctx := consensustest.Context(t, consensustest.CChainID)
	ctx.NodeID = thisNode
	adapter := &testValidatorStateAdapter{valState: valState}
	ctx.ValidatorState = adapter

	require.NoError(proVM.Initialize(
		context.Background(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()), // make sure that DBs are compressed correctly
		initialState,
		nil,  // upgradeBytes
		nil,  // configBytes
		nil,  // msgChan
		nil,  // fxs
		nil,  // appSender
	))

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(context.Background(), interfaces.NormalOp))
	require.NoError(proVM.SetPreference(context.Background(), blocktest.GenesisID))
	return coreVM, proVM
}
