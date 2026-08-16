// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// golive_executed_test.go — a node may go live only where it has actually executed.
//
// Gating normal operation on reaching the frontier exists so a validator never
// serves and votes at a stale height. The gate consults the acceptance oracle,
// which answers from the consensus ledger — what a quorum DECIDED. The VM answers
// what this node RAN. Those diverge, and when they do the ledger is the higher of
// the two, so the gate can be told the node is at the frontier while its VM sits
// far below it. Then the node goes live and nothing is trying to converge it.

package chains

import (
	"context"
	"testing"

	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/stretchr/testify/require"
)

// TestExecutedToRefusesAHeadTheVMHasNotRun is the failure this file exists for.
//
// The gate asks whether this node RAN the chain to the head it is about to go live
// at. The head comes from the consensus ledger — what a quorum decided — and the
// ledger runs ahead of execution, so the head can be well above anything this VM
// has executed. Answering yes there is a validator serving and voting at a stale
// height with nothing left trying to converge it.
func TestExecutedToRefusesAHeadTheVMHasNotRun(t *testing.T) {
	const N = 5
	chain, _ := buildBSChain(N, -1)
	bh := &blockHandler{logger: log.NewNoOpLogger(), vm: newBSVM(chain)}

	// The ledger's head is far above what this VM has executed — the live condition.
	if bh.executedTo(context.Background(), uint64(N)+200) {
		t.Fatalf("go-live was permitted at a head %d blocks above anything this VM ran", 200)
	}
}

// TestExecutedToAllowsAHeadTheVMHasRun: the same rule must not refuse a node that
// has genuinely executed, or every healthy restart hangs in bootstrap.
func TestExecutedToAllowsAHeadTheVMHasRun(t *testing.T) {
	const N = 5
	chain, _ := buildBSChain(N, -1)
	bh := &blockHandler{logger: log.NewNoOpLogger(), vm: newBSVM(chain)}

	_, ran, err := bh.vmLastAccepted(context.Background())
	if err != nil {
		t.Fatalf("harness VM unreadable: %v", err)
	}
	if !bh.executedTo(context.Background(), ran) {
		t.Fatalf("a VM that executed to %d was refused go-live at %d", ran, ran)
	}
}

// TestExecutedToRefusesAnUnreadableVM: an absent measurement is not a passing one.
func TestExecutedToRefusesAnUnreadableVM(t *testing.T) {
	bh := &blockHandler{logger: log.NewNoOpLogger()} // no VM at all
	if bh.executedTo(context.Background(), 1) {
		t.Fatal("go-live was permitted without being able to read the VM")
	}
}

// TestGoLiveAllowedWhenTheVMHasExecuted is the other half, and it is what keeps
// the gate from becoming a hang: a node that genuinely ran to its head must still
// go live, or every healthy restart sits in bootstrap forever.
func TestGoLiveAllowedWhenTheVMHasExecuted(t *testing.T) {
	const N = 5
	chain, byID := buildBSChain(N, -1)

	vm := newBSVM(chain) // executed to its own last-accepted
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 2)
	bh.selfNodeID = beacons[0]
	bh.msgCreator = bsMsgBuilder{}
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons[1:], byID: byID, tip: chain[0]}

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(context.Background())
	bh.bsActive.Store(false)

	require.Equal(t, chainbootstrap.FrontierCaughtUp, status,
		"a node that executed to its head must go live — refusing here hangs every healthy restart")
	require.Equal(t, chain[0].id, tip)
}

// wrappedVM models a wrapped chain: LastAccepted answers with the OUTER head,
// which leads the inner one, and InnerLastAccepted answers with what actually ran.
type wrappedVM struct {
	*bsTestVM
	outer *bsTestBlock
	inner *bsTestBlock
}

func (v *wrappedVM) LastAccepted(context.Context) (ids.ID, error) { return v.outer.id, nil }
func (v *wrappedVM) InnerLastAccepted(context.Context) (ids.ID, error) {
	return v.inner.id, nil
}

// TestExecutedToAsksTheVMThatExecutes is the production divergence exactly: the
// wrapper's head is at the frontier and the executing VM is far below it. Asking
// the wrapper returns the frontier and lets the node go live having run nothing;
// asking the VM that executes returns the truth.
func TestExecutedToAsksTheVMThatExecutes(t *testing.T) {
	const N = 6
	chain, _ := buildBSChain(N, -1)
	// Every block is resolvable, so the ONLY variable is which head is asked for.
	vm := newBSVMAt(chain, N-1)

	bh := &blockHandler{logger: log.NewNoOpLogger()}
	bh.vm = &wrappedVM{bsTestVM: vm, outer: chain[N-1], inner: chain[0]}

	if bh.executedTo(context.Background(), uint64(N-1)) {
		t.Fatalf("the wrapper's head (%d) was accepted as proof of execution while the "+
			"executing VM had only run to %d — the node goes live having run nothing",
			N-1, 0)
	}
}

// TestExecutedToTrustsTheInnerWhenItHasCaughtUp: once the executing VM has run to
// the head, the same node must be allowed live.
func TestExecutedToTrustsTheInnerWhenItHasCaughtUp(t *testing.T) {
	const N = 6
	chain, _ := buildBSChain(N, -1)
	vm := newBSVMAt(chain, N-1) // this node HAS executed to the head

	bh := &blockHandler{logger: log.NewNoOpLogger()}
	bh.vm = &wrappedVM{bsTestVM: vm, outer: chain[N-1], inner: chain[N-1]}

	if !bh.executedTo(context.Background(), uint64(N-1)) {
		t.Fatal("the executing VM was at the head and go-live was still refused")
	}
}

// The head every caller gets must be the one the node RAN, not the one its
// wrapper committed. The recovery loop lowers its position onto this number and
// reports whether a batch moved the node; read off the wrapper, it believes it
// stands past blocks it never executed, skips exactly the band it needs, and
// reports batch after batch landing while the chain it serves stays put.
func TestAppliedHeadIsTheInnerHead(t *testing.T) {
	const N = 10
	chain, _ := buildBSChain(N, -1)
	bh := &blockHandler{logger: log.NewNoOpLogger()}
	bh.vm = &wrappedVM{bsTestVM: newBSVMAt(chain, N), outer: chain[N], inner: chain[3]}

	_, applied, err := bh.vmLastAccepted(context.Background())
	require.NoError(t, err)
	require.Equal(t, chain[3].Height(), applied,
		"the applied head must be the inner one; the wrapper is at %d", chain[N].Height())
}
