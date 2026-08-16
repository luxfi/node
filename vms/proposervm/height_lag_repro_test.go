// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// height_lag_repro_test.go — the DETERMINISTIC REPRODUCTION of the proposervm
// finality-index lag, written against the PRE-EXISTING proposervm API only (no
// symbol introduced by the fix), so it fails on the unfixed code and passes on the
// fixed code without being tautological.
//
// The failure it reproduces, in the shape the node reports it:
//
//	VM initialization failed error="failed to repair accepted chain by height:
//	proposervm finality index (height N) is BEHIND the inner VM tip (height M)"
//	non-critical chain failed to initialize … chainAlias=C
//
// THE MECHANISM. Every post-fork accept commits the envelope, its height index
// entry and the last-accepted pointer in ONE versiondb batch BEFORE the inner
// block is accepted, so the index can only run AHEAD. But the proposervm has one
// accept path that moves the inner VM and writes NOTHING: preForkBlock.Accept,
// whose acceptOuterBlk() is a no-op. Post-fork it was reachable because getBlock()
// silently falls back to constructing a preForkBlock whenever the id is not an
// OUTER envelope id — and the id this fork's consensus ledger records as canonical
// IS the inner block's id (postForkCommonComponents.CanonicalID). One such accept
// and the index is stranded: invisible while running, fatal at the next boot.
//
// This file also holds the shared harness the recovery tests use.
package proposervm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/runtime"
	vmchain "github.com/luxfi/vm/chain"
	"github.com/luxfi/vm/chain/blocktest"

	"github.com/luxfi/node/cache/lru"
	statelessblock "github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/state"
	"github.com/luxfi/node/vms/proposervm/tree"
)

// innerChain is a stand-in for the inner execution VM (the C-Chain EVM): a
// contiguous accepted chain 1..tip answering LastAccepted / GetBlock /
// GetBlockIDAtHeight / ParseBlock exactly as the real one does.
type innerChain struct {
	byHeight map[uint64]*blocktest.Block
	byID     map[ids.ID]*blocktest.Block
	byBytes  map[string]*blocktest.Block
	tip      uint64
}

func newInnerChain(t *testing.T, tip uint64) *innerChain {
	t.Helper()
	ic := &innerChain{
		byHeight: map[uint64]*blocktest.Block{},
		byID:     map[ids.ID]*blocktest.Block{},
		byBytes:  map[string]*blocktest.Block{},
	}
	parent := ids.Empty
	for h := uint64(1); h <= tip; h++ {
		id := ids.GenerateTestID()
		blk := &blocktest.Block{
			IDV:        id,
			ParentV:    parent,
			HeightV:    h,
			BytesV:     append([]byte("inner"), id[:]...),
			TimestampV: time.Unix(int64(h), 0),
		}
		ic.byHeight[h] = blk
		ic.byID[id] = blk
		ic.byBytes[string(blk.BytesV)] = blk
		parent = id
	}
	ic.tip = tip
	return ic
}

// accept models the inner VM accepting a block: its head advances. This is the
// ONLY way the inner tip moves, exactly as in the real EVM.
func (ic *innerChain) accept(h uint64) { ic.tip = h }

func (ic *innerChain) vm() *blocktest.VM {
	return &blocktest.VM{
		LastAcceptedF: func(context.Context) (ids.ID, error) {
			return ic.byHeight[ic.tip].IDV, nil
		},
		GetBlockF: func(_ context.Context, id ids.ID) (vmchain.Block, error) {
			blk, ok := ic.byID[id]
			if !ok {
				return nil, errors.New("inner: block not found")
			}
			return blk, nil
		},
		GetBlockIDAtHeightF: func(_ context.Context, h uint64) (ids.ID, error) {
			blk, ok := ic.byHeight[h]
			if !ok {
				return ids.Empty, errors.New("inner: height not indexed")
			}
			return blk.IDV, nil
		},
		ParseBlockF: func(_ context.Context, b []byte) (vmchain.Block, error) {
			blk, ok := ic.byBytes[string(b)]
			if !ok {
				return nil, errors.New("inner: unparseable bytes")
			}
			return blk, nil
		},
	}
}

