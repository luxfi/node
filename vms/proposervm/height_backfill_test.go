// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// height_backfill_test.go — RECOVERY: a node whose proposervm finality index is
// ALREADY behind the inner VM tip must BOOT and REPAIR itself instead of killing
// the chain ("non-critical chain failed to initialize chainAlias=C" — a dead
// C-Chain on a pod that still reports 1/1 Ready).
//
// Two paths, both OUTER-ONLY (no inner re-execution, no EVM rollback, no resync):
//   - the envelopes for the gap are still in the local block store -> rebuilt at
//     boot with no network and no operator;
//   - they are not -> the chain STARTS anyway, loud, build-gated, and heals from
//     supplied envelopes through BackfillOuterBlock.
//
// The shared harness lives in height_lag_repro_test.go.
package proposervm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/luxfi/ids"

	statelessblock "github.com/luxfi/node/vms/proposervm/block"
)

// ---------------------------------------------------------------------------
// 2. RECOVERY — a node whose index is ALREADY behind boots and heals.
// ---------------------------------------------------------------------------

// TestOuterIndexRebuild_FromLocalStore_BootsAndHeals: the damaged node still holds
// the outer envelopes for the gap in its own block store (the truncated-index /
// inconsistently-restored-snapshot case). Boot must rebuild the index from them —
// no network, no operator, no resync — and must NOT return an error.
func TestOuterIndexRebuild_FromLocalStore_BootsAndHeals(t *testing.T) {
	ctx := context.Background()
	ic := newInnerChain(t, 8)
	vm := testVM(t, ic)

	outers := acceptThroughProposervm(t, vm, ic, 8)

	// DAMAGE: roll the finality pointer + height index back to 5 while the inner VM
	// stays at 8 — byte-for-byte the on-disk state luxd-3 booted with (index 6,
	// inner 98), except the envelopes survive in the block store.
	if err := vm.State.SetLastAccepted(outers[5].ID()); err != nil {
		t.Fatalf("SetLastAccepted: %v", err)
	}
	for h := uint64(6); h <= 8; h++ {
		if err := vm.State.DeleteBlockIDAtHeight(h); err != nil {
			t.Fatalf("DeleteBlockIDAtHeight(%d): %v", h, err)
		}
	}
	if err := vm.db.Commit(); err != nil {
		t.Fatalf("commit damage: %v", err)
	}
	if got := indexHeight(t, vm); got != 5 {
		t.Fatalf("precondition: damaged index must read 5, got %d", got)
	}

	// BOOT.
	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		t.Fatalf("boot with a behind index must REPAIR, not fail: %v", err)
	}

	if _, _, pending := vm.NeedsOuterBackfill(); pending {
		t.Fatal("the local store held every missing envelope — no backfill should be pending")
	}
	if got := indexHeight(t, vm); got != 8 {
		t.Fatalf("index must be rebuilt to the inner tip 8, got %d", got)
	}
	lastAccepted, err := vm.State.GetLastAccepted()
	if err != nil || lastAccepted != outers[8].ID() {
		t.Fatalf("finality pointer must be the canonical envelope at 8 (%s), got %s (err %v)",
			outers[8].ID(), lastAccepted, err)
	}
	for h := uint64(6); h <= 8; h++ {
		got, err := vm.State.GetBlockIDAtHeight(h)
		if err != nil || got != outers[h].ID() {
			t.Fatalf("height index at %d must be %s, got %s (err %v)", h, outers[h].ID(), got, err)
		}
	}
}

