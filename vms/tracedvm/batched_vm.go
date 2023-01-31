// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

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
<<<<<<< HEAD
<<<<<<< HEAD
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, vm.getAncestorsTag, oteltrace.WithAttributes(
=======
	if vm.bVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, "blockVM.GetAncestors", oteltrace.WithAttributes(
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
=======
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, vm.getAncestorsTag, oteltrace.WithAttributes(
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
		attribute.Stringer("blkID", blkID),
		attribute.Int("maxBlocksNum", maxBlocksNum),
		attribute.Int("maxBlocksSize", maxBlocksSize),
		attribute.Int64("maxBlocksRetrivalTime", int64(maxBlocksRetrivalTime)),
	))
	defer span.End()

<<<<<<< HEAD
<<<<<<< HEAD
	return vm.batchedVM.GetAncestors(
=======
	return vm.bVM.GetAncestors(
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
=======
	return vm.batchedVM.GetAncestors(
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
		ctx,
		blkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
}

func (vm *blockVM) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]snowman.Block, error) {
<<<<<<< HEAD
<<<<<<< HEAD
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, vm.batchedParseBlockTag, oteltrace.WithAttributes(
=======
	if vm.bVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, "blockVM.BatchedParseBlock", oteltrace.WithAttributes(
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
=======
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, vm.batchedParseBlockTag, oteltrace.WithAttributes(
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
		attribute.Int("numBlocks", len(blks)),
	))
	defer span.End()

<<<<<<< HEAD
<<<<<<< HEAD
	blocks, err := vm.batchedVM.BatchedParseBlock(ctx, blks)
=======
	blocks, err := vm.bVM.BatchedParseBlock(ctx, blks)
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
=======
	blocks, err := vm.batchedVM.BatchedParseBlock(ctx, blks)
>>>>>>> 37ccd9a48 (Add BuildBlockWithContext as an optional VM method (#2210))
	if err != nil {
		return nil, err
	}

	wrappedBlocks := make([]snowman.Block, len(blocks))
	for i, block := range blocks {
		wrappedBlocks[i] = &tracedBlock{
			Block: block,
			vm:    vm,
		}
	}
	return wrappedBlocks, nil
}