// testVM builds a proposervm over memdb wrapping [ic] — the real VM struct with
// the real State, so every write below goes through the production code paths.
func testVM(t *testing.T, ic *innerChain) *VM {
	t.Helper()
	return testVMOnBase(t, ic, memdb.New())
}

// testVMOnBase is testVM over a caller-supplied base database — the seam the
// crash-boot tests (vm_crashboot_test.go) use to boot a SECOND, cold VM over
// bytes copied out of a running one.
func testVMOnBase(t *testing.T, ic *innerChain, base database.Database) *VM {
	t.Helper()
	db := versiondb.New(base)
	metrics := metric.New("")
	vm := &VM{
		ChainVM:        ic.vm(),
		db:             db,
		State:          state.New(db),
		Tree:           tree.New(),
		verifiedBlocks: map[ids.ID]PostForkBlock{},
		innerBlkCache:  lru.NewSizedCache(innerBlkCacheSize, cachedBlockSize),
		logger:         log.Noop(),
		rt:             &runtime.Runtime{ChainID: ids.GenerateTestID()},
	}
	vm.lastAcceptedTimestampGaugeVec = metrics.NewGaugeVec(
		"last_accepted_timestamp", "timestamp of the last block accepted", []string{"block_type"})
	return vm
}

// outerFor wraps the inner block at [h] in a real proposervm envelope chained off
// [parentOuter] — the same shape a proposer builds.
func outerFor(t *testing.T, ic *innerChain, h uint64, parentOuter ids.ID) statelessblock.SignedBlock {
	t.Helper()
	sb, err := statelessblock.BuildUnsigned(
		parentOuter, time.Unix(int64(h), 0), 0, statelessblock.Epoch{}, ic.byHeight[h].BytesV)
	if err != nil {
		t.Fatalf("BuildUnsigned(h=%d): %v", h, err)
	}
	return sb
}

// acceptThroughProposervm drives heights 1..through through the REAL post-fork
// accept path, so index and inner head advance in lock-step. This is a healthy
// node, and it is what records the fork height.
func acceptThroughProposervm(t *testing.T, vm *VM, ic *innerChain, through uint64) map[uint64]statelessblock.SignedBlock {
	t.Helper()
	return acceptRangeThroughProposervm(t, vm, ic, 1, through, ids.Empty)
}

// acceptRangeThroughProposervm is acceptThroughProposervm for heights
// from..through, chaining the first envelope off parentOuter — so a test can
// keep a source node accepting AFTER a crash copy was taken.
func acceptRangeThroughProposervm(t *testing.T, vm *VM, ic *innerChain, from, through uint64, parentOuter ids.ID) map[uint64]statelessblock.SignedBlock {
	t.Helper()
	outers := map[uint64]statelessblock.SignedBlock{}
	for h := from; h <= through; h++ {
		sb := outerFor(t, ic, h, parentOuter)
		blk := &postForkBlock{
			SignedBlock:              sb,
			postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: ic.byHeight[h]},
		}
		vm.Tree.Add(ic.byHeight[h])
		if err := blk.Accept(context.Background()); err != nil {
			t.Fatalf("post-fork Accept(h=%d): %v", h, err)
		}
		ic.accept(h)
		outers[h] = sb
		parentOuter = sb.ID()
	}
	return outers
}

// indexHeight reads the height the PERSISTED finality index sits at — the exact
// quantity repairAcceptedChainByHeight compares against the inner tip at boot.
func indexHeight(t *testing.T, vm *VM) uint64 {
	t.Helper()
	id, err := vm.State.GetLastAccepted()
	if err != nil {
		t.Fatalf("GetLastAccepted: %v", err)
	}
	blk, err := vm.getPostForkBlock(context.Background(), id)
	if err != nil {
		t.Fatalf("getPostForkBlock(%s): %v", id, err)
	}
	return blk.Height()
}

