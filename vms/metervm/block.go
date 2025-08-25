// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/protocol/chain"
	"github.com/luxfi/ids"
)

var (
	_ block.Block             = (*meterBlock)(nil)
	_ block.WithVerifyContext = (*meterBlock)(nil)

	errExpectedBlockWithVerifyContext = errors.New("expected block.WithVerifyContext")
)

// meterBlock wraps a chain.Block to satisfy the block.Block interface
// while adding metrics
type meterBlock struct {
	innerBlock chain.Block
	vm         *blockVM
}

// ID returns the block's ID
func (mb *meterBlock) ID() ids.ID {
	return mb.innerBlock.ID()
}

// ParentID returns the parent block's ID (block.Block expects ParentID)
func (mb *meterBlock) ParentID() ids.ID {
	return mb.innerBlock.Parent()
}

// Height returns the block's height
func (mb *meterBlock) Height() uint64 {
	return mb.innerBlock.Height()
}

// Timestamp returns the block's timestamp as time.Time
// This satisfies the block.Block interface
func (mb *meterBlock) Timestamp() time.Time {
	return time.Unix(mb.innerBlock.Timestamp(), 0)
}

// Status returns the block's status as uint8
func (mb *meterBlock) Status() uint8 {
	return uint8(mb.innerBlock.Status())
}

// Bytes returns the serialized block
func (mb *meterBlock) Bytes() []byte {
	return mb.innerBlock.Bytes()
}

func (mb *meterBlock) Verify(ctx context.Context) error {
	start := mb.vm.clock.Time()
	err := mb.innerBlock.Verify(ctx)
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
	err := mb.innerBlock.Accept(ctx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	mb.vm.blockMetrics.accept.Observe(duration)
	return err
}

func (mb *meterBlock) Reject(ctx context.Context) error {
	start := mb.vm.clock.Time()
	err := mb.innerBlock.Reject(ctx)
	end := mb.vm.clock.Time()
	duration := float64(end.Sub(start))
	mb.vm.blockMetrics.reject.Observe(duration)
	return err
}

// func (mb *meterBlock) Options(ctx context.Context) ([2]chain.Block, error) {
// 	oracleBlock, ok := mb.Block.(chain.OracleBlock)
// 	if !ok {
// 		return [2]chain.Block{}, chain.ErrNotOracle
// 	}

// 	blks, err := oracleBlock.Options(ctx)
// 	if err != nil {
// 		return [2]chain.Block{}, err
// 	}
// 	return [2]chain.Block{
// 		&meterBlock{
// 			Block: blks[0],
// 			vm:    mb.vm,
// 		},
// 		&meterBlock{
// 			Block: blks[1],
// 			vm:    mb.vm,
// 		},
// 	}, nil
// }

func (mb *meterBlock) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	blkWithCtx, ok := mb.innerBlock.(block.WithVerifyContext)
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
	blkWithCtx, ok := mb.innerBlock.(block.WithVerifyContext)
	if !ok {
		return fmt.Errorf("%w but got %T", errExpectedBlockWithVerifyContext, mb.innerBlock)
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
