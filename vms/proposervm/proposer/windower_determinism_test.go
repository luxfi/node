// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// windower_determinism_test.go — the SAFETY/LIVENESS boundary of the proposer
// schedule, the half that makes the down/wedged/forked-proposer fix BFT-safe.
//
// The consensus engine's liveness fix (re-solicit a substitute's block until it
// finalizes) is only safe because WHICH node is the designated proposer for a slot
// is DETERMINISTIC across every honest node and rotates per slot:
//
//   - DETERMINISM (safety): every honest node computes the IDENTICAL expected
//     proposer for a given (chainID, height, pChainHeight, slot). So an honest node
//     accepts a SIGNED block for slot S iff it was signed by ExpectedProposer(S) —
//     a node proposing OUT OF TURN (before its slot) is rejected by EVERY honest
//     node (Verify's errUnexpectedProposer). An attacker cannot make nodes disagree
//     on the eligible proposer and thereby flood competing accepted blocks / fork.
//
//   - ROTATION (liveness): consecutive slots designate (in general) DIFFERENT
//     proposers, so a down/wedged/forked designated proposer for slot S is routed
//     around: at slot S+1 (5s later) a different validator is designated and builds
//     a signed block the rest accept. This is the upstream proposer-window mechanism,
//     byte-for-byte (windower.go is identical to ava's), and the reason a faulty
//     leader cannot halt the chain.
//
// These properties are asserted across many heights, slots, and seeds — not one
// hand-picked case — so the BFT boundary holds over the input space.
package proposer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

// expectedProposerInTurn models the verify-side decision (verifyPostDurangoBlockDelay
// / shouldBuildSignedBlockPostDurango): a SIGNED block for `slot` is "in turn" iff
// its signer equals the slot's deterministic designated proposer. Out-of-turn ⇒
// the proposervm Verify returns errUnexpectedProposer on every honest node.
func expectedProposerInTurn(t testing.TB, w Windower, height, pChainHeight, slot uint64, signer ids.NodeID) bool {
	t.Helper()
	exp, err := w.ExpectedProposer(context.Background(), height, pChainHeight, slot)
	require.NoError(t, err)
	return exp == signer
}

// TestExpectedProposer_DeterministicAcrossInstances proves the SAFETY half: two
// INDEPENDENT windower instances (modelling two distinct honest nodes, each with
// its own validator-state object over the SAME set) compute byte-identical expected
// proposers for every (height, slot). If they could disagree, two honest nodes would
// accept different signed blocks for one slot → competing accepted blocks → fork.
func TestExpectedProposer_DeterministicAcrossInstances(t *testing.T) {
	require := require.New(t)
	const numValidators = 11

	validatorIDs, vdrStateA := makeValidators(t, numValidators)
	vdrStateB := makeValidatorState(t, validatorIDs) // independent state, same set

	// Two nodes that agree on chainID + netID must agree on the schedule.
	nodeA := New(vdrStateA, netID, fixedChainID)
	nodeB := New(vdrStateB, netID, fixedChainID)

	for height := uint64(0); height < 50; height++ {
		for slot := uint64(0); slot < 3*MaxLookAheadSlots; slot += 37 { // sample the slot space cheaply
			pA, err := nodeA.ExpectedProposer(context.Background(), height, 0, slot)
			require.NoError(err)
			pB, err := nodeB.ExpectedProposer(context.Background(), height, 0, slot)
			require.NoError(err)
			require.Equal(pA, pB,
				"two honest nodes disagreed on the designated proposer for height=%d slot=%d (%s != %s) — "+
					"non-deterministic eligibility breaks the BFT boundary (competing accepted blocks / fork)",
				height, slot, pA, pB)

			// And the designated proposer is always a real member of the set.
			require.Contains(validatorIDs, pA,
				"expected proposer %s for height=%d slot=%d is not in the validator set", pA, height, slot)
		}
	}
}

