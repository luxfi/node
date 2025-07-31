// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/node/quasar/engine/chain/block"
)

var (
	_ block.Block            = (*meterBlock)(nil)
	_ block.WithVerifyContext = (*meterBlock)(nil)

	errExpectedBlockWithVerifyContext = errors.New("expected block.WithVerifyContext")
)

type meterBlock struct {
	block.Block

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

func (mb *meterBlock) Accept() error {
	start := mb.vm.clock.Time()
	err := mb.Block.Accept()
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	mb.vm.blockMetrics.accept.Observe(duration)
	return err
}

func (mb *meterBlock) Reject() error {
	start := mb.vm.clock.Time()
	err := mb.Block.Reject()
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	mb.vm.blockMetrics.reject.Observe(duration)
	return err
}

func (mb *meterBlock) Options(ctx context.Context) ([2]block.Block, error) {
	// Oracle blocks are not supported in the engine/linear/block interface
	return [2]block.Block{}, errors.New("oracle blocks not supported")
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
