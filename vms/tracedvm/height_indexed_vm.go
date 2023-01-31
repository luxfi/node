// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) VerifyHeightIndex(ctx context.Context) error {
	if vm.hVM == nil {
		return block.ErrHeightIndexedVMNotImplemented
	}

<<<<<<< HEAD
	ctx, span := vm.tracer.Start(ctx, vm.verifyHeightIndexTag)
=======
	ctx, span := vm.tracer.Start(ctx, "blockVM.VerifyHeightIndex")
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
	defer span.End()

	return vm.hVM.VerifyHeightIndex(ctx)
}

func (vm *blockVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if vm.hVM == nil {
		return ids.Empty, block.ErrHeightIndexedVMNotImplemented
	}

<<<<<<< HEAD
	ctx, span := vm.tracer.Start(ctx, vm.getBlockIDAtHeightTag, oteltrace.WithAttributes(
=======
	ctx, span := vm.tracer.Start(ctx, "blockVM.GetBlockIDAtHeight", oteltrace.WithAttributes(
>>>>>>> c7cc22f98 (Add VM tracer (#2225))
		attribute.Int64("height", int64(height)),
	))
	defer span.End()

	return vm.hVM.GetBlockIDAtHeight(ctx, height)
}
