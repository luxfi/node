// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// vm_crashboot_test.go — CRASH-COPY RECOVERY ARMOR.
//
// Every recovery test before this file repaired the SAME warm VM value: its
// Tree, verifiedBlocks map and caches were still populated, so a repair that
// silently leaned on in-memory state would still have passed. These tests
// remove that crutch. They copy the COMMITTED bytes out from under a RUNNING
// proposervm — no Shutdown, no extra Commit beyond what the accept path itself
// performed — and boot a SECOND, cold VM over the copy: a process kill plus
// restart, byte-faithful. The recovered VM must
//
//  1. BOOT — repairAcceptedChainByHeight then setLastAcceptedMetadata, the
//     Initialize order — without error;
//  2. AGREE with the source's committed view at the copy instant: finality
//     pointer, fork height, and an openable envelope at EVERY height; and
//  3. BUILD — produce a child whose outer parent is the recovered tip and
//     whose inner parent is that tip's inner block. This is the
//     errInnerParentMismatch invariant (build_inner_parent_test.go) proven
//     from a cold boot on persisted state, not just at steady state.
//
// The matrix pins the persistence boundaries: nothing accepted yet, exactly
// one accept (the fork-height record), a longer run, and a copy taken while
// the source keeps accepting (the copy must be an immutable snapshot).
// The final test also pins recovery of a LEGACY/restored state whose outer index
// is one height ahead of the inner VM. Current code cannot create that state
// because postForkBlock.Accept is inner-first, but old data must remain bootable.
//
// The shared harness lives in height_lag_repro_test.go.
package proposervm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	validatorstest "github.com/luxfi/validators/validatorstest"
	vmchain "github.com/luxfi/vm/chain"
	"github.com/luxfi/vm/chain/blocktest"
)

// crashCopy copies every committed key/value out of a live base database into
// a fresh memdb — a byte-level snapshot taken with the source still running.
// This is exactly what a crash leaves behind: versiondb buffers uncommitted
// writes in memory, and they die with the process.
func crashCopy(t *testing.T, base database.Database) *memdb.Database {
	t.Helper()
	cp := memdb.New()
	it := base.NewIterator()
	defer it.Release()
	for it.Next() {
		if err := cp.Put(it.Key(), it.Value()); err != nil {
			t.Fatalf("crashCopy Put: %v", err)
		}
	}
	if err := it.Error(); err != nil {
		t.Fatalf("crashCopy iterator: %v", err)
	}
	return cp
}

// snapshot clones the inner chain as its own database would survive a crash at
// this instant: every block ever inserted (verified-but-unaccepted included,
// as the real EVM persists bodies on insert) with the acceptance tip pinned at
// [tip]. The clone is immune to the source's subsequent progress.
func (ic *innerChain) snapshot(tip uint64) *innerChain {
	cp := &innerChain{
		byHeight: make(map[uint64]*blocktest.Block, len(ic.byHeight)),
		byID:     make(map[ids.ID]*blocktest.Block, len(ic.byID)),
		byBytes:  make(map[string]*blocktest.Block, len(ic.byBytes)),
		tip:      tip,
	}
	for h, b := range ic.byHeight {
		cp.byHeight[h] = b
	}
	for id, b := range ic.byID {
		cp.byID[id] = b
	}
	for s, b := range ic.byBytes {
		cp.byBytes[s] = b
	}
	return cp
}

// buildableVM is ic.vm() plus the two calls a build needs. SetPreference is what
// anchorInnerBuildParent reaches: it moves the head exactly as the EVM does.
// BuildBlock mints a FRESH child of the head (building is not accepting, and a
// replacement proposal after a crash is a new block, not the lost one).
func (ic *innerChain) buildableVM() *blocktest.VM {
	vm := ic.vm()
	vm.SetPreferenceF = func(_ context.Context, id ids.ID) error {
		blk, ok := ic.byID[id]
		if !ok {
			return errors.New("inner: preference not in chain")
		}
		ic.tip = blk.HeightV
		return nil
	}
	vm.BuildBlockF = func(context.Context) (vmchain.Block, error) {
		parent := ic.byHeight[ic.tip]
		id := ids.GenerateTestID()
		child := &blocktest.Block{
			IDV:        id,
			ParentV:    parent.IDV,
			HeightV:    parent.HeightV + 1,
			BytesV:     append([]byte("inner"), id[:]...),
			TimestampV: parent.TimestampV.Add(time.Second),
		}
		ic.byID[id] = child
		ic.byBytes[string(child.BytesV)] = child
		return child, nil
	}
	return vm
}

