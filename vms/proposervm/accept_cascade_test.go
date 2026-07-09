// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// accept_cascade_test.go — RED item #1 (choke #4): the NODE-LAYER proof that a
// proposervm-finalize propagates Accept to the inner execution VM, so the inner
// (luxfi/evm) lastAccepted ADVANCES.
//
// THE LIVE SYMPTOM this guards: consensus finalized an outer proposervm block but the
// inner EVM lastAccepted stayed frozen at the parent height (mainnet 1085755: block
// exists-but-unaccepted, "empty block" build spam). The consensus engine's FinalizedLedger
// advancing on VM.Accept is proven in engine/chain; here we prove the OTHER half — that
// VM.Accept on a proposervm wrapper actually cascades to the inner block's Accept:
//
//	postForkBlock.Accept → acceptOuterBlk (proposervm lastAccepted) → acceptInnerBlk →
//	Tree.Accept(innerBlk) → innerBlk.Accept  ⇒ inner VM lastAccepted advances
//
// The inner block here is a recording stand-in for the luxfi/evm block (the real EVM
// RLP-import→produce→tip byte-exactness is proven separately in evm/core
// rlp_seam_red_test.go); composed, the two cover the full engine→proposervm→EVM path.
package proposervm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/vm/chain/blocktest"

	"github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/state"
	"github.com/luxfi/node/vms/proposervm/tree"
)

// innerVMHead models the inner execution VM's accepted head (the luxfi/evm lastAccepted).
// It advances ONLY when an inner block's Accept runs — exactly the propagation under test.
type innerVMHead struct {
	lastAcceptedID     ids.ID
	lastAcceptedHeight uint64
	acceptCalls        int64
}

// recordingInner is a chain.Block whose Accept advances the shared inner head, standing in
// for the inner EVM block whose Accept moves lastAccepted.
type recordingInner struct {
	*blocktest.Block
	head *innerVMHead
}

func (r *recordingInner) Accept(ctx context.Context) error {
	if err := r.Block.Accept(ctx); err != nil {
		return err
	}
	r.head.lastAcceptedID = r.IDV
	r.head.lastAcceptedHeight = r.HeightV
	atomic.AddInt64(&r.head.acceptCalls, 1)
	return nil
}

func newCascadeVM() *VM {
	db := versiondb.New(memdb.New())
	vm := &VM{
		db:             db,
		State:          state.New(db),
		Tree:           tree.New(),
		verifiedBlocks: map[ids.ID]PostForkBlock{},
		logger:         log.Noop(),
		// quasarGate nil ⇒ verifyQuasarFinality is a no-op (the classical accept path).
	}
	// The last-accepted timestamp gauge is touched by Accept's metric reporting.
	metrics := metric.New("")
	vm.lastAcceptedTimestampGaugeVec = metrics.NewGaugeVec(
		"last_accepted_timestamp", "timestamp of the last block accepted", []string{"block_type"})
	return vm
}

func makeInner(head *innerVMHead, id, parent ids.ID, height uint64) *recordingInner {
	return &recordingInner{
		Block: &blocktest.Block{
			IDV:        id,
			ParentV:    parent,
			HeightV:    height,
			BytesV:     id[:],
			TimestampV: time.Unix(int64(height), 0),
		},
		head: head,
	}
}

// makeOuter wraps inner in a real proposervm envelope (an unsigned stateless block over the
// inner bytes), with distinct outer parent ⇒ distinct outer envelope id.
func makeOuter(t *testing.T, vm *VM, inner *recordingInner, parentOuter ids.ID, height uint64) *postForkBlock {
	t.Helper()
	sb, err := block.BuildUnsigned(parentOuter, time.Unix(int64(height), 0), 0, block.Epoch{}, inner.BytesV)
	if err != nil {
		t.Fatalf("BuildUnsigned: %v", err)
	}
	return &postForkBlock{
		SignedBlock:              sb,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: inner},
	}
}