// TestFinalityIndexLag_IndexMustNeverFallBehindInnerTip is THE reproduction.
//
// It asks the proposervm for the block at the CANONICAL id of height 6 — the id
// the consensus ledger records for a proposervm-wrapped block — accepts whatever
// comes back, and then asserts the two things a correct proposervm guarantees:
//
//  1. the persisted finality index is never below the inner VM tip, and
//  2. a boot-time reconciliation of that state never kills the chain.
//
// UNFIXED, getBlock hands back a preForkBlock, its Accept is a silent no-op on the
// outer index, the inner VM advances to 6, and this test fails on (1) — printing
// the divergence that makes the next boot fatal. FIXED, the pre-fork
// construction/accept is refused, nothing diverges, and both assertions hold.
//
// It uses NO symbol introduced by the fix, so it is a genuine before/after probe.
func TestFinalityIndexLag_IndexMustNeverFallBehindInnerTip(t *testing.T) {
	ctx := context.Background()
	ic := newInnerChain(t, 8)
	vm := testVM(t, ic)

	acceptThroughProposervm(t, vm, ic, 5)
	if got, tip := indexHeight(t, vm), ic.tip; got != 5 || tip != 5 {
		t.Fatalf("precondition: want index=5 inner=5, got index=%d inner=%d", got, tip)
	}
	forkHeight, err := vm.State.GetForkHeight()
	if err != nil {
		t.Fatalf("GetForkHeight: %v", err)
	}

	// The engine asks for height 6 by its CANONICAL (inner) id.
	if blk, err := vm.getBlock(ctx, ic.byHeight[6].IDV); err == nil {
		if _, isPreFork := blk.(*preForkBlock); isPreFork {
			t.Logf("getBlock(canonical id @6) returned a preForkBlock — fork height is %d", forkHeight)
		}
		if err := blk.Accept(ctx); err == nil {
			ic.accept(6) // the inner VM applied it
		}
	}

	// ASSERTION 1 — the invariant.
	if got := indexHeight(t, vm); got < ic.tip {
		// Errorf, not Fatalf: assertion 2 below must also run, so one failing run
		// shows the WHOLE symptom chain (lag now -> dead chain at boot).
		t.Errorf("FINALITY INDEX LAG REPRODUCED: index is at height %d but the inner VM tip is %d "+
			"(fork height %d). An accept advanced the inner VM without writing the proposervm index; "+
			"this node is now unbootable", got, ic.tip, forkHeight)
	}

	// ASSERTION 2 — boot must not kill the chain.
	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		t.Fatalf("BOOT KILLED THE CHAIN: %v", err)
	}
}

// TestPreForkAcceptStillWorksBeforeFork is the positive control for the guard: on
// a chain that has NOT forked there is no fork height, every block is legitimately
// pre-fork, and both construction and Accept must still work. A guard that broke
// this would brick every chain before its transition block.
func TestPreForkAcceptStillWorksBeforeFork(t *testing.T) {
	ctx := context.Background()
	ic := newInnerChain(t, 3)
	vm := testVM(t, ic)

	if _, err := vm.State.GetForkHeight(); err == nil {
		t.Fatal("precondition: a fresh chain must have no fork height")
	}
	pre := &preForkBlock{Block: ic.byHeight[1], vm: vm}
	if err := pre.Accept(ctx); err != nil {
		t.Fatalf("pre-fork accept before the fork must succeed, got %v", err)
	}
	if _, err := vm.getPreForkBlock(ctx, ic.byHeight[1].IDV); err != nil {
		t.Fatalf("pre-fork getBlock before the fork must succeed, got %v", err)
	}
}