// bootRecoveredVM stands a COLD VM up over copied bytes and runs the boot
// sequence in Initialize order. Windower/validator-state/no-op wiring matches
// build_inner_parent_test.go: every slot open, so builds take the unsigned
// path and the recovery properties are the only thing under test.
func bootRecoveredVM(t *testing.T, ic *innerChain, base database.Database) *VM {
	t.Helper()
	vm := testVMOnBase(t, ic, base)
	vm.ChainVM = ic.buildableVM()
	vm.Windower = anyoneCanProposeWindower{}
	vm.validatorState = &validatorstest.State{
		GetCurrentHeightF: func(context.Context) (uint64, error) { return 0, nil },
	}
	if err := vm.repairAcceptedChainByHeight(context.Background()); err != nil {
		t.Fatalf("BOOT KILLED THE CHAIN (repair): %v", err)
	}
	if err := vm.setLastAcceptedMetadata(context.Background()); err != nil {
		t.Fatalf("BOOT KILLED THE CHAIN (metadata): %v", err)
	}
	return vm
}

// mustBuildOnRecoveredTip drives the recovered VM's REAL public build path —
// SetPreference(last accepted) then BuildBlock, what the engine does after a
// restart — and asserts the built child extends the recovered tip on BOTH
// layers: outer parent == the tip envelope, inner parent == the tip's inner
// block. A recovered node failing either would reject its own proposal.
func mustBuildOnRecoveredTip(t *testing.T, vm *VM, ic *innerChain) {
	t.Helper()
	require := require.New(t)
	ctx := context.Background()

	// Deterministic clock strictly after the recovered tip's timestamp
	// (harness blocks carry time.Unix(height, 0)).
	vm.Clock.Set(time.Unix(int64(ic.tip)+2, 0))

	lastAccepted, err := vm.LastAccepted(ctx)
	require.NoError(err)
	require.NoError(vm.SetPreference(ctx, lastAccepted),
		"the recovered tip must be fetchable — SetPreference validates before assigning")

	built, err := vm.BuildBlock(ctx)
	require.NoError(err, "a crash-recovered node must be able to build")
	child, ok := built.(*postForkBlock)
	require.True(ok, "the child of a recovered tip is a post-fork envelope")

	wantInnerParent := ic.byHeight[ic.tip].IDV
	require.Equal(wantInnerParent, child.innerBlk.Parent(),
		"the built child's inner parent must be the recovered tip's inner block "+
			"(errInnerParentMismatch invariant, from a cold boot)")
	require.Equal(ic.tip+1, child.Height())
	require.Equal(lastAccepted, child.Parent(),
		"the built child's outer parent must be the recovered finality pointer")
}

