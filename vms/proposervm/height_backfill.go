// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// height_backfill.go — RECOVERY for a proposervm finality index that sits BELOW
// the inner VM's accepted tip.
//
// THE INVARIANT. Every post-fork accept commits the outer envelope, its height
// index entry and the last-accepted pointer in ONE versiondb batch BEFORE the
// inner block is accepted (vm.acceptPostForkBlock → postForkBlock.Accept), so the
// proposervm index can only ever be AHEAD of the inner VM, never behind.
//
// HOW IT BREAKS ANYWAY. The proposervm has one accept path that advances the inner
// VM while leaving the outer index untouched: preForkBlock.Accept, whose
// acceptOuterBlk() is a no-op by construction. Post-fork that path was reachable
// because BOTH of the proposervm's block constructors fall back to it silently —
// ParseBlock() falls back to parsePreForkBlock when the outer envelope does not
// parse, and getBlock() falls back to getPreForkBlock when the id is not an outer
// id (which is exactly what the CANONICAL id recorded by the consensus ledger is:
// postForkCommonComponents.CanonicalID() == innerBlk.ID()). Either fallback hands
// the engine a preForkBlock wrapping a post-fork inner block; accepting it moves
// the inner VM and NOT the index. Nothing complained, because nothing asserted the
// invariant at the moment it was violated — it was only ever checked at the NEXT
// boot, by which point the node could not start. That is why the lag looked like
// a mysterious "persistence" problem: the writes were never issued at all.
//
// The prevention half lives at the two fallbacks + preForkBlock.acceptOuterBlk
// (see pre_fork_block.go and vm.getPreForkBlock): post-fork, a pre-fork block is
// never constructed and never accepted.
//
// THE RECOVERY half lives here, and it must work for nodes that are ALREADY
// damaged. It is two escalating steps, both OUTER-ONLY — they never re-execute,
// re-verify or re-accept an inner block, so a node whose EVM is already at the tip
// is repaired without touching the EVM:
//
//  1. rebuildOuterIndexFromStore: re-derive the height index and last-accepted
//     pointer from the outer envelopes ALREADY in this node's block store, binding
//     each candidate to the inner block this node itself accepted at that height.
//     Fully local, no network, no operator. This is what turns "refuses to start"
//     into "starts, repairs itself, and keeps its finality pointer".
//
//  2. If step 1 cannot reach the inner tip, the node still STARTS, in an explicit
//     backfill-pending state: loud, fail-safe (it will not BUILD a block while its
//     finality index is incomplete) and repairable through BackfillOuterBlock,
//     which accepts the missing envelopes from any source and applies the SAME
//     binding checks. The alternative — the previous behaviour — was a dead
//     C-Chain and a destructive resync.
//
// WHAT IS NEVER DONE, and why: the finality pointer is never dropped
// (DeleteLastAccepted). proposervm.LastAccepted would then fall back to an
// INNER-namespace id whose ParentID is contiguity-incompatible with the network's
// OUTER wrappers, permanently wedging bootstrap, catch-up and live Verify at the
// inner tip — a silent wedge strictly worse than the loud stop it would replace.
// Every path below only ever moves the pointer FORWARD, onto an envelope that was
// proven to wrap the inner block this node already accepted at that height.
package proposervm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"

	statelessblock "github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/state"
)

var (
	// ErrOuterBackfillNotPending is returned by BackfillOuterBlock when the index
	// is already whole — the repair is idempotent, not an error to call twice.
	ErrOuterBackfillNotPending = errors.New("proposervm: no outer-index backfill is pending")

	// ErrOuterBackfillRejected is returned when a candidate envelope does not bind
	// to this node's own accepted chain. Fail-closed: an envelope is only indexed
	// when it provably wraps the inner block WE accepted at that height and chains
	// off the envelope WE already hold at height-1, so a peer (or a stale file)
	// cannot steer the finality pointer.
	ErrOuterBackfillRejected = errors.New("proposervm: outer backfill candidate rejected")

	// errPreForkAfterFork is the prevention guard: post-fork, a block at or above
	// the recorded fork height is NEVER a pre-fork block. Constructing or accepting
	// one is what silently strands the finality index below the inner tip.
	errPreForkAfterFork = errors.New("proposervm: refusing a pre-fork block at or above the fork height")
)

