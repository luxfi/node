// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/lp181"
	"github.com/luxfi/node/vms/proposervm/proposer"
	"github.com/luxfi/runtime"
	vmcore "github.com/luxfi/vm"
	chain "github.com/luxfi/vm/chain"
)

const (
	// allowable block issuance in the future
	maxSkew = 10 * time.Second
)

var (
	errUnsignedChild            = errors.New("expected child to be signed")
	errUnexpectedBlockType      = errors.New("unexpected proposer block type")
	errInnerParentMismatch      = errors.New("inner parentID didn't match expected parent")
	errTimeNotMonotonic         = errors.New("time must monotonically increase")
	errPChainHeightNotMonotonic = errors.New("non monotonically increasing P-chain height")
	errPChainHeightNotReached   = errors.New("block P-chain height larger than current P-chain height")
	errTimeTooAdvanced          = errors.New("time is too far advanced")
	errEpochMismatch            = errors.New("epoch mismatch")
	errProposerWindowNotStarted = errors.New("proposer window hasn't started")
	errUnexpectedProposer       = errors.New("unexpected proposer for current window")
	errProposerMismatch         = errors.New("proposer mismatch")
	errProposersNotActivated    = errors.New("proposers haven't been activated yet")
	errPChainHeightTooLow       = errors.New("block P-chain height is too low")
	errNotOracle                = errors.New("block is not an oracle block")
)

// OracleBlock is a block that can return multiple child options
type OracleBlock interface {
	chain.Block
	Options(context.Context) ([2]chain.Block, error)
}

// Convert chain.Epoch (consensus) to block.Epoch (proposervm stateless block)
func toBlockEpoch(ce chain.Epoch) block.Epoch {
	return block.Epoch{
		PChainHeight: ce.PChainHeight,
		Number:       ce.Number,
		StartTime:    ce.StartTime,
	}
}

// Convert block.Epoch (proposervm stateless block) to chain.Epoch (consensus)
func toChainBlockEpoch(be block.Epoch) chain.Epoch {
	return chain.Epoch{
		PChainHeight: be.PChainHeight,
		Number:       be.Number,
		StartTime:    be.StartTime,
	}
}

type Block interface {
	chain.Block

	getInnerBlk() chain.Block

	// After a state sync, we may need to update last accepted block data
	// without propagating any changes to the innerVM.
	// acceptOuterBlk and acceptInnerBlk allow controlling acceptance of outer
	// and inner blocks.
	acceptOuterBlk() error
	acceptInnerBlk(context.Context) error

	verifyPreForkChild(ctx context.Context, child *preForkBlock) error
	verifyPostForkChild(ctx context.Context, child *postForkBlock) error
	verifyPostForkOption(ctx context.Context, child *postForkOption) error

	buildChild(context.Context) (Block, error)

	pChainHeight(context.Context) (uint64, error)
	pChainEpoch(context.Context) (chain.Epoch, error)
	selectChildPChainHeight(context.Context) (uint64, error)
}

type PostForkBlock interface {
	Block

	getStatelessBlk() block.Block
	setInnerBlk(chain.Block)
}

// field of postForkBlock and postForkOption
type postForkCommonComponents struct {
	vm       *VM
	innerBlk chain.Block
}

// Return the inner block's height
func (p *postForkCommonComponents) Height() uint64 {
	return p.innerBlk.Height()
}

