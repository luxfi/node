// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/luxfi/node/consensus/engine/chain/block"

	oteltrace "go.opentelemetry.io/otel/trace"
)

func (vm *blockVM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (block.Block, error) {
	if vm.buildBlockVM == nil {
		return vm.BuildBlock(ctx)
	}

	ctx, span := vm.tracer.Start(ctx, vm.buildBlockWithContextTag, oteltrace.WithAttributes(
		attribute.Int64("pChainHeight", int64(blockCtx.PChainHeight)),
	))
	defer span.End()

	return vm.buildBlockVM.BuildBlockWithContext(ctx, blockCtx)
}
