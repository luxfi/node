// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/log"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/lp181"
	"github.com/luxfi/node/vms/proposervm/proposer"
	"github.com/luxfi/runtime"
	chain "github.com/luxfi/vm/chain"
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

// acceptOuterBlk is a NO-OP for a genuine pre-fork block — a pre-fork block has no
// outer envelope, so there is nothing to index — and a hard REFUSAL once the chain
// has forked.
//
// The refusal is the second half of the finality-index root-cause fix (the first is
// vm.getPreForkBlock). Accepting a pre-fork block at or above the fork height
// advances the inner VM while leaving the proposervm index exactly where it was:
// the index silently falls behind the inner tip during normal operation, and the
// NEXT boot cannot start the chain. Refusing here converts that invisible,
// deferred, fatal corruption into an immediate, attributable error at the exact
// accept that would have caused it — and the engine's accept path is fail-closed,
// so finality stops rather than diverging.
func (b *preForkBlock) acceptOuterBlk() error {
	return b.vm.refusePreForkAfterFork(b.Height())
}

func (b *preForkBlock) acceptInnerBlk(ctx context.Context) error {
	return b.Block.Accept(ctx)
}

func (b *preForkBlock) Verify(ctx context.Context) error {
	parent, err := b.vm.getPreForkBlock(ctx, b.Block.Parent())
	if err != nil {
		return err
	}
	return parent.verifyPreForkChild(ctx, b)
}

func (b *preForkBlock) Options(ctx context.Context) ([2]chain.Block, error) {
	oracleBlk, ok := b.Block.(OracleBlock)
	if !ok {
		return [2]chain.Block{}, errNotOracle
	}

	options, err := oracleBlk.Options(ctx)
	if err != nil {
		return [2]chain.Block{}, err
	}
	// A pre-fork block's child options are always pre-fork blocks
	return [2]chain.Block{
		&preForkBlock{
			Block: options[0],
			vm:    b.vm,
		},
		&preForkBlock{
			Block: options[1],
			vm:    b.vm,
		},
	}, nil
}

func (b *preForkBlock) getInnerBlk() chain.Block {
	return b.Block
}

func (b *preForkBlock) verifyPreForkChild(ctx context.Context, child *preForkBlock) error {
	// preFork children are only allowed when the parent is an oracle block.
	if err := verifyIsOracleBlock(ctx, b.Block); err != nil {
		return errUnexpectedBlockType
	}

	b.vm.logger.Debug("allowing pre-fork block after the fork time",
		log.String("reason", "parent is an oracle block"),
		log.Stringer("blkID", b.ID()),
	)

	return child.Block.Verify(ctx)
}