// outerBackfill is the state of an incomplete outer index. Zero value = whole.
//
// It is guarded by its own leaf mutex (vm.backfillMu) rather than vm.lock: the
// accept path, the build path and the operator repair path all read it, and it is
// never held across a callout, so it cannot participate in a deadlock.
type outerBackfill struct {
	// next is the FIRST height whose outer envelope is missing. It advances as
	// envelopes land, so it doubles as the resume point across restarts.
	next uint64
	// tip is the inner VM's accepted height — the height the index must reach.
	tip uint64
	// innerTipID is the inner block at [tip], recorded for diagnostics.
	innerTipID ids.ID
}

// NeedsOuterBackfill reports the still-missing outer height range [from, to] and
// whether a backfill is pending. It is the seam an operator tool or a node-layer
// fetch driver reads; when pending is false the index is whole.
func (vm *VM) NeedsOuterBackfill() (from uint64, to uint64, pending bool) {
	vm.backfillMu.RLock()
	defer vm.backfillMu.RUnlock()
	if vm.backfill == nil {
		return 0, 0, false
	}
	return vm.backfill.next, vm.backfill.tip, true
}

// outerBackfillPending is the internal fast check used by the build gate.
func (vm *VM) outerBackfillPending() bool {
	vm.backfillMu.RLock()
	defer vm.backfillMu.RUnlock()
	return vm.backfill != nil
}

// innerBlockAtHeight returns the inner VM's ACCEPTED block at [height] together
// with its bytes — the binding target every repair candidate must match. It reads
// the inner VM's own height index, so it is the inner VM's truth, not ours.
func (vm *VM) innerBlockAtHeight(ctx context.Context, height uint64) (ids.ID, []byte, error) {
	innerID, err := vm.ChainVM.GetBlockIDAtHeight(ctx, height)
	if err != nil {
		return ids.Empty, nil, err
	}
	innerBlk, err := vm.ChainVM.GetBlock(ctx, innerID)
	if err != nil {
		return ids.Empty, nil, err
	}
	return innerID, innerBlk.Bytes(), nil
}

// indexOuterAtHeight commits ONE outer envelope into the finality index: the
// envelope itself, its height entry, and the last-accepted pointer, in a single
// versiondb batch — byte-for-byte what acceptPostForkBlock writes on the live
// path. It deliberately does NOT touch the inner VM.
func (vm *VM) indexOuterAtHeight(height uint64, blk statelessblock.Block) error {
	if err := vm.State.PutBlock(blk); err != nil {
		return err
	}
	if err := vm.State.SetBlockIDAtHeight(height, blk.ID()); err != nil {
		return err
	}
	if err := vm.State.SetLastAccepted(blk.ID()); err != nil {
		return err
	}
	return vm.db.Commit()
}

// rebuildOuterIndexFromStore walks the gap (proHeight, innerHeight] forward,
// re-indexing each height from an outer envelope ALREADY present in this node's
// block store.
//
// Soundness. A candidate at height h is accepted ONLY if
//
//	envelope.Block()    == the bytes of the inner block WE accepted at h, and
//	envelope.ParentID() == the envelope we just indexed at h-1
//
// so the rebuilt chain is the unique outer chain that wraps our own accepted inner
// chain. Nothing is inferred and nothing is fabricated: an unmatched height stops
// the walk immediately.
//
// Returns the height the index reached (>= proHeight). A return equal to
// innerHeight means the index is whole again.
func (vm *VM) rebuildOuterIndexFromStore(
	ctx context.Context,
	proHeight uint64,
	proID ids.ID,
	innerHeight uint64,
) (uint64, error) {
	// One pass over the block store: inner-bytes digest -> outer envelope. Keyed by
	// digest rather than by inner id because deriving an inner id from bytes would
	// mean parsing untrusted bytes through the inner VM; the digest comparison is
	// exact and side-effect free.
	byInnerDigest := make(map[[sha256.Size]byte]statelessblock.Block)
	if err := state.ScanBlocks(vm.db, func(blk statelessblock.Block) error {
		byInnerDigest[sha256.Sum256(blk.Block())] = blk
		return nil
	}); err != nil {
		return proHeight, fmt.Errorf("failed to scan the proposervm block store: %w", err)
	}

	reached := proHeight
	parentID := proID
	for h := proHeight + 1; h <= innerHeight; h++ {
		_, innerBytes, err := vm.innerBlockAtHeight(ctx, h)
		if err != nil {
			vm.logger.Warn("proposervm outer-index rebuild: inner block unreadable, stopping walk",
				log.Uint64("height", h), log.Err(err))
			break
		}
		candidate, ok := byInnerDigest[sha256.Sum256(innerBytes)]
		if !ok || candidate.ParentID() != parentID {
			break
		}
		if err := vm.indexOuterAtHeight(h, candidate); err != nil {
			return reached, fmt.Errorf("failed to re-index outer block at height %d: %w", h, err)
		}
		reached = h
		parentID = candidate.ID()
	}
	return reached, nil
}

