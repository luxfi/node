// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/luxfi/node/snow/consensus/snowman"
	"github.com/luxfi/node/snow/engine/snowman/block"

	oteltrace "go.opentelemetry.io/otel/trace"
)

func (vm *blockVM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	if vm.buildBlockVM == nil {
		return vm.BuildBlock(ctx)
	}

	ctx, span := vm.tracer.Start(ctx, vm.buildBlockWithContextTag, oteltrace.WithAttributes(
		attribute.Int64("pChainHeight", int64(blockCtx.PChainHeight)),
	))
	defer span.End()

	return vm.buildBlockVM.BuildBlockWithContext(ctx, blockCtx)
}
