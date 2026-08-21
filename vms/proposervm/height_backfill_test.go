// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// height_backfill_test.go — RECOVERY: a node whose proposervm finality index is
// ALREADY behind the inner VM tip must BOOT and REPAIR itself instead of killing
// the chain ("non-critical chain failed to initialize chainAlias=C" — a dead
// C-Chain inside a node whose health still reports fine, because the process and
// every other chain on it are up).
//
// Two paths, both OUTER-ONLY (no inner re-execution, no EVM rollback, no resync):
//   - the envelopes for the gap are still in the local block store -> rebuilt at
//     boot with no network and no operator;
//   - they are not -> the chain STARTS anyway, loud, build-gated, and heals from
//     the normal quorum-certificate-gated Accept path.
//
// The shared harness lives in height_lag_repro_test.go.
package proposervm

import (
	"context"
	"testing"

	"github.com/luxfi/ids"
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
	// stays at 8 — the on-disk shape a node boots with when its index trails the
	// inner tip, except the envelopes survive in the block store.
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

// TestOuterBackfill_HealsOnlyThroughAcceptedEnvelopes covers the damaged node's
// live recovery. Parsing untrusted peer bytes cannot advance the finality index;
// only the normal postForkBlock.Accept transition (called by consensus after it
// verifies the weighted quorum certificate) can do so.
func TestOuterBackfill_HealsOnlyThroughAcceptedEnvelopes(t *testing.T) {
	ctx := context.Background()
	ic := newInnerChain(t, 8)
	vm := testVM(t, ic)

	acceptThroughProposervm(t, vm, ic, 5)
	lastGood, err := vm.State.GetLastAccepted()
	if err != nil {
		t.Fatalf("GetLastAccepted: %v", err)
	}

	// DAMAGE: the inner VM ran on to 8 without the index (exactly what the pre-fork
	// fallback did). No envelopes for 6..8 exist locally.
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

	// Merely parsing the next valid signed envelope is not authority to move the
	// pointer. This is the regression against the removed parse-time repair path.
	first := outerFor(t, ic, 6, lastGood)
	if _, err := vm.ParseBlock(ctx, first.Bytes()); err != nil {
		t.Fatalf("ParseBlock(h=6): %v", err)
	}
	if got := indexHeight(t, vm); got != 5 {
		t.Fatalf("parse-only traffic advanced finality index to %d; want 5", got)
	}

	// HEAL — consensus supplies 6, 7, 8 oldest-first and calls Accept only after
	// each wrapper's weighted certificate passes. The inner blocks are already
	// canonical, so the real EVM treats those inner Accept calls as idempotent.
	parent := lastGood
	want := map[uint64]ids.ID{}
	for h := uint64(6); h <= 8; h++ {
		sb := outerFor(t, ic, h, parent)
		blk := &postForkBlock{
			SignedBlock:              sb,
			postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: ic.byHeight[h]},
		}
		vm.Tree.Add(ic.byHeight[h])
		if err := blk.Accept(ctx); err != nil {
			t.Fatalf("certified Accept(h=%d): %v", h, err)
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
// symptom: repairAcceptedChainByHeight must NEVER return an error for a behind
// index, because chains/manager.go turns that error into
// "non-critical chain failed to initialize chainAlias=C" — a dead C-Chain inside a
// node whose health still reports fine.
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

// TestOuterBackfill_ParseTrafficCannotAdvanceFinality proves network input that
// has only parsed (not passed a quorum certificate) has no write-side effect.
func TestOuterBackfill_ParseTrafficCannotAdvanceFinality(t *testing.T) {
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

	// Peer traffic, oldest-first, delivered through the ordinary parse path.
	parent := lastGood
	for h := uint64(6); h <= 8; h++ {
		sb := outerFor(t, ic, h, parent)
		if _, err := vm.ParseBlock(ctx, sb.Bytes()); err != nil {
			t.Fatalf("ParseBlock(h=%d): %v", h, err)
		}
		parent = sb.ID()
	}

	if _, _, pending := vm.NeedsOuterBackfill(); !pending {
		t.Fatal("parse-only traffic must not clear the cert-gated recovery marker")
	}
	if got := indexHeight(t, vm); got != 5 {
		t.Fatalf("parse-only traffic advanced finality index to %d; want 5", got)
	}
	if !vm.outerBackfillPending() {
		t.Fatal("build gate must remain until certified Accept closes the gap")
	}
}