// enterOuterBackfill records an incomplete index and lets the VM start anyway.
// Fail-safe, not fail-open: BuildBlock is gated off while pending (a node whose
// finality index has a hole must never PROPOSE), the height index still reports
// ErrNotFound inside the gap so nothing reads a wrong id, and the last-accepted
// pointer is left exactly where the rebuild got to.
func (vm *VM) enterOuterBackfill(from, tip uint64, innerTipID ids.ID) {
	vm.backfillMu.Lock()
	vm.backfill = &outerBackfill{next: from, tip: tip, innerTipID: innerTipID}
	vm.backfillMu.Unlock()

	vm.logger.Warn("proposervm STARTING WITH AN INCOMPLETE FINALITY INDEX — outer backfill pending",
		log.Uint64("firstMissingHeight", from),
		log.Uint64("innerTipHeight", tip),
		log.Stringer("innerTipID", innerTipID),
		log.String("effect", "this chain will NOT build blocks until the missing outer envelopes are supplied"),
		log.String("recovery", "feed the outer proposervm envelopes for the missing heights via "+
			"BackfillOuterBlock (cmd/repair-proposervm backfill) — each is bound to the inner block "+
			"this node already accepted, so no EVM re-execution and no resync are required"),
	)
}

// BackfillOuterBlock supplies ONE missing outer envelope, oldest-first, and is the
// operator/driver half of the recovery. It is fail-closed and idempotent:
//
//   - not pending                       -> ErrOuterBackfillNotPending
//   - envelope below the next height     -> nil (already indexed; tolerate replay)
//   - signature/format invalid           -> error (parsed WITH verification)
//   - not the next height, wrong parent,
//     or wrapping a different inner block-> ErrOuterBackfillRejected
//
// On the final height it clears the pending state and refreshes the in-memory
// last-accepted metadata, after which the chain behaves exactly like one that
// booted whole.
func (vm *VM) BackfillOuterBlock(ctx context.Context, outerBytes []byte) error {
	vm.backfillMu.Lock()
	defer vm.backfillMu.Unlock()

	if vm.backfill == nil {
		return ErrOuterBackfillNotPending
	}
	height := vm.backfill.next

	// Parse WITH proposer-signature verification: the envelope must be a real
	// block of THIS chain, not merely well-formed bytes.
	blk, err := statelessblock.Parse(outerBytes, vm.rt.ChainID)
	if err != nil {
		return fmt.Errorf("%w: envelope did not parse/verify: %w", ErrOuterBackfillRejected, err)
	}

	// BIND 1 — the envelope must chain off the one we already hold.
	parentID, err := vm.State.GetLastAccepted()
	if err != nil {
		return fmt.Errorf("%w: cannot read the current finality pointer: %w", ErrOuterBackfillRejected, err)
	}
	if blk.ParentID() != parentID {
		return fmt.Errorf("%w: envelope parent %s != current finality pointer %s (feed oldest-first)",
			ErrOuterBackfillRejected, blk.ParentID(), parentID)
	}

	// BIND 2 — the envelope must wrap the inner block WE accepted at this height.
	// This is what makes accepting bytes from an untrusted source safe.
	innerID, innerBytes, err := vm.innerBlockAtHeight(ctx, height)
	if err != nil {
		return fmt.Errorf("%w: inner block at height %d unreadable: %w", ErrOuterBackfillRejected, height, err)
	}
	if !bytes.Equal(blk.Block(), innerBytes) {
		return fmt.Errorf("%w: envelope at height %d does not wrap our accepted inner block %s",
			ErrOuterBackfillRejected, height, innerID)
	}

	if err := vm.indexOuterAtHeight(height, blk); err != nil {
		return err
	}

	vm.logger.Info("proposervm outer backfill: height repaired",
		log.Uint64("height", height),
		log.Stringer("outerID", blk.ID()),
		log.Uint64("remaining", vm.backfill.tip-height),
	)

	if height >= vm.backfill.tip {
		tip := vm.backfill.tip
		vm.backfill = nil
		if err := vm.setLastAcceptedMetadata(ctx); err != nil {
			return fmt.Errorf("outer backfill completed at height %d but last-accepted metadata failed: %w", tip, err)
		}
		vm.logger.Info("proposervm FINALITY INDEX FULLY REPAIRED — backfill complete",
			log.Uint64("height", tip))
		return nil
	}
	vm.backfill.next = height + 1
	return nil
}
