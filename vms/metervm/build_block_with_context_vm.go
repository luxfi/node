// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"

	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/consensus/engine/linear/block"
)

func (vm *blockVM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (linear.Block, error) {
	if vm.buildBlockVM == nil {
		return vm.BuildBlock(ctx)
	}

	start := vm.clock.Time()
	blk, err := vm.buildBlockVM.BuildBlockWithContext(ctx, blockCtx)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.blockMetrics.buildBlockWithContextErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.buildBlockWithContext.Observe(duration)
	return &meterBlock{
		Block: blk,
		vm:    vm,
	}, nil
}
