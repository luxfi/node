// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// height_backfill.go — RECOVERY for a proposervm finality index that sits BELOW
// the inner VM's accepted tip.
//
// THE INVARIANT. A post-fork accept first commits the inner execution block and
// then commits the outer envelope, height index, and last-accepted pointer in one
// versiondb batch (postForkBlock.Accept). A failure can therefore leave the outer
// index behind, but it must never leave the outer index ahead of execution.
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
//     backfill-pending state: loud and fail-safe (it will not BUILD a block while
//     its finality index is incomplete). The normal quorum-certificate-gated
//     Accept path then replays the missing wrappers oldest-first. The inner VM
//     recognizes only its exact historical canonical blocks as idempotent, and
//     each successful outer accept advances this marker. There is no separate
//     unsigned repair admission path.
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
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"

	statelessblock "github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/state"
)

var errPreForkAfterFork = errors.New("proposervm: refusing a pre-fork block at or above the fork height")

// outerBackfill is the state of an incomplete outer index. Zero value = whole.
//
// It is guarded by its own leaf mutex (vm.backfillMu) rather than vm.lock: the
// accept path and build path both read it, and it is
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
// whether a backfill is pending. When pending is false the index is whole.
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
		log.String("recovery", "the normal quorum-certificate catch-up path will accept the missing "+
			"outer envelopes; the EVM permits a no-op only for the exact canonical inner block it "+
			"already accepted at that height"),
	)
}

// noteOuterAccepted advances recovery state after the ONE authoritative wrapper
// transition: postForkBlock.Accept. That call is reached by catch-up only after a
// weighted quorum certificate has been verified. Parsing or merely holding an
// envelope can never move this marker.
func (vm *VM) noteOuterAccepted(height uint64) {
	vm.backfillMu.Lock()
	defer vm.backfillMu.Unlock()

	if vm.backfill == nil {
		return
	}
	if height != vm.backfill.next {
		return
	}
	if height >= vm.backfill.tip {
		tip := vm.backfill.tip
		vm.backfill = nil
		vm.logger.Info("proposervm FINALITY INDEX FULLY REPAIRED — backfill complete",
			log.Uint64("height", tip))
		return
	}
	vm.backfill.next = height + 1
}
