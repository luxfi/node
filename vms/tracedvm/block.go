// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"

	"github.com/luxfi/node/consensus/engine/chain/block"

	oteltrace "go.opentelemetry.io/otel/trace"
)

var (
	_ block.Block            = (*tracedBlock)(nil)
	_ block.WithVerifyContext = (*tracedBlock)(nil)

	errExpectedBlockWithVerifyContext = errors.New("expected block.WithVerifyContext")
)

type tracedBlock struct {
	block.Block

	vm *blockVM
}

func (b *tracedBlock) Verify(ctx context.Context) error {
	blkID := b.ID()
	ctx, span := b.vm.tracer.Start(ctx, b.vm.verifyTag, oteltrace.WithAttributes(
		attribute.String("blkID", blkID),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return b.Block.Verify(ctx)
}

func (b *tracedBlock) Accept() error {
	ctx := context.Background()
	blkID := b.ID()
	ctx, span := b.vm.tracer.Start(ctx, b.vm.acceptTag, oteltrace.WithAttributes(
		attribute.String("blkID", blkID),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return b.Block.Accept()
}

func (b *tracedBlock) Reject() error {
	ctx := context.Background()
	blkID := b.ID()
	ctx, span := b.vm.tracer.Start(ctx, b.vm.rejectTag, oteltrace.WithAttributes(
		attribute.String("blkID", blkID),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return b.Block.Reject()
}

func (b *tracedBlock) Options(ctx context.Context) ([2]block.Block, error) {
	// Oracle blocks are not supported in the engine/linear/block interface
	return [2]block.Block{}, errors.New("oracle blocks not supported")
}

func (b *tracedBlock) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	blkWithCtx, ok := b.Block.(block.WithVerifyContext)
	if !ok {
		return false, nil
	}

	blkID := b.ID()
	ctx, span := b.vm.tracer.Start(ctx, b.vm.shouldVerifyWithContextTag, oteltrace.WithAttributes(
		attribute.String("blkID", blkID),
		attribute.Int64("height", int64(b.Height())),
	))
	defer span.End()

	return blkWithCtx.ShouldVerifyWithContext(ctx)
}

func (b *tracedBlock) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	blkWithCtx, ok := b.Block.(block.WithVerifyContext)
	if !ok {
		return fmt.Errorf("%w but got %T", errExpectedBlockWithVerifyContext, b.Block)
	}

	blkID := b.ID()
	ctx, span := b.vm.tracer.Start(ctx, b.vm.verifyWithContextTag, oteltrace.WithAttributes(
		attribute.String("blkID", blkID),
		attribute.Int64("height", int64(b.Height())),
		attribute.Int64("pChainHeight", int64(blockCtx.PChainHeight)),
	))
	defer span.End()

	return blkWithCtx.VerifyWithContext(ctx, blockCtx)
}
