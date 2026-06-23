// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// quorum_setroot_test.go — node-layer tests for MEDIUM-1: the set-root and the
// ⅔-by-stake tally MUST be read from the HEIGHT-INDEXED validators.State at the
// cert-position height, so every node computes the IDENTICAL root, weights, and
// total for a given value-chain height — INDEPENDENT of async current-map skew
// during a validator-set change.
//
// The cross-node test (TestValidatorSetRoot_CrossNodeAgreesDespiteSkew) is the
// one the red flagged as MISSING: it models two nodes whose CURRENT validator
// views diverge (a set-change in flight) but who agree on the historical set at a
// pinned height H — and proves they nonetheless compute the SAME set-root at H.
// Reading the current map (the prior bug) would have made their roots differ →
// mismatched canonical signed messages → dropped votes → finality stall.
package chains

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"testing"

	"github.com/luxfi/ids"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/validatorstest"
)

// expectedSetRoot is an INDEPENDENT reimplementation of the canonical set-root
// spec, used only by the golden test to cross-check hashValidatorSet. It is
// deliberately NOT a call to hashValidatorSet (that would be a tautology): if the
// production encoding ever diverges from this spec the golden test fails.
func expectedSetRoot(t *testing.T, vdrs []vdr) ids.ID {
	t.Helper()
	sort.Slice(vdrs, func(i, j int) bool {
		return bytes.Compare(vdrs[i].nodeID[:], vdrs[j].nodeID[:]) < 0
	})
	h := sha256.New()
	var u64 [8]byte
	for _, v := range vdrs {
		h.Write(v.nodeID[:])
		binary.BigEndian.PutUint64(u64[:], v.light)
		h.Write(u64[:])
		binary.BigEndian.PutUint64(u64[:], uint64(len(v.pk)))
		h.Write(u64[:])
		h.Write(v.pk)
	}
	var root ids.ID
	copy(root[:], h.Sum(nil))
	return root
}

// vdr is a tiny height→set fixture: which validators (and weights/keys) the
// height-indexed state reports at each value-chain height.
type vdr struct {
	nodeID ids.NodeID
	pk     []byte
	light  uint64
}

// stateWithHistory builds a validators.State whose GetValidatorSet returns the
// set registered for the requested height (and an empty set for unknown heights),
// scoped to netID. This is the height-indexed source MEDIUM-1 reads from.
func stateWithHistory(netID ids.ID, byHeight map[uint64][]vdr) *validatorstest.TestState {
	s := validatorstest.NewTestState()
	s.GetValidatorSetF = func(_ context.Context, height uint64, gotNet ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		if gotNet != netID {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		}
		out := make(map[ids.NodeID]*validators.GetValidatorOutput)
		for _, v := range byHeight[height] {
			out[v.nodeID] = &validators.GetValidatorOutput{
				NodeID:    v.nodeID,
				PublicKey: v.pk,
				Light:     v.light,
				Weight:    v.light,
			}
		}
		return out, nil
	}
	return s
}

// TestValidatorSetRoot_CrossNodeAgreesDespiteSkew is the MEDIUM-1 regression
// guard. Two nodes are mid validator-set change: their CURRENT sets differ
// (height 11 vs height 12), but both still serve the SAME committed set at the
// value-chain block height H=10 being voted on. The set-root at H MUST be
// identical on both nodes — that identity is what makes their signatures over the
// block's canonical message mutually verifiable. The prior current-map read would
// have produced two different roots here and stalled finality.
func TestValidatorSetRoot_CrossNodeAgreesDespiteSkew(t *testing.T) {
	netID := ids.GenerateTestID()
	n0, n1, n2, n3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	pk0, pk1, pk2, pk3 := []byte("pk-0-48bytes-placeholder"), []byte("pk-1"), []byte("pk-2"), []byte("pk-3")

	const H = uint64(10)
	setAtH := []vdr{{n0, pk0, 10}, {n1, pk1, 20}, {n2, pk2, 30}}

	// Node A has already applied a stake bump that took effect at height 11
	// (n1: 20→25) and sees that as its current set. It still knows the height-10
	// set (the block being voted on).
	nodeA := stateWithHistory(netID, map[uint64][]vdr{
		H:  setAtH,
		11: {{n0, pk0, 10}, {n1, pk1, 25}, {n2, pk2, 30}},
	})
	// Node B has already applied a NEW validator that joined at height 12
	// (n3 added) — a different, later current set. It too still knows height 10.
	nodeB := stateWithHistory(netID, map[uint64][]vdr{
		H:  setAtH,
		12: {{n0, pk0, 10}, {n1, pk1, 25}, {n2, pk2, 30}, {n3, pk3, 5}},
	})

	rootA := newValidatorSetRootSource(nodeA, netID).ValidatorSetRoot(H)
	rootB := newValidatorSetRootSource(nodeB, netID).ValidatorSetRoot(H)

	if rootA == ids.Empty {
		t.Fatal("set-root at the voted height must be non-Empty (the set is non-empty)")
	}
	if rootA != rootB {
		t.Fatalf("MEDIUM-1: cross-node set-root at height %d MUST be identical despite "+
			"current-set skew, got A=%s B=%s", H, rootA, rootB)
	}

	// Sanity: had either node (wrongly) committed to its CURRENT set instead of
	// the height-H set, the roots WOULD differ — proving the test actually
	// exercises the skew (not a vacuous match).
	rootA_cur := newValidatorSetRootSource(nodeA, netID).ValidatorSetRoot(11)
	rootB_cur := newValidatorSetRootSource(nodeB, netID).ValidatorSetRoot(12)
	if rootA_cur == rootB_cur {
		t.Fatal("test is vacuous: the two nodes' CURRENT sets must differ to exercise the skew")
	}
	if rootA == rootA_cur {
		t.Fatal("test is vacuous: height-H set must differ from node A's current set")
	}
}