// TestOuterBackfill_HealsFromSuppliedEnvelopes: the envelopes for the gap are NOT
// in the local store (the pre-fork-fallback case — they were never persisted).
// The node must still START — the old code killed the C-Chain here — come up
// explicitly backfill-pending, refuse to build, and then heal from supplied
// envelopes with no EVM re-execution.
func TestOuterBackfill_HealsFromSuppliedEnvelopes(t *testing.T) {
	ctx := context.Background()
	ic := newInnerChain(t, 8)
	vm := testVM(t, ic)

	acceptThroughProposervm(t, vm, ic, 5)
	lastGood, err := vm.State.GetLastAccepted()
	if err != nil {
		t.Fatalf("GetLastAccepted: %v", err)
	}

	// DAMAGE: the inner VM ran on to 8 without the index (exactly what the pre-fork
	// fallback did in production). No envelopes for 6..8 exist locally.
	ic.accept(8)

	// BOOT — must not error.
	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		t.Fatalf("boot with a behind index must START the chain, got: %v", err)
	}
	from, to, pending := vm.NeedsOuterBackfill()
	if !pending || from != 6 || to != 8 {
		t.Fatalf("want backfill pending 6..8, got from=%d to=%d pending=%v", from, to, pending)
	}

	// FAIL-SAFE: a node with a hole in its finality index must not propose.
	if _, err := vm.BuildBlock(ctx); err == nil {
		t.Fatal("BuildBlock must refuse while the finality index is incomplete")
	}

	// NEGATIVE 1 — an envelope wrapping a DIFFERENT inner block than the one we
	// accepted at this height is rejected (a peer cannot steer the pointer).
	bogus, err := statelessblock.BuildUnsigned(
		lastGood, time.Unix(6, 0), 0, statelessblock.Epoch{}, []byte("not-our-inner-block"))
	if err != nil {
		t.Fatalf("BuildUnsigned: %v", err)
	}
	if err := vm.BackfillOuterBlock(ctx, bogus.Bytes()); !errors.Is(err, ErrOuterBackfillRejected) {
		t.Fatalf("an envelope that does not wrap our accepted inner block must be rejected, got %v", err)
	}

	// NEGATIVE 2 — out of order (height 7 before 6) is rejected: it does not chain
	// off the current finality pointer.
	outOfOrder := outerFor(t, ic, 7, lastGood)
	if err := vm.BackfillOuterBlock(ctx, outOfOrder.Bytes()); !errors.Is(err, ErrOuterBackfillRejected) {
		t.Fatalf("an out-of-order envelope must be rejected, got %v", err)
	}

	// HEAL — feed 6, 7, 8 oldest-first, as a healthy peer or the repair tool would.
	parent := lastGood
	want := map[uint64]ids.ID{}
	for h := uint64(6); h <= 8; h++ {
		sb := outerFor(t, ic, h, parent)
		if err := vm.BackfillOuterBlock(ctx, sb.Bytes()); err != nil {
			t.Fatalf("BackfillOuterBlock(h=%d): %v", h, err)
		}
		want[h] = sb.ID()
		parent = sb.ID()
	}

	if _, _, pending := vm.NeedsOuterBackfill(); pending {
		t.Fatal("backfill must be complete after the last height")
	}
	if got := indexHeight(t, vm); got != 8 {
		t.Fatalf("index must reach the inner tip 8, got %d", got)
	}
	for h := uint64(6); h <= 8; h++ {
		got, err := vm.State.GetBlockIDAtHeight(h)
		if err != nil || got != want[h] {
			t.Fatalf("height index at %d must be %s, got %s (err %v)", h, want[h], got, err)
		}
	}
	// Idempotent: nothing pending, so a replayed call says so rather than corrupting.
	if err := vm.BackfillOuterBlock(ctx, outOfOrder.Bytes()); !errors.Is(err, ErrOuterBackfillNotPending) {
		t.Fatalf("want ErrOuterBackfillNotPending after completion, got %v", err)
	}
	// The chain is a full participant again.
	if _, _, pending := vm.NeedsOuterBackfill(); pending {
		t.Fatal("build gate must have lifted")
	}
	// And a fresh boot on the repaired state is a clean no-op.
	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		t.Fatalf("boot after repair must be a no-op, got %v", err)
	}
}

// TestBootWithBehindIndex_NeverKillsTheChain is the direct regression lock on the
// production symptom: repairAcceptedChainByHeight must NEVER return an error for a
// behind index, because chains/manager.go turns that error into
// "non-critical chain failed to initialize chainAlias=C" — a dead C-Chain on a pod
// that still reports 1/1 Ready.
func TestBootWithBehindIndex_NeverKillsTheChain(t *testing.T) {
	ctx := context.Background()
	for _, gap := range []uint64{1, 2, 6, 91} {
		ic := newInnerChain(t, 100)
		vm := testVM(t, ic)
		acceptThroughProposervm(t, vm, ic, 100-gap)
		ic.accept(100)

		if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
			t.Fatalf("gap %d: boot must not fail, got %v", gap, err)
		}
		from, to, pending := vm.NeedsOuterBackfill()
		if !pending || from != 100-gap+1 || to != 100 {
			t.Fatalf("gap %d: want pending %d..100, got %d..%d pending=%v", gap, 100-gap+1, from, to, pending)
		}
	}
}

func mustForkHeight(t *testing.T, vm *VM) uint64 {
	t.Helper()
	h, err := vm.State.GetForkHeight()
	if err != nil {
		t.Fatalf("GetForkHeight: %v", err)
	}
	return h
}

// TestOuterBackfill_SelfHealsFromOrdinaryParseTraffic proves the loop closes with
// no new transport and no operator: every envelope a peer serves passes through
// VM.ParseBlock, and a backfill-pending node absorbs exactly the ones it needs.
// This is what lets an ALREADY-DAMAGED node come back by itself.
func TestOuterBackfill_SelfHealsFromOrdinaryParseTraffic(t *testing.T) {
	ctx := context.Background()
	ic := newInnerChain(t, 8)
	vm := testVM(t, ic)

	acceptThroughProposervm(t, vm, ic, 5)
	lastGood, err := vm.State.GetLastAccepted()
	if err != nil {
		t.Fatalf("GetLastAccepted: %v", err)
	}
	ic.accept(8) // the inner VM ran on without the index

	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		t.Fatalf("boot must start the chain, got %v", err)
	}
	if _, _, pending := vm.NeedsOuterBackfill(); !pending {
		t.Fatal("precondition: backfill must be pending")
	}

	// Peer traffic, oldest-first, delivered through the ORDINARY parse path.
	parent := lastGood
	for h := uint64(6); h <= 8; h++ {
		sb := outerFor(t, ic, h, parent)
		if _, err := vm.ParseBlock(ctx, sb.Bytes()); err != nil {
			t.Fatalf("ParseBlock(h=%d): %v", h, err)
		}
		parent = sb.ID()
	}

	if _, _, pending := vm.NeedsOuterBackfill(); pending {
		t.Fatal("the node must have healed itself from ordinary parse traffic")
	}
	if got := indexHeight(t, vm); got != 8 {
		t.Fatalf("index must reach the inner tip 8, got %d", got)
	}
	// The build gate lifted with the heal (the gate itself is asserted in
	// TestOuterBackfill_HealsFromSuppliedEnvelopes; driving the real builder here
	// would need a validator set this harness deliberately does not stand up).
	if _, _, pending := vm.NeedsOuterBackfill(); pending {
		t.Fatal("build gate must have lifted")
	}
}
