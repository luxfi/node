// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// quorum_verifier_height_test.go — node-layer regression for RESIDUAL-B and the
// CRITICAL-1 wiring: the BLS vote verifier resolves the voter's public key from
// the HEIGHT-INDEXED validators.State AT THE BLOCK'S P-CHAIN EPOCH HEIGHT — the
// SAME height-pinned source as the set-root and the ⅔-by-stake tally — NOT from
// the current validator map.
//
// The round-1 fix left the verifier reading the CURRENT map (m.Validators):
// a validator present in set@H (it legitimately signed block H) but already gone
// from the current map during async staking skew had its vote DROPPED, and if it
// held >⅓ of the stake-at-H the block never finalized. These tests prove the
// verifier now reads set@H, so such a vote verifies at H (and a vote keyed to the
// wrong height does not).
package chains

import (
	"context"
	"testing"

	"github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/validatorstest"
)

// blsKey is a test validator's BLS key material.
type blsKey struct {
	nodeID ids.NodeID
	sk     *bls.SecretKey
	pkComp []byte
}

func newBLSKey(t *testing.T) blsKey {
	t.Helper()
	sk, err := bls.NewSecretKey()
	if err != nil {
		t.Fatalf("NewSecretKey: %v", err)
	}
	return blsKey{
		nodeID: ids.GenerateTestNodeID(),
		sk:     sk,
		pkComp: bls.PublicKeyToCompressedBytes(sk.PublicKey()),
	}
}

// stateWithBLSByHeight builds a height-indexed validators.State that reports the
// given BLS validators at each height (empty for unknown heights / wrong net).
func stateWithBLSByHeight(netID ids.ID, byHeight map[uint64][]blsKey) *validatorstest.TestState {
	s := validatorstest.NewTestState()
	s.GetValidatorSetF = func(_ context.Context, height uint64, gotNet ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		if gotNet != netID {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		}
		out := make(map[ids.NodeID]*validators.GetValidatorOutput)
		for _, k := range byHeight[height] {
			out[k.nodeID] = &validators.GetValidatorOutput{
				NodeID:    k.nodeID,
				PublicKey: k.pkComp,
				Light:     1,
				Weight:    1,
			}
		}
		return out, nil
	}
	return s
}

// TestBLSVoteVerifier_ResolvesPubkeyAtEpochHeight is the RESIDUAL-B core: a
// validator that is in set@H but has LEFT the set at a later height still has its
// vote verified at H (the verifier reads set@H, not the current map).
func TestBLSVoteVerifier_ResolvesPubkeyAtEpochHeight(t *testing.T) {
	netID := ids.GenerateTestID()
	keep := newBLSKey(t) // stays in the set across epochs
	leaver := newBLSKey(t) // in set@10, GONE from set@11 (departed the current set)

	const H = uint64(10)
	state := stateWithBLSByHeight(netID, map[uint64][]blsKey{
		10: {keep, leaver},
		11: {keep}, // leaver has departed by height 11 (the "current" epoch)
	})

	v := newBLSVoteVerifier(state, netID)

	// The leaver signs a message; its vote MUST verify at epoch H=10 (it was a
	// member then), proving pubkey resolution is at the epoch, not the current set.
	msg := []byte("LUX/chain/vote/v1\x00 — epoch-pinned verify")
	sig, err := leaver.sk.Sign(msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigBytes := bls.SignatureToBytes(sig)

	if !v.VerifyVote(leaver.nodeID, msg, sigBytes, H) {
		t.Fatal("RESIDUAL-B: a validator in set@H must verify at H even after leaving the current set " +
			"(verifier read the current map instead of set@H)")
	}

	// At height 11 the leaver is NOT a member → its vote MUST NOT verify there.
	// This proves the resolution is genuinely height-pinned (not height-agnostic).
	if v.VerifyVote(leaver.nodeID, msg, sigBytes, 11) {
		t.Fatal("a validator absent from set@11 must NOT verify at 11 (height pinning is not in effect)")
	}

	// The keeper verifies at both heights (it is in both sets) — sanity that the
	// epoch read is not rejecting valid current members.
	keepSig, _ := keep.sk.Sign(msg)
	keepBytes := bls.SignatureToBytes(keepSig)
	if !v.VerifyVote(keep.nodeID, msg, keepBytes, 10) || !v.VerifyVote(keep.nodeID, msg, keepBytes, 11) {
		t.Fatal("a validator present at both epochs must verify at both")
	}
}

// TestBLSVoteVerifier_FailClosed proves the verifier never panics and returns
// false for every fail-soft case: nil state, unknown voter at the epoch, wrong
// network, an unknown height (empty set), a wrong-length signature, and the
// HIGH-1 malformed-infinity signature (0x40||zeros) that used to PANIC the purego
// BLS path — here it must be a clean false, not a crash.
func TestBLSVoteVerifier_FailClosed(t *testing.T) {
	netID := ids.GenerateTestID()
	k := newBLSKey(t)
	state := stateWithBLSByHeight(netID, map[uint64][]blsKey{10: {k}})
	msg := []byte("msg")
	sig, _ := k.sk.Sign(msg)
	good := bls.SignatureToBytes(sig)

	// nil state → false for any voter.
	if newBLSVoteVerifier(nil, netID).VerifyVote(k.nodeID, msg, good, 10) {
		t.Fatal("nil state must yield false")
	}
	v := newBLSVoteVerifier(state, netID)
	// unknown voter at a known epoch.
	if v.VerifyVote(ids.GenerateTestNodeID(), msg, good, 10) {
		t.Fatal("unknown voter at the epoch must yield false")
	}
	// known voter at an UNKNOWN epoch (empty set) → false.
	if v.VerifyVote(k.nodeID, msg, good, 999) {
		t.Fatal("known voter at an unknown epoch (empty set) must yield false")
	}
	// wrong network → empty set → false.
	if newBLSVoteVerifier(state, ids.GenerateTestID()).VerifyVote(k.nodeID, msg, good, 10) {
		t.Fatal("wrong network must yield false")
	}
	// wrong-length signature → false (no panic).
	if v.VerifyVote(k.nodeID, msg, good[:len(good)-1], 10) {
		t.Fatal("wrong-length signature must yield false")
	}
	// HIGH-1 malformed infinity sig (0x40||zeros) → clean false, NOT a panic.
	mal := make([]byte, bls.SignatureLen)
	mal[0] = 0x40
	if v.VerifyVote(k.nodeID, msg, mal, 10) {
		t.Fatal("malformed-infinity signature must yield false")
	}
	// A WRONG signature (valid form, wrong key) → false.
	other := newBLSKey(t)
	otherSig, _ := other.sk.Sign(msg)
	if v.VerifyVote(k.nodeID, msg, bls.SignatureToBytes(otherSig), 10) {
		t.Fatal("a signature by a different key must yield false")
	}
}

// ensure the node verifier still satisfies the (now height-aware) engine interface.
var _ chain.VoteVerifier = (*blsVoteVerifier)(nil)
