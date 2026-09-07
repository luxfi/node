// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// quorum_guard_node_test.go — the fail-closed guard and the wiring that carries
// it. A test that injects a validators.State straight into the source
// constructors never exercises the path where m.validatorState is nil, which is
// the default until the P-Chain publishes its State. These tests pin:
//
//	(1) the guard predicate: a K>1 chain with NO height-indexed state is refused
//	    (would otherwise stall finality forever);
//	(2) the failure mechanism: getValidatorState(nil) → the no-op State, whose
//	    GetValidatorSet is EMPTY at every height → the stake source totals 0 and
//	    the set-root is Empty (exactly the inputs that make VerifyWeighted fail
//	    closed). This is what the guard exists to prevent silently;
//	(3) the fix: once a REAL height-indexed state is published, getValidatorState
//	    returns it and the same sources read a live set (non-zero stake / non-Empty
//	    root) — finality can proceed.
package chains

import (
	"context"
	"testing"

	"github.com/luxfi/ids"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/validatorstest"
)

// TestQuorumGuard_RefusesNoopState pins the guard predicate: a
// nil (unpublished) validator state is NOT live, so a K>1 chain must refuse to
// start; a published state IS live.
func TestQuorumGuard_RefusesNoopState(t *testing.T) {
	if quorumValidatorStateLive(nil) {
		t.Fatal("a nil validator state must NOT be considered live (would stall finality)")
	}
	live := validatorstest.NewTestState()
	if !quorumValidatorStateLive(live) {
		t.Fatal("a published height-indexed validator state must be considered live")
	}
}

// TestGetValidatorState_NoopIsEmptyAtEveryHeight proves the failure mechanism the
// guard prevents: with no state published, getValidatorState yields the no-op
// State, and the height-pinned sources read an EMPTY set at every height — zero
// total stake and an Empty set-root, the exact inputs that make the engine's
// VerifyWeighted fail closed (ErrQCStakeBelowSupermajority) so NO block finalizes.
func TestGetValidatorState_NoopIsEmptyAtEveryHeight(t *testing.T) {
	netID := ids.GenerateTestID()

	// Production default: validatorState unset → getValidatorState(nil) = no-op.
	noop := getValidatorState(nil)
	if noop == nil {
		t.Fatal("getValidatorState(nil) must return the no-op State, not nil")
	}

	stake := newValidatorStakeSource(noop, netID)
	root := newValidatorSetRootSource(noop, netID)

	for _, h := range []uint64{0, 1, 7, 1_000, 10_000_000} {
		if total := stake.TotalStake(h); total != 0 {
			t.Fatalf("no-op State must report zero total stake at height %d, got %d", h, total)
		}
		if r := root.ValidatorSetRoot(h); r != ids.Empty {
			t.Fatalf("no-op State must commit to Empty set-root at height %d, got %s", h, r)
		}
	}
}

// TestGetValidatorState_LiveStateReadsSet proves the fix: once a real
// height-indexed state is published (as the P-Chain does), getValidatorState
// returns it and the SAME sources read a live set — non-zero stake and a
// non-Empty set-root — so the ⅔-by-stake finality predicate can be satisfied.
func TestGetValidatorState_LiveStateReadsSet(t *testing.T) {
	netID := ids.GenerateTestID()
	n0, n1, n2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()

	const H = uint64(7)
	live := validatorstest.NewTestState()
	live.GetValidatorSetF = func(_ context.Context, height uint64, gotNet ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		if gotNet != netID || height != H {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		}
		return map[ids.NodeID]*validators.GetValidatorOutput{
			n0: {NodeID: n0, PublicKey: []byte("pk0"), Light: 30, Weight: 30},
			n1: {NodeID: n1, PublicKey: []byte("pk1"), Light: 30, Weight: 30},
			n2: {NodeID: n2, PublicKey: []byte("pk2"), Light: 40, Weight: 40},
		}, nil
	}

	// getValidatorState passes a non-nil state through unchanged.
	got := getValidatorState(live)
	stake := newValidatorStakeSource(got, netID)
	root := newValidatorSetRootSource(got, netID)

	if total := stake.TotalStake(H); total != 100 {
		t.Fatalf("live State must report total stake 100 at the epoch, got %d", total)
	}
	if w := stake.Weight(n2, H); w != 40 {
		t.Fatalf("live State must report n2 weight 40 at the epoch, got %d", w)
	}
	if r := root.ValidatorSetRoot(H); r == ids.Empty {
		t.Fatal("live State must commit to a NON-Empty set-root at the epoch")
	}
	// A different height (no set) is still Empty/zero — the read is height-pinned.
	if stake.TotalStake(H+1) != 0 || root.ValidatorSetRoot(H+1) != ids.Empty {
		t.Fatal("live State read must be height-pinned (empty at a height with no set)")
	}
}
