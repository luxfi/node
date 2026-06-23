// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// quorum_setroot_test.go — node-layer tests for the MEDIUM fix: the node must
// (a) supply a deterministic commitment to the active weighted validator set so
// the engine can pin every cert to its epoch, and (b) supply CURRENT-epoch stake
// weights honestly (the single-epoch node Manager has no per-height history; the
// cross-epoch soundness is enforced by the set-root binding, not by guessing
// stake at past heights).
package chains

import (
	"testing"

	"github.com/luxfi/ids"
	validators "github.com/luxfi/validators"
)

// TestValidatorSetRootSource_DeterministicAndSetSensitive proves the set-root is
// (1) deterministic for a fixed set (so honest nodes agree, a precondition for
// mutually-verifiable signatures), (2) order-independent of insertion (the
// commitment sorts by NodeID), and (3) CHANGES when the set or a weight changes
// (so a different epoch yields a different root → a cross-epoch cert is pinned
// out). Empty/absent sets commit to ids.Empty (the "unbound" answer).
func TestValidatorSetRootSource_DeterministicAndSetSensitive(t *testing.T) {
	netID := ids.GenerateTestID()
	n0, n1, n2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	pk0, pk1, pk2 := []byte("pk-0-48bytes-placeholder"), []byte("pk-1"), []byte("pk-2")

	// Empty manager → Empty root.
	empty := newValidatorSetRootSource(validators.NewManager(), netID)
	if got := empty.ValidatorSetRoot(0); got != ids.Empty {
		t.Fatalf("empty set must commit to ids.Empty, got %s", got)
	}

	// nil manager → Empty root (fail-soft).
	if got := (&validatorSetRootSource{vdrs: nil, networkID: netID}).ValidatorSetRoot(0); got != ids.Empty {
		t.Fatalf("nil manager must commit to ids.Empty, got %s", got)
	}

	// Build set {n0:10, n1:20, n2:30} in one order.
	mgrA := validators.NewManager()
	mustAddStaker(t, mgrA, netID, n0, pk0, 10)
	mustAddStaker(t, mgrA, netID, n1, pk1, 20)
	mustAddStaker(t, mgrA, netID, n2, pk2, 30)
	rootA := newValidatorSetRootSource(mgrA, netID).ValidatorSetRoot(0)
	if rootA == ids.Empty {
		t.Fatal("non-empty set must commit to a non-Empty root")
	}

	// Same set, DIFFERENT insertion order → SAME root (sorted commitment).
	mgrB := validators.NewManager()
	mustAddStaker(t, mgrB, netID, n2, pk2, 30)
	mustAddStaker(t, mgrB, netID, n0, pk0, 10)
	mustAddStaker(t, mgrB, netID, n1, pk1, 20)
	rootB := newValidatorSetRootSource(mgrB, netID).ValidatorSetRoot(0)
	if rootA != rootB {
		t.Fatalf("set-root must be insertion-order independent: %s != %s", rootA, rootB)
	}

	// The height argument does not change the current-epoch commitment.
	if h := newValidatorSetRootSource(mgrB, netID).ValidatorSetRoot(999_999); h != rootA {
		t.Fatalf("set-root must be stable across the (advisory) height arg: %s != %s", h, rootA)
	}

	// Change ONE weight → DIFFERENT root (a new epoch is a different set).
	mgrC := validators.NewManager()
	mustAddStaker(t, mgrC, netID, n0, pk0, 10)
	mustAddStaker(t, mgrC, netID, n1, pk1, 21) // 20 -> 21
	mustAddStaker(t, mgrC, netID, n2, pk2, 30)
	rootC := newValidatorSetRootSource(mgrC, netID).ValidatorSetRoot(0)
	if rootC == rootA {
		t.Fatal("a weight change MUST change the set-root (else a cert is not pinned to its epoch's stake)")
	}

	// Add a validator → DIFFERENT root.
	mustAddStaker(t, mgrC, netID, ids.GenerateTestNodeID(), []byte("pk-3"), 5)
	if rootD := newValidatorSetRootSource(mgrC, netID).ValidatorSetRoot(0); rootD == rootC {
		t.Fatal("adding a validator MUST change the set-root")
	}

	// A different network's set is independent (scoped by networkID).
	if other := newValidatorSetRootSource(mgrA, ids.GenerateTestID()).ValidatorSetRoot(0); other != ids.Empty {
		t.Fatalf("a network with no validators must commit to ids.Empty, got %s", other)
	}
}

// TestValidatorStakeSource_CurrentEpochWeights proves the stake source reports
// the current set's weights and total honestly (the live in-epoch path), and is
// fail-soft (nil manager → 0).
func TestValidatorStakeSource_CurrentEpochWeights(t *testing.T) {
	netID := ids.GenerateTestID()
	n0, n1 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	mgr := validators.NewManager()
	mustAddStaker(t, mgr, netID, n0, []byte("pk0"), 70)
	mustAddStaker(t, mgr, netID, n1, []byte("pk1"), 30)

	src := newValidatorStakeSource(mgr, netID)
	if w := src.Weight(n0, 0); w != 70 {
		t.Fatalf("Weight(n0) = %d, want 70", w)
	}
	if w := src.Weight(n1, 12345); w != 30 { // height arg is advisory for the single-epoch manager
		t.Fatalf("Weight(n1) = %d, want 30", w)
	}
	if total := src.TotalStake(0); total != 100 {
		t.Fatalf("TotalStake = %d, want 100", total)
	}
	// Unknown node → 0 (cannot inflate the numerator).
	if w := src.Weight(ids.GenerateTestNodeID(), 0); w != 0 {
		t.Fatalf("Weight(unknown) = %d, want 0", w)
	}
	// nil manager → fail-soft zeros.
	nilSrc := &validatorStakeSource{vdrs: nil, networkID: netID}
	if nilSrc.Weight(n0, 0) != 0 || nilSrc.TotalStake(0) != 0 {
		t.Fatal("nil manager must yield zero weight and total")
	}
}

func mustAddStaker(t *testing.T, m validators.Manager, netID ids.ID, nodeID ids.NodeID, pk []byte, light uint64) {
	t.Helper()
	if err := m.AddStaker(netID, nodeID, pk, ids.GenerateTestID(), light); err != nil {
		t.Fatalf("AddStaker: %v", err)
	}
}
