// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/consensustest"
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

func batchedParseBlockCachingTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "batchedParseBlockCachingTestKey"

	// create mock
	vm := &blockmock.ChainVM{}
	
	if loadExpectations {
		blk1 := &chainmock.Block{
			IDF:        func() ids.ID { return blkID1 },
			ParentF:    func() ids.ID { return blkID0 },
			HeightF:    func() uint64 { return 1 },
			TimestampF: func() time.Time { return time1 },
		}
		blk2 := &chainmock.Block{
			IDF:        func() ids.ID { return blkID2 },
			ParentF:    func() ids.ID { return blkID1 },
			HeightF:    func() uint64 { return 2 },
			TimestampF: func() time.Time { return time2 },
		}
		
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
		
		vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
			return preSummaryBlk.ID(), nil
		}
		
		vm.GetBlockF = func(context.Context, ids.ID) (block.Block, error) {
			return preSummaryBlk, nil
		}
	}

	return vm
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