// Verify returns nil if:
// 1) [p]'s inner block is not an oracle block
// 2) [child]'s P-Chain height >= [parentPChainHeight]
// 3) [p]'s inner block is the parent of [c]'s inner block
// 4) [child]'s timestamp isn't before [p]'s timestamp
// 5) [child]'s timestamp is within the skew bound
// 6) [childPChainHeight] <= the current P-Chain height
// 7) [child]'s timestamp is within its proposer's window
// 8) [child] has a valid signature from its proposer
// 9) [child]'s inner block is valid
// 10) [child] has the expected epoch
func (p *postForkCommonComponents) Verify(
	ctx context.Context,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	parentEpoch chain.Epoch,
	child *postForkBlock,
) error {
	if err := verifyIsNotOracleBlock(ctx, p.innerBlk); err != nil {
		return err
	}

	childPChainHeight := child.PChainHeight()
	if childPChainHeight < parentPChainHeight {
		return errPChainHeightNotMonotonic
	}

	expectedInnerParentID := p.innerBlk.ID()
	innerParentID := child.innerBlk.Parent()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	childTimestamp := child.Timestamp()
	// Check timestamp monotonicity first
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	childEpoch := child.PChainEpoch()

	// Check timestamp is not too far in the future
	maxTimestamp := p.vm.Time().Add(maxSkew)
	if childTimestamp.After(maxTimestamp) {
		return errTimeTooAdvanced
	}

	// FIX 1: Consolidate all P-chain dependent validations into single block
	if p.vm.consensusState == uint32(vmcore.Ready) {
		// All P-chain dependent validations here - only when synced
		// 1. Epoch validation
		if expected := lp181.NewEpoch(p.vm.Upgrades, parentPChainHeight, toBlockEpoch(parentEpoch), parentTimestamp, childTimestamp); childEpoch != expected {
			return fmt.Errorf("%w: epoch %v != expected %v", errEpochMismatch, childEpoch, expected)
		}

		// 2. P-chain height check
		currentPChainHeight, err := p.vm.validatorState.GetCurrentHeight(ctx)
		if err != nil {
			p.vm.logger.Error("block verification failed",
				log.String("reason", "failed to get current P-Chain height"),
				log.Stringer("blkID", child.ID()),
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

		// 3. Proposer window validation
		shouldHaveProposer, err := p.verifyPostDurangoBlockDelay(ctx, parentTimestamp, parentPChainHeight, child)
		if err != nil {
			return err
		}

		hasProposer := child.SignedBlock.Proposer() != ids.EmptyNodeID
		if shouldHaveProposer != hasProposer {
			return fmt.Errorf("%w: shouldHaveProposer (%v) != hasProposer (%v)", errProposerMismatch, shouldHaveProposer, hasProposer)
		}

		p.vm.logger.Debug("verified post-fork block",
			log.Stringer("blkID", child.ID()),
			log.Time("parentTimestamp", parentTimestamp),
			log.Time("blockTimestamp", childTimestamp),
		)
	}

	_ = childPChainHeight
	_ = parentPChainHeight
	contextPChainHeight := childEpoch.PChainHeight

	return p.vm.verifyAndRecordInnerBlk(
		ctx,
		&runtime.Runtime{
			PChainHeight: contextPChainHeight,
		},
		child,
	)
}

// slotSnappedChildTimestamp returns the timestamp a child built at wall-clock `now` off a
// parent at `parentTimestamp` should carry. It is the parent-anchored proposer-window grid:
// parentTimestamp + slot*WindowDuration, where slot = TimeToSlot(parentTimestamp, now).
//
// WHY (round-scoped view-change liveness): off a STALE parent — idle past the proposer
// window, so "anyone can propose" — buildChild is called repeatedly, and a raw wall-clock
// timestamp (now.Truncate(1s)) makes EVERY rebuild a DISTINCT envelope over the same inner
// block. That is an unbounded, ever-growing set of siblings at one height; the round-scoped
// view-change can never gather α aligned prevotes on any single candidate (no proof-of-lock
// forms) → finality stalls fleet-wide (the distributed liveness stall a goroutine dump of the
// hung fleet showed: all nodes quiescent, waiting for votes no one will send). Snapping to the
// window grid makes all rebuilds WITHIN one slot byte-IDENTICAL — one stable candidate per
// slot for the view-change to converge on.
//
// WHY IT IS SAFE: TimeToSlot(parent, snapped) == slot == TimeToSlot(parent, now), so the
// block's SLOT is unchanged; ExpectedProposer (verifyPostDurangoBlockDelay /
// shouldBuildSignedBlockPostDurango) returns the IDENTICAL verdict, so the proposer-window and
// signed/unsigned checks are byte-for-byte unaffected, and the derived epoch is stabilised
// across the slot instead of churned. Only slot>0 is snapped, so:
//   - normal in-window production (slot 0, child < WindowDuration after parent) keeps its exact
//     sub-window timestamp — zero behaviour change on a live chain;
//   - a snapped time is strictly > parent (monotonic, no errTimeNotMonotonic) and ≤ now (never
//     errTimeTooAdvanced).
// It is a pure function of (parentTimestamp, now) so it is idempotent for any two calls in the
// same slot — the property the liveness fix relies on.
func slotSnappedChildTimestamp(parentTimestamp, now time.Time) time.Time {
	ts := now.Truncate(time.Second)
	if ts.Before(parentTimestamp) {
		return parentTimestamp
	}
	if slot := proposer.TimeToSlot(parentTimestamp, ts); slot > 0 {
		return parentTimestamp.Add(time.Duration(slot) * proposer.WindowDuration)
	}
	return ts
}

// Return the child (a *postForkBlock) of this block
func (p *postForkCommonComponents) buildChild(
	ctx context.Context,
	parentID ids.ID,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	parentEpoch chain.Epoch,
) (Block, error) {
	// Child's timestamp is the later of now and this block's timestamp, SLOT-SNAPPED to the
	// parent-anchored proposer-window grid (see slotSnappedChildTimestamp) so repeated rebuilds
	// off a stale parent are byte-identical — the round-scoped view-change liveness fix.
	newTimestamp := slotSnappedChildTimestamp(parentTimestamp, p.vm.Time())

	// The child's P-Chain height is proposed as the optimal P-Chain height that
	// is at least the parent's P-Chain height
	pChainHeight, err := p.vm.selectChildPChainHeight(ctx, parentPChainHeight)
	if err != nil {
		p.vm.logger.Error("unexpected build block failure",
			log.String("reason", "failed to calculate optimal P-chain height"),
			log.Stringer("parentID", parentID),
			log.Err(err),
		)
		return nil, err
	}

	shouldBuildSignedBlock, err := p.shouldBuildSignedBlockPostDurango(
		ctx,
		parentID,
		parentTimestamp,
		parentPChainHeight,
		newTimestamp,
	)
	if err != nil {
		return nil, err
	}

	epoch := lp181.NewEpoch(p.vm.Upgrades, parentPChainHeight, toBlockEpoch(parentEpoch), parentTimestamp, newTimestamp)

	_ = pChainHeight
	contextPChainHeight := epoch.PChainHeight

	var innerBlock chain.Block
	if p.vm.blockBuilderVM != nil {
		builtBlock, err := p.vm.blockBuilderVM.BuildBlockWithRuntime(ctx, &runtime.Runtime{
			PChainHeight: contextPChainHeight,
		})
		if err != nil {
			return nil, err
		}
		innerBlock = builtBlock
	} else {
		engineBlock, err := p.vm.ChainVM.BuildBlock(ctx)
		if err != nil {
			return nil, err
		}
		innerBlock = engineBlock
	}

	// Build the child
	var statelessChild block.SignedBlock
	if shouldBuildSignedBlock {
		if p.vm.StakingMLDSASigner != nil {
			// Strict-PQ: sign with ML-DSA-65 and stamp the raw ML-DSA pubkey so the
			// block's Proposer() == the windower's ML-DSA-keyed election.
			statelessChild, err = block.BuildMLDSA(
				parentID,
				newTimestamp,
				pChainHeight,
				epoch,
				p.vm.StakingMLDSAPub,
				innerBlock.Bytes(),
				p.vm.rt.ChainID,
				p.vm.StakingMLDSASigner,
			)
		} else {
			statelessChild, err = block.Build(
				parentID,
				newTimestamp,
				pChainHeight,
				epoch,
				p.vm.StakingCertLeaf,
				innerBlock.Bytes(),
				p.vm.rt.ChainID,
				p.vm.StakingLeafSigner,
			)
		}
	} else {
		statelessChild, err = block.BuildUnsigned(
			parentID,
			newTimestamp,
			pChainHeight,
			epoch,
			innerBlock.Bytes(),
		)
	}
	if err != nil {
		p.vm.logger.Error("unexpected build block failure",
			log.String("reason", "failed to generate proposervm block header"),
			log.Stringer("parentID", parentID),
			log.Stringer("blkID", innerBlock.ID()),
			log.Err(err),
		)
		return nil, err
	}

	child := &postForkBlock{
		SignedBlock: statelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       p.vm,
			innerBlk: innerBlock,
		},
	}

	p.vm.logger.Info("built block",
		log.Stringer("blkID", child.ID()),
		log.Stringer("innerBlkID", innerBlock.ID()),
		log.Uint64("height", child.Height()),
		log.Uint64("pChainHeight", pChainHeight),
		log.Time("parentTimestamp", parentTimestamp),
		log.Time("blockTimestamp", newTimestamp),
		log.Reflect("epoch", epoch),
	)
	return child, nil
}

func (p *postForkCommonComponents) getInnerBlk() chain.Block {
	return p.innerBlk
}

func (p *postForkCommonComponents) setInnerBlk(innerBlk chain.Block) {
	p.innerBlk = innerBlk
}

// canonicalCommitter (proposervm side) — expose the INNER execution block's
// identity to the consensus engine so it collapses duplicate outer wrappers of ONE
// inner block to ONE consensus object.
//
// WHY THIS EXISTS. Post-Durango, when the chain is stuck the designated proposer
// re-wraps the SAME inner block with a fresh proposer signature / block-timestamp
// each slot — many DISTINCT outer envelope ids over ONE inner block. The consensus
// engine's finality object is the CanonicalID (VotePosition.CanonicalID; the signed
// vote binds it and EXCLUDES the outer id). Until this method existed, no block
// exposed a CanonicalID, so the engine's canonicalIDOf fell back to the OUTER id and
// treated every re-wrap as a distinct fork — votes scattered across the wrappers,
// no α-of-K cert ever formed, and the chain froze (the mainnet 1082879 storm). By
// returning the inner block's id here, every outer wrapper of one inner block yields
// the SAME CanonicalID → they collapse to ONE, votes aggregate, one cert forms.
//
// SAFETY. CanonicalID is derived from the ACTUAL verified inner block (innerBlk is
// execution-verified before the wrapper is tracked), so two wrappers collapse iff
// they genuinely carry the same inner block; two wrappers over DIFFERENT inner
// blocks (a real execution fork) return DIFFERENT CanonicalIDs and are NOT collapsed.
// ParentCanonicalID is the inner PARENT id, so a forked parent (two wrappers of one
// inner parent) also collapses for its children. The execution/payload roots are left
// Empty (the engine treats Empty as "not exposed"); the inner-id collapse is what the
// finality path needs, and Empty roots keep the signed message byte-compatible.
func (p *postForkCommonComponents) CanonicalID() ids.ID        { return p.innerBlk.ID() }
func (p *postForkCommonComponents) ParentCanonicalID() ids.ID  { return p.innerBlk.Parent() }
func (p *postForkCommonComponents) ExecutionStateRoot() ids.ID { return ids.Empty }
func (p *postForkCommonComponents) PayloadRoot() ids.ID        { return ids.Empty }

func verifyIsOracleBlock(ctx context.Context, b chain.Block) error {
	oracle, ok := b.(OracleBlock)
	if !ok {
		return fmt.Errorf(
			"%w: expected block %s to be an OracleBlock but it's a %T",
			errUnexpectedBlockType, b.ID(), b,
		)
	}
	_, err := oracle.Options(ctx)
	return err
}

func verifyIsNotOracleBlock(ctx context.Context, b chain.Block) error {
	oracle, ok := b.(OracleBlock)
	if !ok {
		return nil
	}
	_, err := oracle.Options(ctx)
	switch err {
	case nil:
		return fmt.Errorf(
			"%w: expected block %s not to be an oracle block but it's a %T",
			errUnexpectedBlockType, b.ID(), b,
		)
	case errNotOracle:
		return nil
	default:
		return err
	}
}

func (p *postForkCommonComponents) verifyPostDurangoBlockDelay(
	ctx context.Context,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	blk *postForkBlock,
) (bool, error) {
	var (
		blkTimestamp = blk.Timestamp()
		blkHeight    = blk.Height()
		currentSlot  = proposer.TimeToSlot(parentTimestamp, blkTimestamp)
		proposerID   = blk.Proposer()
	)
	// populate the slot for the block.
	blk.slot = &currentSlot

	// find the expected proposer
	expectedProposerID, err := p.vm.Windower.ExpectedProposer(
		ctx,
		blkHeight,
		parentPChainHeight,
		currentSlot,
	)
	switch {
	case errors.Is(err, proposer.ErrAnyoneCanPropose):
		return false, nil // block should be unsigned
	case err != nil:
		p.vm.logger.Error("unexpected block verification failure",
			log.String("reason", "failed to calculate expected proposer"),
			log.Stringer("blkID", blk.ID()),
			log.Err(err),
		)
		return false, err
	case expectedProposerID == proposerID:
		return true, nil // block should be signed
	default:
		return false, fmt.Errorf("%w: slot %d expects %s", errUnexpectedProposer, currentSlot, expectedProposerID)
	}
}

func (p *postForkCommonComponents) shouldBuildSignedBlockPostDurango(
	ctx context.Context,
	parentID ids.ID,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	newTimestamp time.Time,
) (bool, error) {
	parentHeight := p.innerBlk.Height()
	currentSlot := proposer.TimeToSlot(parentTimestamp, newTimestamp)
	expectedProposerID, err := p.vm.Windower.ExpectedProposer(
		ctx,
		parentHeight+1,
		parentPChainHeight,
		currentSlot,
	)
	switch {
	case errors.Is(err, proposer.ErrAnyoneCanPropose):
		return false, nil // build an unsigned block
	case err != nil:
		p.vm.logger.Error("unexpected build block failure",
			log.String("reason", "failed to calculate expected proposer"),
			log.Stringer("parentID", parentID),
			log.Err(err),
		)
		return false, err
	case expectedProposerID == p.vm.rt.NodeID:
		return true, nil // build a signed block
	}

	// It's not our turn to propose a block yet. This is likely caused by having
	// previously notified the consensus engine to attempt to build a block on
	// top of a block that is no longer the preferred block.
	p.vm.logger.Debug("build block dropped",
		log.Time("parentTimestamp", parentTimestamp),
		log.Time("blockTimestamp", newTimestamp),
		log.Uint64("slot", currentSlot),
		log.Stringer("expectedProposer", expectedProposerID),
	)
	return false, fmt.Errorf("%w: slot %d expects %s", errUnexpectedProposer, currentSlot, expectedProposerID)
}
