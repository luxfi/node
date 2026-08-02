// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// missing_outer_anchor_test.go — THE STRANDED-VALIDATOR DEFECT.
//
// height_backfill_test.go covers the case where the outer index is BEHIND the
// inner tip: an anchor exists, the heights disagree, and repair runs. This file
// covers the case one step worse, where the anchor is ABSENT entirely.
//
// How a live fleet reaches it: `admin_importChain` writes inner EVM blocks
// straight into the chain store and never produces a single proposervm envelope.
// Do that into a wiped node and the inner VM reports a healthy tip while
// State.GetLastAccepted() returns database.ErrNotFound. The pre-fix code read
// that ErrNotFound as "the underlying chain is the only chain and there is
// nothing to repair" and returned nil — so no backfill was recorded, no request
// was made, nothing was logged, and the node sat at its import height forever
// while its peers advanced. Observed on devnet luxd-0/luxd-3 (stuck at 5092
// while the fleet reached 7321) and testnet luxd-1 (stuck at its import height
// while four peers climbed past it).
//
// The distinction that must be preserved: a GENUINELY pre-fork chain also has no
// anchor, and it must still boot normally. The two are told apart by the fork
// height, which is persisted outside the last-accepted pointer.
package proposervm

import (
	"context"
	"testing"
)

// TestMissingOuterAnchor_PostFork_EntersBackfill is the regression for the
// stranded validator. Inner state exists, the fork height proves the chain is
// post-fork, and the outer anchor is gone. Boot must record a pending backfill —
// which also gates BuildBlock off, so a node that cannot name its own outer tip
// never proposes.
//
// FAILS on the pre-fix code (ErrNotFound -> return nil, pending == false).
func TestMissingOuterAnchor_PostFork_EntersBackfill(t *testing.T) {
	ctx := context.Background()
	ic := newInnerChain(t, 8)
	vm := testVM(t, ic)

	// Build a normal post-fork chain so the fork height is recorded the way a
	// real validator records it.
	acceptThroughProposervm(t, vm, ic, 8)
	if _, err := vm.State.GetForkHeight(); err != nil {
		t.Fatalf("precondition: a post-fork chain must have a recorded fork height: %v", err)
	}

	// DAMAGE: delete the outer anchor and the whole height index, leaving the
	// inner VM at 8. This is the on-disk shape of an RLP import into a wiped
	// node — inner history present, proposervm pointer absent.
	if err := vm.State.DeleteLastAccepted(); err != nil {
		t.Fatalf("DeleteLastAccepted: %v", err)
	}
	for h := uint64(1); h <= 8; h++ {
		if err := vm.State.DeleteBlockIDAtHeight(h); err != nil {
			t.Fatalf("DeleteBlockIDAtHeight(%d): %v", h, err)
		}
	}
	if err := vm.db.Commit(); err != nil {
		t.Fatalf("commit damage: %v", err)
	}

	// BOOT. Must not error — a node that cannot repair must still start, loudly,
	// rather than killing the chain.
	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		t.Fatalf("boot with a missing outer anchor must not fail: %v", err)
	}

	from, tip, pending := vm.NeedsOuterBackfill()
	if !pending {
		t.Fatal("MISSING-OUTER-ANCHOR BLIND SPOT: inner state exists at height 8 and the outer " +
			"last-accepted pointer is absent, but boot recorded NO pending backfill. That is the " +
			"silent 'nothing to repair' path that strands an imported validator at its import " +
			"height forever — no backfill state, no request, no log.")
	}
	if tip != 8 {
		t.Fatalf("backfill target must be the inner tip 8, got %d", tip)
	}
	if from == 0 || from > tip {
		t.Fatalf("first missing height must be within (0, tip]; got from=%d tip=%d", from, tip)
	}

	// The build gate is the safety half: a node with a hole must never propose.
	if !vm.outerBackfillPending() {
		t.Fatal("a pending backfill must gate BuildBlock off")
	}
}

// TestMissingOuterAnchor_PreFork_BootsClean is the other side of the branch: a
// chain that has never crossed the proposervm activation boundary has no anchor
// and no fork height, and must still boot with nothing pending. Without this,
// the fix above would turn every legitimately pre-fork chain into a permanently
// build-gated one.
func TestMissingOuterAnchor_PreFork_BootsClean(t *testing.T) {
	ctx := context.Background()
	ic := newInnerChain(t, 5)
	vm := testVM(t, ic)

	// No acceptThroughProposervm: nothing post-fork ever happened, so neither the
	// anchor nor the fork height exists — exactly a pre-fork chain at boot.
	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		t.Fatalf("a pre-fork chain must boot cleanly: %v", err)
	}
	if _, _, pending := vm.NeedsOuterBackfill(); pending {
		t.Fatal("a pre-fork chain has no outer envelopes to backfill — it must not be build-gated")
	}
}

// TestDanglingOuterAnchor_StartsInBackfill is the mainnet-upgrade regression.
//
// A snapshot clone (or any truncated copy) can carry the proposervm's
// last-accepted POINTER while the envelope it names is absent. That branch used
// to `return fmt.Errorf("failed to get last accepted block: …")`, which fails VM
// init — the node logs "error creating required chain" and exits 1, so EVERY
// restart is fatal on a chain that is otherwise intact. Observed on mainnet
// 96369: v1.36.49 exited code 1 twice within ~3 minutes while testnet and
// devnet ran the same image happily, which is what stranded mainnet on v1.36.2
// without any of the consensus fixes.
//
// Starting degraded is strictly better: enterOuterBackfill makes the damage
// visible AND gates BuildBlock off, so the node cannot propose while its index
// is rebuilt.
func TestDanglingOuterAnchor_StartsInBackfill(t *testing.T) {
	ctx := context.Background()
	ic := newInnerChain(t, 6)
	vm := testVM(t, ic)
	acceptThroughProposervm(t, vm, ic, 6)

	// DAMAGE: keep the last-accepted POINTER, destroy the block it names.
	anchor, err := vm.State.GetLastAccepted()
	if err != nil {
		t.Fatalf("precondition: a post-fork chain must have an anchor: %v", err)
	}
	if err := vm.State.DeleteBlock(anchor); err != nil {
		t.Fatalf("DeleteBlock(%s): %v", anchor, err)
	}
	if err := vm.db.Commit(); err != nil {
		t.Fatalf("commit damage: %v", err)
	}

	// Boot MUST NOT fail. This is the whole point: a dangling anchor must not be
	// able to kill chain init.
	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		t.Fatalf("DANGLING ANCHOR KILLED INIT: %v — this is the failure that made every restart fatal "+
			"and stranded mainnet on an old version; it must start degraded instead", err)
	}

	// And it must start DEGRADED, not pretend to be healthy.
	if !vm.outerBackfillPending() {
		t.Fatal("a dangling anchor must enter backfill so BuildBlock is gated off — a node that cannot " +
			"retrieve its own outer tip must never propose")
	}
}
