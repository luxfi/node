// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
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
<<<<<<< HEAD
<<<<<<< HEAD
	ancestors, err := vm.batchedVM.GetAncestors(
=======
	ancestors, err := vm.bVM.GetAncestors(
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
	ancestors, err := vm.batchedVM.GetAncestors(
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
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

func (vm *blockVM) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]snowman.Block, error) {
<<<<<<< HEAD
<<<<<<< HEAD
	if vm.batchedVM == nil {
=======
	if vm.bVM == nil {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
	if vm.batchedVM == nil {
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
		return nil, block.ErrRemoteVMNotImplemented
	}

	start := vm.clock.Time()
<<<<<<< HEAD
<<<<<<< HEAD
	blocks, err := vm.batchedVM.BatchedParseBlock(ctx, blks)
=======
	blocks, err := vm.bVM.BatchedParseBlock(ctx, blks)
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
	blocks, err := vm.batchedVM.BatchedParseBlock(ctx, blks)
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
	end := vm.clock.Time()
	vm.blockMetrics.batchedParseBlock.Observe(float64(end.Sub(start)))

	wrappedBlocks := make([]snowman.Block, len(blocks))
	for i, block := range blocks {
		wrappedBlocks[i] = &meterBlock{
			Block: block,
			vm:    vm,
		}
	}
	return wrappedBlocks, err
}