// TestValidatorSetRoot_HeightSelectsEpoch proves the root is a deterministic
// FUNCTION OF HEIGHT (not a fixed current-set snapshot): different heights with
// different sets yield different roots, the same height always yields the same
// root, and the encoding is insertion-order independent (canonical sort).
func TestValidatorSetRoot_HeightSelectsEpoch(t *testing.T) {
	netID := ids.GenerateTestID()
	n0, n1, n2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	pk0, pk1, pk2 := []byte("pk-0-48bytes-placeholder"), []byte("pk-1"), []byte("pk-2")

	state := stateWithHistory(netID, map[uint64][]vdr{
		10: {{n0, pk0, 10}, {n1, pk1, 20}, {n2, pk2, 30}},
		11: {{n0, pk0, 10}, {n1, pk1, 21}, {n2, pk2, 30}}, // n1 weight changed
	})
	src := newValidatorSetRootSource(state, netID)

	root10 := src.ValidatorSetRoot(10)
	root11 := src.ValidatorSetRoot(11)
	if root10 == ids.Empty || root11 == ids.Empty {
		t.Fatal("non-empty sets must commit to non-Empty roots")
	}
	if root10 == root11 {
		t.Fatal("a different epoch (height with a changed weight) MUST yield a different root")
	}
	// Same height is stable (deterministic), repeatedly.
	if again := src.ValidatorSetRoot(10); again != root10 {
		t.Fatalf("set-root at a fixed height must be deterministic: %s != %s", again, root10)
	}

	// Insertion-order independence: a state that lists the SAME height-10 members
	// in a different slice order yields the SAME root (canonical NodeID sort).
	reordered := stateWithHistory(netID, map[uint64][]vdr{
		10: {{n2, pk2, 30}, {n0, pk0, 10}, {n1, pk1, 20}},
	})
	if r := newValidatorSetRootSource(reordered, netID).ValidatorSetRoot(10); r != root10 {
		t.Fatalf("set-root must be member-order independent: %s != %s", r, root10)
	}
}

// TestValidatorSetRoot_FailSoftIsUniform proves the fail-soft answers are
// Empty/uniform (never a panic, never a per-node-divergent default): a nil state,
// an unknown height (empty set), an unknown network, and a height-read error all
// commit to ids.Empty. Uniformity is the safety property — a symmetric error
// degrades every node to the same Empty root, never to disagreeing roots.
func TestValidatorSetRoot_FailSoftIsUniform(t *testing.T) {
	netID := ids.GenerateTestID()

	// nil state → Empty.
	if got := (&validatorSetRootSource{state: nil, networkID: netID}).ValidatorSetRoot(10); got != ids.Empty {
		t.Fatalf("nil state must commit to ids.Empty, got %s", got)
	}

	// Known network, but a height with no registered set → Empty.
	state := stateWithHistory(netID, map[uint64][]vdr{
		10: {{ids.GenerateTestNodeID(), []byte("pk"), 10}},
	})
	if got := newValidatorSetRootSource(state, netID).ValidatorSetRoot(999); got != ids.Empty {
		t.Fatalf("unknown height must commit to ids.Empty, got %s", got)
	}

	// Wrong network → empty set → Empty.
	if got := newValidatorSetRootSource(state, ids.GenerateTestID()).ValidatorSetRoot(10); got != ids.Empty {
		t.Fatalf("unknown network must commit to ids.Empty, got %s", got)
	}

	// A height-read ERROR → Empty (and it is symmetric: the same error on every
	// node yields the same Empty root).
	errState := validatorstest.NewTestState()
	errState.GetValidatorSetF = func(_ context.Context, _ uint64, _ ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return nil, errors.New("state unavailable at height")
	}
	if got := newValidatorSetRootSource(errState, netID).ValidatorSetRoot(10); got != ids.Empty {
		t.Fatalf("a height-read error must commit to ids.Empty, got %s", got)
	}
}

