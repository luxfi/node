// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"time"

	"github.com/luxfi/node/consensus/engine/chain/block"
	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/ids"
)

func (vm *blockVM) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	start := vm.clock.Time()
	ancestors, err := vm.batchedVM.GetAncestors(
		ctx,
		blkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	end := vm.clock.Time()
	vm.blockMetrics.getAncestors.Observe(float64(end.Sub(start)))
	return ancestors, err
}

func (vm *blockVM) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]chain.Block, error) {
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	start := vm.clock.Time()
	blocks, err := vm.batchedVM.BatchedParseBlock(ctx, blks)
	end := vm.clock.Time()
	vm.blockMetrics.batchedParseBlock.Observe(float64(end.Sub(start)))

	wrappedBlocks := make([]chain.Block, len(blocks))
	for i, block := range blocks {
		wrappedBlocks[i] = &meterBlock{
			Block: block,
			vm:    vm,
		}
	}
	return wrappedBlocks, err
}