// TestAcceptCascade_InnerHeadAdvances_AtScale drives 1000 heights through the REAL
// postForkBlock.Accept cascade and asserts BOTH the proposervm outer head AND the inner
// EVM head advance in lock-step at every height. A break here IS the 1085755 freeze
// (outer finalizes, inner frozen).
func TestAcceptCascade_InnerHeadAdvances_AtScale(t *testing.T) {
	vm := newCascadeVM()
	head := &innerVMHead{}

	parentOuter := ids.Empty
	parentInner := ids.Empty
	const N = 1000
	for h := uint64(1); h <= N; h++ {
		innerID := ids.GenerateTestID()
		inner := makeInner(head, innerID, parentInner, h)
		vm.Tree.Add(inner) // the inner was verified+recorded (as verifyAndRecordInnerBlk does)

		outer := makeOuter(t, vm, inner, parentOuter, h)
		if err := outer.Accept(context.Background()); err != nil {
			t.Fatalf("height %d: Accept: %v", h, err)
		}

		// THE PROOF: the inner EVM head advanced to this height's inner block.
		if head.lastAcceptedHeight != h || head.lastAcceptedID != innerID {
			t.Fatalf("height %d: inner EVM head did NOT advance (got height=%d id=%s) — the "+
				"proposervm-finalize→inner-Accept propagation is broken (the 1085755 freeze)",
				h, head.lastAcceptedHeight, head.lastAcceptedID)
		}
		// The proposervm outer head advanced too (lock-step).
		if vm.lastAcceptedHeight != h {
			t.Fatalf("height %d: proposervm lastAcceptedHeight=%d (outer/inner must move together)", h, vm.lastAcceptedHeight)
		}
		// Inner block Accept ran exactly once for this height.
		if got := atomic.LoadInt64(&head.acceptCalls); got != int64(h) {
			t.Fatalf("height %d: inner Accept call count=%d, want %d", h, got, h)
		}

		parentOuter = outer.ID()
		parentInner = innerID
	}
}

// TestAcceptCascade_SiblingStorm_WinnerAdvancesInnerOnce reproduces the anyone-can-propose
// storm at the node layer: FIVE distinct outer proposervm envelopes wrap the SAME inner
// execution block (the alias set). Accepting the ONE finalized wrapper (as consensus's
// storm-alias resolution does) must advance the inner EVM head to that shared inner block
// exactly once — not freeze it, and not double-accept.
func TestAcceptCascade_SiblingStorm_WinnerAdvancesInnerOnce(t *testing.T) {
	vm := newCascadeVM()
	head := &innerVMHead{}

	const H = uint64(1)
	innerID := ids.GenerateTestID()
	inner := makeInner(head, innerID, ids.Empty, H)
	vm.Tree.Add(inner)

	// Five competing OUTER wrappers of the SAME inner block (distinct outer parents ⇒ distinct
	// envelope ids), the sibling storm the finalize path must collapse.
	wrappers := make([]*postForkBlock, 5)
	seen := map[ids.ID]struct{}{}
	for i := range wrappers {
		wrappers[i] = makeOuter(t, vm, inner, ids.GenerateTestID(), H)
		id := wrappers[i].ID()
		if _, dup := seen[id]; dup {
			t.Fatal("precondition: the five wrappers must have DISTINCT outer envelope ids")
		}
		seen[id] = struct{}{}
	}

	// Finalize exactly ONE wrapper (the winner). Its Accept must propagate to the shared inner.
	if err := wrappers[2].Accept(context.Background()); err != nil {
		t.Fatalf("Accept winner: %v", err)
	}

	if head.lastAcceptedHeight != H || head.lastAcceptedID != innerID {
		t.Fatalf("inner EVM head must advance to the shared inner block %s on the winner's Accept, got (height=%d id=%s)",
			innerID, head.lastAcceptedHeight, head.lastAcceptedID)
	}
	if got := atomic.LoadInt64(&head.acceptCalls); got != 1 {
		t.Fatalf("the inner block Accept must run EXACTLY once (five aliases share one inner), got %d", got)
	}
	if vm.lastAcceptedHeight != H {
		t.Fatalf("proposervm lastAcceptedHeight must be %d, got %d", H, vm.lastAcceptedHeight)
	}
}
