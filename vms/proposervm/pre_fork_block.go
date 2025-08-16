// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/protocol/chain"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/proposervm/block"
)

var (
	_ Block = (*preForkBlock)(nil)

	errChildOfPreForkBlockHasProposer = errors.New("child of pre-fork block has proposer")
)

type preForkBlock struct {
	chain.Block
	vm *VM
}

// EpochBit returns the epoch bit for FPC
func (b *preForkBlock) EpochBit() bool {
	// Forward to inner block if it supports it
	if innerBlk, ok := b.Block.(interface{ EpochBit() bool }); ok {
		return innerBlk.EpochBit()
	}
	return false
}

// FPCVotes returns embedded fast-path vote references
func (b *preForkBlock) FPCVotes() [][]byte {
	// Forward to inner block if it supports it
	if innerBlk, ok := b.Block.(interface{ FPCVotes() [][]byte }); ok {
		return innerBlk.FPCVotes()
	}
	return nil
}

// Timestamp returns the timestamp of the inner block
func (b *preForkBlock) Timestamp() time.Time {
	// Forward to inner block if it supports it
	if innerBlk, ok := b.Block.(interface{ Timestamp() time.Time }); ok {
		return innerBlk.Timestamp()
	}
	// Fallback to current time
	return b.vm.Time()
}

func (b *preForkBlock) Accept(ctx context.Context) error {
	if err := b.acceptOuterBlk(); err != nil {
		return err
	}
	return b.acceptInnerBlk(ctx)
}

func (*preForkBlock) acceptOuterBlk() error {
	return nil
}

func (b *preForkBlock) acceptInnerBlk(ctx context.Context) error {
	return b.Block.Accept(ctx)
}

func (b *preForkBlock) Status() choices.Status {
	forkHeight, err := b.vm.GetForkHeight()
	if err == database.ErrNotFound {
		// Pre-fork, so the status is always processing if we have the block
		return choices.Processing
	}
	if err != nil {
		// TODO: Once `Status()` can return an error, we should return the error
		// here.
		b.vm.log.Error("unexpected error looking up fork height",
			zap.Error(err),
		)
		return choices.Processing
	}

	// The fork has occurred earlier than this block, so preForkBlocks are all
	// invalid.
	if b.Height() >= forkHeight {
		return choices.Rejected
	}
	return choices.Processing
}

func (b *preForkBlock) Verify(ctx context.Context) error {
	parent, err := b.vm.getPreForkBlock(ctx, b.Block.Parent())
	if err != nil {
		return err
	}
	return parent.verifyPreForkChild(ctx, b)
}

func (b *preForkBlock) Options(ctx context.Context) ([2]chain.Block, error) {
	// Oracle blocks are not supported in the new consensus
	return [2]chain.Block{}, nil
}

func (b *preForkBlock) getInnerBlk() chain.Block {
	return b.Block
}

func (b *preForkBlock) verifyPreForkChild(ctx context.Context, child *preForkBlock) error {
	parentTimestamp := b.Timestamp()
	if !parentTimestamp.Before(b.vm.ActivationTime) {
		if err := verifyIsOracleBlock(ctx, b.Block); err != nil {
			return err
		}

		b.vm.log.Debug("allowing pre-fork block after the fork time",
			zap.String("reason", "parent is an oracle block"),
			zap.Stringer("blkID", b.ID()),
		)
	}

	return child.Block.Verify(ctx)
}