// TestExpectedProposer_DeterministicAcrossSeeds proves determinism is a property of
// the (chainID, height, slot) inputs, not of any per-instance random state: the same
// inputs always yield the same proposer, and DIFFERENT chainIDs generally yield a
// DIFFERENT schedule (so the seed actually mixes chainID — no cross-chain schedule
// collision an attacker could exploit). Run over many seeds.
func TestExpectedProposer_DeterministicAcrossSeeds(t *testing.T) {
	require := require.New(t)
	validatorIDs, vdrState := makeValidators(t, 21)

	differs := 0
	const seeds = 64
	for s := 0; s < seeds; s++ {
		cid := ids.ID{byte(s), byte(s >> 8), 0x5a}
		w1 := New(vdrState, netID, cid)
		w2 := New(makeValidatorState(t, validatorIDs), netID, cid)
		// Repeat-call determinism + cross-instance determinism for this seed.
		for slot := uint64(0); slot < 16; slot++ {
			p1a, err := w1.ExpectedProposer(context.Background(), 7, 0, slot)
			require.NoError(err)
			p1b, err := w1.ExpectedProposer(context.Background(), 7, 0, slot)
			require.NoError(err)
			p2, err := w2.ExpectedProposer(context.Background(), 7, 0, slot)
			require.NoError(err)
			require.Equal(p1a, p1b, "repeated call non-deterministic at seed=%d slot=%d", s, slot)
			require.Equal(p1a, p2, "cross-instance non-deterministic at seed=%d slot=%d", s, slot)
			require.Contains(validatorIDs, p1a)
		}
		// Compare schedules of consecutive chainIDs at one slot to confirm chainID mixes.
		if s > 0 {
			prev := New(vdrState, netID, ids.ID{byte(s - 1), byte((s - 1) >> 8), 0x5a})
			a, _ := prev.ExpectedProposer(context.Background(), 7, 0, 0)
			b, _ := w1.ExpectedProposer(context.Background(), 7, 0, 0)
			if a != b {
				differs++
			}
		}
	}
	// The schedule must depend on chainID for the overwhelming majority of seed pairs
	// (a constant schedule would mean chainID is ignored — a real schedule-collision bug).
	require.Greater(differs, seeds/2,
		"chainID barely affects the schedule (%d/%d seed pairs differ) — seed derivation may ignore chainID",
		differs, seeds)
}

// TestExpectedProposer_OutOfTurnSignerIsRejected proves the SAFETY boundary the
// fallback must NOT breach: for every slot, EXACTLY ONE validator is "in turn" (the
// designated proposer) and EVERY OTHER validator is "out of turn" — so an honest
// node accepts a signed block for that slot ONLY from the designated proposer and
// REJECTS an out-of-turn (early / wrong) proposer's signed block. This is the
// windower half of verifyPostDurangoBlockDelay's errUnexpectedProposer.
func TestExpectedProposer_OutOfTurnSignerIsRejected(t *testing.T) {
	require := require.New(t)
	validatorIDs, vdrState := makeValidators(t, 11)
	w := New(vdrState, netID, fixedChainID)

	for slot := uint64(0); slot < 64; slot++ {
		inTurnCount := 0
		var designated ids.NodeID
		for _, id := range validatorIDs {
			if expectedProposerInTurn(t, w, 9, 0, slot, id) {
				inTurnCount++
				designated = id
			}
		}
		require.Equal(1, inTurnCount,
			"slot %d must have EXACTLY ONE in-turn proposer (got %d) — otherwise two signed blocks are both "+
				"'in turn' and an out-of-turn proposer is accepted (the early-acceptance fork hole)", slot, inTurnCount)

		// Every non-designated validator is out of turn for this slot → its signed
		// block is rejected by Verify on every honest node.
		for _, id := range validatorIDs {
			if id == designated {
				continue
			}
			require.False(expectedProposerInTurn(t, w, 9, 0, slot, id),
				"validator %s is NOT the designated proposer for slot %d yet was treated as in-turn — "+
					"an out-of-turn proposal must be rejected", id, slot)
		}
	}
}

// TestExpectedProposer_SlotRotation_RoutesAroundDownProposer proves the LIVENESS
// half: across a window of consecutive slots the designated proposer rotates over
// MANY distinct validators, so a single down/wedged/forked designated proposer for
// one slot is routed around — a later slot designates a healthy validator who builds
// a signed block the honest majority accepts and finalizes. (This is why a faulty
// leader cannot halt the chain.)
func TestExpectedProposer_SlotRotation_RoutesAroundDownProposer(t *testing.T) {
	require := require.New(t)
	const numValidators = 11
	_, vdrState := makeValidators(t, numValidators)
	w := New(vdrState, netID, fixedChainID)

	// Pick slot 0's designated proposer as the "down/wedged/forked" leader.
	down, err := w.ExpectedProposer(context.Background(), 13, 0, 0)
	require.NoError(err)

	// Within a small number of slots after it, a DIFFERENT (healthy) validator must be
	// designated — i.e. the down leader is routed around quickly. Assert a healthy
	// substitute appears within the next few slots, and that over a window the schedule
	// covers most of the set (real rotation, not a stuck single proposer).
	distinct := map[ids.NodeID]struct{}{}
	substituteWithin := -1
	for slot := uint64(0); slot < uint64(numValidators)*3; slot++ {
		p, err := w.ExpectedProposer(context.Background(), 13, 0, slot)
		require.NoError(err)
		distinct[p] = struct{}{}
		if slot >= 1 && slot <= 5 && p != down && substituteWithin < 0 {
			substituteWithin = int(slot)
		}
	}
	require.GreaterOrEqual(substituteWithin, 1,
		"no healthy substitute proposer was designated within 5 slots of the down leader %s — a faulty "+
			"leader would stall the chain instead of being routed around", down)
	require.Greater(len(distinct), numValidators/2,
		"the schedule designated only %d of %d validators over the window — rotation too weak to route around "+
			"faults", len(distinct), numValidators)
}