// TestValidatorStakeSource_HeightPinned proves the ⅔-by-stake tally is read at
// the SAME height as the set-root (MEDIUM-1's second skew): Weight/TotalStake are
// a deterministic function of height, so a validator whose vote is in a
// height-H cert contributes its height-H weight — not whatever the current map
// happens to hold after a membership change. This is what stops a current-map
// weight read from dropping a legitimately-signed quorum.
func TestValidatorStakeSource_HeightPinned(t *testing.T) {
	netID := ids.GenerateTestID()
	n0, n1, n2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()

	state := stateWithHistory(netID, map[uint64][]vdr{
		10: {{n0, []byte("pk0"), 70}, {n1, []byte("pk1"), 30}},                       // total 100
		11: {{n0, []byte("pk0"), 70}, {n1, []byte("pk1"), 30}, {n2, []byte("pk2"), 50}}, // total 150
	})
	src := newValidatorStakeSource(state, netID)

	// At height 10 the tally is the height-10 epoch.
	if w := src.Weight(n0, 10); w != 70 {
		t.Fatalf("Weight(n0, h=10) = %d, want 70", w)
	}
	if total := src.TotalStake(10); total != 100 {
		t.Fatalf("TotalStake(h=10) = %d, want 100", total)
	}
	// n2 is NOT in the height-10 set → 0 at h=10, but 50 at h=11. The tally is
	// height-pinned, not current-map.
	if w := src.Weight(n2, 10); w != 0 {
		t.Fatalf("Weight(n2, h=10) = %d, want 0 (n2 joined at h=11)", w)
	}
	if w := src.Weight(n2, 11); w != 50 {
		t.Fatalf("Weight(n2, h=11) = %d, want 50", w)
	}
	if total := src.TotalStake(11); total != 150 {
		t.Fatalf("TotalStake(h=11) = %d, want 150", total)
	}

	// Unknown node at a known height → 0 (cannot inflate the numerator).
	if w := src.Weight(ids.GenerateTestNodeID(), 10); w != 0 {
		t.Fatalf("Weight(unknown, h=10) = %d, want 0", w)
	}
	// nil state → fail-soft zeros.
	nilSrc := &validatorStakeSource{state: nil, networkID: netID}
	if nilSrc.Weight(n0, 10) != 0 || nilSrc.TotalStake(10) != 0 {
		t.Fatal("nil state must yield zero weight and total")
	}
}

// TestHashValidatorSet_ByteStability is a GOLDEN test pinning the canonical
// set-root encoding so the wire format cannot drift (the engine's epoch-binding
// contract and any persisted/gossiped cert depend on this exact byte layout). If
// this value changes, the set-root encoding changed and every node in the
// network must upgrade in lockstep — it is a CONSENSUS-BREAKING change.
func TestHashValidatorSet_ByteStability(t *testing.T) {
	// Fixed (non-random) NodeIDs so the golden is reproducible.
	var a, b ids.NodeID
	a[0], b[0] = 0x01, 0x02
	set := map[ids.NodeID]*validators.GetValidatorOutput{
		a: {NodeID: a, PublicKey: []byte{0xaa, 0xbb}, Light: 10},
		b: {NodeID: b, PublicKey: []byte{0xcc}, Light: 20},
	}
	got := hashValidatorSet(set)

	// Independently recompute the expected commitment from the canonical spec:
	// sorted-by-NodeID, each nodeID || light(8,BE) || len(pk)(8,BE) || pk, SHA-256.
	want := expectedSetRoot(t, []vdr{
		{a, []byte{0xaa, 0xbb}, 10},
		{b, []byte{0xcc}, 20},
	})
	if got != want {
		t.Fatalf("set-root encoding drifted (CONSENSUS-BREAKING):\n got  %s\n want %s", got, want)
	}

	// Empty/nil set → ids.Empty.
	if hashValidatorSet(nil) != ids.Empty {
		t.Fatal("nil set must commit to ids.Empty")
	}
	if hashValidatorSet(map[ids.NodeID]*validators.GetValidatorOutput{}) != ids.Empty {
		t.Fatal("empty set must commit to ids.Empty")
	}
}