// This method only returns nil once (during the transition)
func (b *preForkBlock) verifyPostForkChild(ctx context.Context, child *postForkBlock) error {
	if err := verifyIsNotOracleBlock(ctx, b.Block); err != nil {
		return err
	}

	childID := child.ID()
	childPChainHeight := child.PChainHeight()
	vs := consensus.GetValidatorState(b.vm.ctx)
	if vs == nil {
		return fmt.Errorf("validator state not available")
	}
	currentPChainHeight, err := vs.GetCurrentHeight()
	if err != nil {
		b.vm.log.Error("block verification failed",
			zap.String("reason", "failed to get current P-Chain height"),
			zap.Stringer("blkID", childID),
			zap.Error(err),
		)
		return err
	}
	if childPChainHeight > currentPChainHeight {
		return fmt.Errorf("%w: %d > %d",
			errPChainHeightNotReached,
			childPChainHeight,
			currentPChainHeight,
		)
	}
	if childPChainHeight < b.vm.MinimumPChainHeight {
		return errPChainHeightTooLow
	}

	// Make sure [b] is the parent of [child]'s inner block
	expectedInnerParentID := b.ID()
	innerParentID := child.innerBlk.Parent()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	// A *preForkBlock can only have a *postForkBlock child
	// if the *preForkBlock is the last *preForkBlock before activation takes effect
	// (its timestamp is at or after the activation time)
	parentTimestamp := b.Timestamp()
	if parentTimestamp.Before(b.vm.ActivationTime) {
		return errProposersNotActivated
	}

	// Child's timestamp must be at or after its parent's timestamp
	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	// Child timestamp can't be too far in the future
	maxTimestamp := b.vm.Time().Add(maxSkew)
	if childTimestamp.After(maxTimestamp) {
		return errTimeTooAdvanced
	}

	// Verify the lack of signature on the node
	if child.SignedBlock.Proposer() != ids.EmptyNodeID {
		return errChildOfPreForkBlockHasProposer
	}

	// Verify the inner block and track it as verified
	return b.vm.verifyAndRecordInnerBlk(ctx, nil, child)
}

func (*preForkBlock) verifyPostForkOption(context.Context, *postForkOption) error {
	return errUnexpectedBlockType
}

func (b *preForkBlock) buildChild(ctx context.Context) (Block, error) {
	parentTimestamp := b.Timestamp()

	// Check if automining is enabled via environment variable
	autominingEnabled := os.Getenv("LUX_ENABLE_AUTOMINING") == "true"

	if parentTimestamp.Before(b.vm.ActivationTime) && !autominingEnabled {
		// The chain hasn't forked yet (unless automining is enabled)
		innerBlock, err := b.vm.ChainVM.BuildBlock(ctx)
		if err != nil {
			return nil, err
		}

		b.vm.log.Info("built block",
			zap.Stringer("blkID", innerBlock.ID()),
			zap.Uint64("height", innerBlock.Height()),
			zap.Time("parentTimestamp", parentTimestamp),
		)

		return &preForkBlock{
			Block: innerBlock,
			vm:    b.vm,
		}, nil
	}

	// The chain is currently forking

	parentID := b.ID()
	newTimestamp := b.vm.Time().Truncate(time.Second)
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
	}

	// The child's P-Chain height is proposed as the optimal P-Chain height that
	// is at least the minimum height
	pChainHeight, err := b.vm.optimalPChainHeight(ctx, b.vm.MinimumPChainHeight)
	if err != nil {
		b.vm.log.Error("unexpected build block failure",
			zap.String("reason", "failed to calculate optimal P-chain height"),
			zap.Stringer("parentID", parentID),
			zap.Error(err),
		)
		return nil, err
	}

	innerBlock, err := b.vm.ChainVM.BuildBlock(ctx)
	if err != nil {
		return nil, err
	}

	statelessBlock, err := block.BuildUnsigned(
		parentID,
		newTimestamp,
		pChainHeight,
		innerBlock.Bytes(),
	)
	if err != nil {
		return nil, err
	}

	blk := &postForkBlock{
		SignedBlock: statelessBlock,
		postForkCommonComponents: postForkCommonComponents{
			vm:       b.vm,
			innerBlk: innerBlock,
			status:   choices.Processing,
		},
	}

	b.vm.log.Info("built block",
		zap.Stringer("blkID", blk.ID()),
		zap.Stringer("innerBlkID", innerBlock.ID()),
		zap.Uint64("height", blk.Height()),
		zap.Uint64("pChainHeight", pChainHeight),
		zap.Time("parentTimestamp", parentTimestamp),
		zap.Time("blockTimestamp", newTimestamp))
	return blk, nil
}

func (*preForkBlock) pChainHeight(context.Context) (uint64, error) {
	return 0, nil
}
