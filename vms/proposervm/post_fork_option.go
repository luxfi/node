// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
	"github.com/luxfi/node/vms/proposervm/block"
)

var _ PostForkBlock = (*postForkOption)(nil)

// The parent of a *postForkOption must be a *postForkBlock.
type postForkOption struct {
	block.Block
	postForkCommonComponents

	timestamp time.Time
}

// ID returns the block ID as string to satisfy choices.Decidable interface
func (b *postForkOption) ID() string {
	return b.Block.ID().String()
}

func (b *postForkOption) Timestamp() time.Time {
	if b.Height() <= b.vm.lastAcceptedHeight {
		return b.vm.lastAcceptedTime
	}
	return b.timestamp
}

func (b *postForkOption) Accept() error {
	if err := b.acceptOuterBlk(); err != nil {
		return err
	}
	ctx := context.Background()
	return b.acceptInnerBlk(ctx)
}

func (b *postForkOption) acceptOuterBlk() error {
	return b.vm.acceptPostForkBlock(b)
}

func (b *postForkOption) acceptInnerBlk(ctx context.Context) error {
	// mark the inner block as accepted and all conflicting inner blocks as
	// rejected
	return b.vm.Tree.Accept(ctx, b.innerBlk)
}

func (b *postForkOption) Reject() error {
	// we do not reject the inner block here because that block may be contained
	// in the proposer block that causing this block to be rejected.

	blkID, _ := ids.FromString(b.ID())
	delete(b.vm.verifiedBlocks, blkID)
	return nil
}

func (b *postForkOption) Parent() ids.ID {
	return b.ParentID()
}

// Status returns the status of this block
func (b *postForkOption) Status() choices.Status {
	return b.innerBlk.Status()
}

// Time returns the time as Unix timestamp to satisfy chain.Block interface
func (b *postForkOption) Time() uint64 {
	return uint64(b.Timestamp().Unix())
}

// If Verify returns nil, Accept or Reject is eventually called on [b] and
// [b.innerBlk].
func (b *postForkOption) Verify(ctx context.Context) error {
	parent, err := b.vm.getBlock(ctx, b.ParentID())
	if err != nil {
		return err
	}
	// Get timestamp from parent block
	if pfb, ok := parent.(*postForkBlock); ok {
		b.timestamp = pfb.Timestamp()
	} else if pfo, ok := parent.(*postForkOption); ok {
		b.timestamp = pfo.Timestamp()
	} else {
		// This should not happen in post-fork context
		return errUnexpectedBlockType
	}
	return parent.verifyPostForkOption(ctx, b)
}

func (*postForkOption) verifyPreForkChild(context.Context, *preForkBlock) error {
	// A *preForkBlock's parent must be a *preForkBlock
	return errUnsignedChild
}

func (b *postForkOption) verifyPostForkChild(ctx context.Context, child *postForkBlock) error {
	parentTimestamp := b.Timestamp()
	parentPChainHeight, err := b.pChainHeight(ctx)
	if err != nil {
		return err
	}
	return b.postForkCommonComponents.Verify(
		ctx,
		parentTimestamp,
		parentPChainHeight,
		child,
	)
}

func (*postForkOption) verifyPostForkOption(context.Context, *postForkOption) error {
	// A *postForkOption's parent can't be a *postForkOption
	return errUnexpectedBlockType
}

func (b *postForkOption) buildChild(ctx context.Context) (Block, error) {
	parentIDStr := b.ID()
	parentID, _ := ids.FromString(parentIDStr)
	parentPChainHeight, err := b.pChainHeight(ctx)
	if err != nil {
		b.vm.ctx.Log.Error("unexpected build block failure",
			zap.String("reason", "failed to fetch parent's P-chain height"),
			zap.Stringer("parentID", parentID),
			zap.Error(err),
		)
		return nil, err
	}
	return b.postForkCommonComponents.buildChild(
		ctx,
		parentID,
		b.Timestamp(),
		parentPChainHeight,
	)
}

// This block's P-Chain height is its parent's P-Chain height
func (b *postForkOption) pChainHeight(ctx context.Context) (uint64, error) {
	parent, err := b.vm.getBlock(ctx, b.ParentID())
	if err != nil {
		return 0, err
	}
	return parent.pChainHeight(ctx)
}

func (b *postForkOption) getStatelessBlk() block.Block {
	return b.Block
}