// This method only returns nil once (during the transition)
func (b *preForkBlock) verifyPostForkChild(ctx context.Context, child *postForkBlock) error {
	// FIX 4: Oracle parent validation - check if parent is oracle
	parentIsOracle := verifyIsOracleBlock(ctx, b.Block) == nil
	if parentIsOracle && child.SignedBlock.Proposer() != ids.EmptyNodeID {
		return errChildOfPreForkBlockHasProposer
	}

	if err := verifyIsNotOracleBlock(ctx, b.Block); err != nil {
		return err
	}

	childID := child.ID()
	childPChainHeight := child.PChainHeight()
	currentPChainHeight, err := b.vm.validatorState.GetCurrentHeight(ctx)
	if err != nil {
		b.vm.logger.Error("block verification failed",
			log.String("reason", "failed to get current P-Chain height"),
			log.Stringer("blkID", childID),
			log.Err(err),
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
	// Make sure [b] is the parent of [child]'s inner block
	expectedInnerParentID := b.ID()
	innerParentID := child.innerBlk.Parent()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	// pre-fork → post-fork transition condition is permanently satisfied.
	parentTimestamp := b.Timestamp()

	// Child's timestamp must be at or after its parent's timestamp
	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	// Validate epoch (LP-181 epoching, always-on under activate-all-implicitly).
	// Pre-fork blocks always have empty epoch, so use that as parent epoch
	parentEpoch := block.Epoch{} // Pre-fork blocks have no epoch
	childEpoch := child.PChainEpoch()
	// For pre-fork blocks, we don't have explicit P-chain height tracking.
	// We use 0 as the parent P-chain height for genesis/pre-fork blocks.
	parentPChainHeight := uint64(0)
	expectedEpoch := lp181.NewEpoch(b.vm.Upgrades, parentPChainHeight, parentEpoch, parentTimestamp, childTimestamp)
	if childEpoch != expectedEpoch {
		return fmt.Errorf("%w: epoch %v != expected %v", errEpochMismatch, childEpoch, expectedEpoch)
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
	// "chain is currently forking" path is the only one taken.
	parentTimestamp := b.Timestamp()
	parentID := b.ID()
	newTimestamp := b.vm.Time().Truncate(proposer.TimestampGranularity())
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
	}

	// The child's P-Chain height is proposed as the optimal P-Chain height
	// available; under activate-all-implicitly there is no historical
	// minimum-height floor.
	pChainHeight, err := b.vm.selectChildPChainHeight(ctx, 0)
	if err != nil {
		b.vm.logger.Error("unexpected build block failure",
			log.String("reason", "failed to calculate optimal P-chain height"),
			log.Stringer("parentID", parentID),
			log.Err(err),
		)
		return nil, err
	}

	// CRITICAL-1: single-proposer transition. The pre-fork → post-fork transition
	// block (the first post-fork block, child of the last pre-fork block) MUST stay
	// UNSIGNED — verifyPostForkChild rejects a signed transition
	// (errChildOfPreForkBlockHasProposer), and an unsigned block carries no
	// verifiable proposer binding, so the wire format cannot be made single-proposer
	// by signing it. But WHO builds it can and MUST be gated. Without this gate every
	// validator builds its OWN unsigned transition block stamped with its LOCAL
	// wall-clock second (newTimestamp); a fleet that crosses into Ready at different
	// instants then emits DIFFERENT height-(N+1) blocks → two valid blocks at one
	// height → a FORK at chain start, which every fresh net (devnet/Zoo/Hanzo) and
	// every existing-chain upgrade traverses. Gate the builder with the SAME windower
	// the post-fork path uses (shouldBuildSignedBlockPostDurango): only the elected
	// proposer for the CURRENT slot builds; as wall-clock advances the eligible set
	// widens (slot progression), so a down leader does not stall the transition —
	// liveness is provided by vm.timeToBuild windowing the wait. Non-leaders return
	// WITHOUT building and adopt the leader's gossiped transition block through the
	// α-of-K cert path. This makes the HONEST fleet emit exactly ONE transition block.
	// The residual case — a Byzantine node publishing a competing UNSIGNED transition
	// block (which verifyPostForkChild still admits, since an unsigned block cannot be
	// bound to a proposer) — is rendered SAFE by the per-height finality guard (only
	// one block finalizes at a height) and no longer crashes the fleet (consensus
	// CRITICAL-2). Correct resolution of ExpectedProposer on a sovereign L1 depends on
	// CRITICAL-3 (the windower reading the L1's own validator set, not an empty
	// primary set).
	childHeight := b.Height() + 1
	slot := proposer.TimeToSlot(parentTimestamp, newTimestamp)
	expectedProposerID, err := b.vm.Windower.ExpectedProposer(ctx, childHeight, pChainHeight, slot)
	switch {
	case errors.Is(err, proposer.ErrAnyoneCanPropose):
		// No proposer schedule (empty/degenerate validator set — e.g. K==1, or a
		// chain whose windower set is not yet populated). Fall through to the legacy
		// unsigned build: single-proposer cannot hold without a schedule, and
		// CRITICAL-2 makes the residual equivocation survivable.
	case err != nil:
		b.vm.logger.Error("unexpected build block failure",
			log.String("reason", "failed to calculate expected transition proposer"),
			log.Stringer("parentID", parentID),
			log.Err(err),
		)
		return nil, err
	case expectedProposerID != b.vm.rt.NodeID:
		// Not our turn at this slot — DO NOT build. vm.timeToBuild windows the wait
		// so we adopt the elected leader's gossiped transition block; a later slot
		// elects us iff the leader is down.
		b.vm.logger.Debug("transition build dropped: not our slot",
			log.Stringer("parentID", parentID),
			log.Uint64("childHeight", childHeight),
			log.Uint64("slot", slot),
			log.Stringer("expectedProposer", expectedProposerID),
		)
		return nil, fmt.Errorf("%w: slot %d expects %s", errUnexpectedProposer, slot, expectedProposerID)
	}
	// else: we ARE the elected proposer for this slot — build the unsigned block.

	var innerBlock chain.Block
	if b.vm.blockBuilderVM != nil {
		// VM supports BuildBlockWithRuntime
		builtBlock, err := b.vm.blockBuilderVM.BuildBlockWithRuntime(ctx, &runtime.Runtime{})
		if err != nil {
			return nil, err
		}
		innerBlock = builtBlock
	} else {
		// VM doesn't support BuildBlockWithRuntime, use BuildBlock
		engineBlock, err := b.vm.ChainVM.BuildBlock(ctx)
		if err != nil {
			return nil, err
		}
		innerBlock = engineBlock
	}

	// Calculate the epoch for the child block (LP-181, always-on under
	// activate-all-implicitly).
	parentEpoch := block.Epoch{} // Pre-fork blocks have no epoch
	// For pre-fork blocks, we don't have explicit P-chain height tracking.
	// We use 0 as the parent P-chain height for genesis/pre-fork blocks.
	parentPChainHeight := uint64(0)
	childEpoch := lp181.NewEpoch(b.vm.Upgrades, parentPChainHeight, parentEpoch, parentTimestamp, newTimestamp)

	statelessBlock, err := block.BuildUnsigned(
		parentID,
		newTimestamp,
		pChainHeight,
		childEpoch,
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
		},
	}

	b.vm.logger.Info("built block",
		log.Stringer("blkID", blk.ID()),
		log.Stringer("innerBlkID", innerBlock.ID()),
		log.Uint64("height", blk.Height()),
		log.Uint64("pChainHeight", pChainHeight),
		log.Time("parentTimestamp", parentTimestamp),
		log.Time("blockTimestamp", newTimestamp))
	return blk, nil
}

func (*preForkBlock) pChainHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (*preForkBlock) pChainEpoch(context.Context) (chain.Epoch, error) {
	return chain.Epoch{}, nil
}

func (b *preForkBlock) selectChildPChainHeight(ctx context.Context) (uint64, error) {
	return b.vm.selectChildPChainHeight(ctx, 0)
}