// TestCrashBoot_ColdVMOverDirtyCopy_AgreesAndBuilds is the persistence-boundary
// matrix: commit-state copies taken with NO shutdown, each booted by a second,
// cold VM that must agree with the source's committed view at the copy instant
// and then build on it.
func TestCrashBoot_ColdVMOverDirtyCopy_AgreesAndBuilds(t *testing.T) {
	for _, tt := range []struct {
		name       string
		accepted   uint64 // heights accepted through the proposervm before the copy
		runOnAfter uint64 // heights the SOURCE keeps accepting after the copy
	}{
		// Nothing ever accepted through the proposervm: no fork height, no
		// outer state. Boot must be a no-op and the pre-fork world intact.
		{name: "nothing_accepted", accepted: 0, runOnAfter: 0},
		// Exactly one accept — the boundary that records the fork height.
		{name: "first_accept_records_fork", accepted: 1, runOnAfter: 0},
		{name: "several_accepted", accepted: 8, runOnAfter: 0},
		// The copy is taken and the source RUNS ON: later accepts must not
		// leak into the recovered view (the copy is an immutable snapshot).
		{name: "copy_while_source_runs_on", accepted: 5, runOnAfter: 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()

			ic := newInnerChain(t, tt.accepted+tt.runOnAfter+2)
			base := memdb.New()
			src := testVMOnBase(t, ic, base)
			outers := acceptThroughProposervm(t, src, ic, tt.accepted)

			// THE COPY — no Shutdown, the source stays live. Snapshot both
			// layers at the same instant, as one machine's disk would be.
			cp := crashCopy(t, base)
			icAtCopy := ic.snapshot(ic.tip)

			if tt.runOnAfter > 0 {
				acceptRangeThroughProposervm(
					t, src, ic, tt.accepted+1, tt.accepted+tt.runOnAfter, outers[tt.accepted].ID())
				if got := indexHeight(t, src); got != tt.accepted+tt.runOnAfter {
					t.Fatalf("source must have run on to %d, at %d", tt.accepted+tt.runOnAfter, got)
				}
			}

			vm2 := bootRecoveredVM(t, icAtCopy, cp)

			if tt.accepted == 0 {
				// Pre-fork world: no outer state exists, none may be invented.
				_, err := vm2.State.GetLastAccepted()
				require.ErrorIs(err, database.ErrNotFound)
				_, err = vm2.State.GetForkHeight()
				require.ErrorIs(err, database.ErrNotFound)
				// LastAccepted falls through to the inner chain.
				la, err := vm2.LastAccepted(ctx)
				require.NoError(err)
				require.Equal(icAtCopy.byHeight[icAtCopy.tip].IDV, la)
			} else {
				// The recovered finality pointer is the copy-instant tip —
				// not genesis, not the source's later progress.
				got, err := vm2.State.GetLastAccepted()
				require.NoError(err)
				require.Equal(outers[tt.accepted].ID(), got)
				require.Equal(mustForkHeight(t, src), mustForkHeight(t, vm2))

				// Every height's envelope is indexed AND openable cold.
				for h := uint64(1); h <= tt.accepted; h++ {
					id, err := vm2.State.GetBlockIDAtHeight(h)
					require.NoError(err, "height %d must be indexed", h)
					require.Equal(outers[h].ID(), id, "height %d", h)
					blk, err := vm2.getPostForkBlock(ctx, id)
					require.NoError(err, "envelope at height %d must be openable on the recovered VM", h)
					require.Equal(h, blk.Height())
				}
			}
			if _, _, pending := vm2.NeedsOuterBackfill(); pending {
				t.Fatal("a lock-step copy has no gap — no backfill may be pending")
			}

			mustBuildOnRecoveredTip(t, vm2, icAtCopy)
		})
	}
}

// TestCrashBoot_OuterCommittedInnerNot_RollsBackAndBuilds pins compatibility
// with legacy/restored bytes whose outer pointer is one height ahead of the
// inner VM. Current inner-first acceptance cannot create this state, but boot
// must still roll the pointer back and keep building.
func TestCrashBoot_OuterCommittedInnerNot_RollsBackAndBuilds(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	ic := newInnerChain(t, 8)
	base := memdb.New()
	src := testVMOnBase(t, ic, base)
	outers := acceptThroughProposervm(t, src, ic, 4)
	require.Equal(uint64(4), ic.tip, "precondition: lock-step through 4")

	// Construct the legacy/restored mismatch: persist height 5's outer envelope…
	sb := outerFor(t, ic, 5, outers[4].ID())
	blk := &postForkBlock{
		SignedBlock:              sb,
		postForkCommonComponents: postForkCommonComponents{vm: src, innerBlk: ic.byHeight[5]},
	}
	src.Tree.Add(ic.byHeight[5])
	require.NoError(blk.Accept(ctx))
	// …while the copied inner snapshot remains at 4 (no ic.accept(5)).

	cp := crashCopy(t, base)
	icAtCopy := ic.snapshot(4)
	vm2 := bootRecoveredVM(t, icAtCopy, cp)

	got, err := vm2.State.GetLastAccepted()
	require.NoError(err)
	require.Equal(outers[4].ID(), got,
		"boot must roll the finality pointer back onto the height the inner VM survived at")

	// The node re-proposes at 5: a NEW envelope over a NEW inner child of 4 —
	// the lost block is gone, not resurrected.
	mustBuildOnRecoveredTip(t, vm2, icAtCopy)
}
