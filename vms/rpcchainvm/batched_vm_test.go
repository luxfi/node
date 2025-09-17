// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/engine/chain/block/blockmock"
	"github.com/luxfi/consensus/engine/chain/chainmock"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/chain"
)

var (
	blkBytes1 = []byte{1}
	blkBytes2 = []byte{2}

	blkID0 = ids.ID{0}
	blkID1 = ids.ID{1}
	blkID2 = ids.ID{2}

	status1 = choices.Accepted
	status2 = choices.Processing

	time1 = time.Unix(1, 0)
	time2 = time.Unix(2, 0)
)

// ChainVMAdapter adapts a blockmock.ChainVM to block.ChainVM interface
type ChainVMAdapter struct {
	vm *blockmock.ChainVM
}

func (c *ChainVMAdapter) Initialize(ctx context.Context, chainCtx interface{}, db interface{}, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, msgChan interface{}, fxs []interface{}, appSender interface{}) error {
	return c.vm.Initialize(ctx, chainCtx, db, genesisBytes, upgradeBytes, configBytes, msgChan, fxs, appSender)
}

func (c *ChainVMAdapter) BuildBlock(ctx context.Context) (block.Block, error) {
	mockBlock, err := c.vm.BuildBlock(ctx)
	if err != nil {
		return nil, err
	}
	// The chainmock.Block already implements block.Block interface
	return mockBlock, nil
}

func (c *ChainVMAdapter) ParseBlock(ctx context.Context, bytes []byte) (block.Block, error) {
	mockBlock, err := c.vm.ParseBlock(ctx, bytes)
	if err != nil {
		return nil, err
	}
	// The chainmock.Block already implements block.Block interface
	return mockBlock, nil
}

func (c *ChainVMAdapter) GetBlock(ctx context.Context, id ids.ID) (block.Block, error) {
	mockBlock, err := c.vm.GetBlock(ctx, id)
	if err != nil {
		return nil, err
	}
	// The chainmock.Block already implements block.Block interface
	return mockBlock, nil
}

func (c *ChainVMAdapter) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return c.vm.GetBlockIDAtHeight(ctx, height)
}

func (c *ChainVMAdapter) SetPreference(ctx context.Context, id ids.ID) error {
	return c.vm.SetPreference(ctx, id)
}

func (c *ChainVMAdapter) LastAccepted(ctx context.Context) (ids.ID, error) {
	return c.vm.LastAccepted(ctx)
}

func batchedParseBlockCachingTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "batchedParseBlockCachingTestKey"

	// create mock
	vm := &blockmock.ChainVM{}

	if loadExpectations {
		blk1 := chainmock.NewBlock(blkID1, blkID0, 1)
		blk2 := chainmock.NewBlock(blkID2, blkID1, 2)

		parseBlockCallCount := 0
		vm.ParseBlockF = func(ctx context.Context, bytes []byte) (block.Block, error) {
			parseBlockCallCount++
			switch parseBlockCallCount {
			case 1:
				if string(bytes) == string(blkBytes1) {
					return blk1, nil
				}
			case 2:
				if string(bytes) == string(blkBytes2) {
					return blk2, nil
				}
			}
			return nil, nil
		}

		vm.LastAcceptedF = func(ctx context.Context) (ids.ID, error) {
			return preSummaryBlk.ID(), nil
		}

		vm.GetBlockF = func(ctx context.Context, id ids.ID) (block.Block, error) {
			return preSummaryBlk, nil
		}
	}

	return &ChainVMAdapter{vm}
}

func TestBatchedParseBlockCaching(t *testing.T) {
	require := require.New(t)
	testKey := batchedParseBlockCachingTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	// Initialize the VM - using nil for all parameters as this is a test
	require.NoError(vm.Initialize(context.Background(), nil, memdb.New(), nil, nil, nil, nil, nil, nil))

	// Call should parse the first block
	blk, err := vm.ParseBlock(context.Background(), blkBytes1)
	require.NoError(err)
	require.Equal(blkID1, blk.ID())

	require.IsType(&chain.BlockWrapper{}, blk)

	// Call should cache the first block and parse the second block
	blks, err := vm.BatchedParseBlock(context.Background(), [][]byte{blkBytes1, blkBytes2})
	require.NoError(err)
	require.Len(blks, 2)
	require.Equal(blkID1, blks[0].ID())
	require.Equal(blkID2, blks[1].ID())

	require.IsType(&chain.BlockWrapper{}, blks[0])
	require.IsType(&chain.BlockWrapper{}, blks[1])

	// Call should be fully cached and not result in a grpc call
	blks, err = vm.BatchedParseBlock(context.Background(), [][]byte{blkBytes1, blkBytes2})
	require.NoError(err)
	require.Len(blks, 2)
	require.Equal(blkID1, blks[0].ID())
	require.Equal(blkID2, blks[1].ID())

	require.IsType(&chain.BlockWrapper{}, blks[0])
	require.IsType(&chain.BlockWrapper{}, blks[1])
}
