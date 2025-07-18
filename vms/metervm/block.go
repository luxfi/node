// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/node/snow/engine/snowman/block"
)

var (
	_ chain.Block           = (*meterBlock)(nil)
	_ chain.OracleBlock     = (*meterBlock)(nil)
	_ block.WithVerifyContext = (*meterBlock)(nil)

	errExpectedBlockWithVerifyContext = errors.New("expected block.WithVerifyContext")
)

type meterBlock struct {
	chain.Block

	vm *blockVM
}

func (mb *meterBlock) Verify(ctx context.Context) error {
	start := mb.vm.clock.Time()
	err := mb.Block.Verify(ctx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		mb.vm.blockMetrics.verifyErr.Observe(duration)
	} else {
		mb.vm.verify.Observe(duration)
	}
	return err
}

func (mb *meterBlock) Accept(ctx context.Context) error {
	start := mb.vm.clock.Time()
	err := mb.Block.Accept(ctx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	mb.vm.blockMetrics.accept.Observe(duration)
	return err
}

func (mb *meterBlock) Reject(ctx context.Context) error {
	start := mb.vm.clock.Time()
	err := mb.Block.Reject(ctx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	mb.vm.blockMetrics.reject.Observe(duration)
	return err
}

func (mb *meterBlock) Options(ctx context.Context) ([2]chain.Block, error) {
	oracleBlock, ok := mb.Block.(chain.OracleBlock)
	if !ok {
		return [2]chain.Block{}, chain.ErrNotOracle
	}

	blks, err := oracleBlock.Options(ctx)
	if err != nil {
		return [2]chain.Block{}, err
	}
	return [2]chain.Block{
		&meterBlock{
			Block: blks[0],
			vm:    mb.vm,
		},
		&meterBlock{
			Block: blks[1],
			vm:    mb.vm,
		},
	}, nil
}

func (mb *meterBlock) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	blkWithCtx, ok := mb.Block.(block.WithVerifyContext)
	if !ok {
		return false, nil
	}

	start := mb.vm.clock.Time()
	shouldVerify, err := blkWithCtx.ShouldVerifyWithContext(ctx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	mb.vm.blockMetrics.shouldVerifyWithContext.Observe(duration)
	return shouldVerify, err
}

func (mb *meterBlock) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	blkWithCtx, ok := mb.Block.(block.WithVerifyContext)
	if !ok {
		return fmt.Errorf("%w but got %T", errExpectedBlockWithVerifyContext, mb.Block)
	}

	start := mb.vm.clock.Time()
	err := blkWithCtx.VerifyWithContext(ctx, blockCtx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		mb.vm.blockMetrics.verifyWithContextErr.Observe(duration)
	} else {
		mb.vm.verifyWithContext.Observe(duration)
	}
	return err
}
