// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// frontier_naming_unit_test.go — focused, transport-free unit tests for the three fresh-net
// frontier-naming guards introduced/changed on this branch:
//
//   - repliedCovers         — set-CONTAINMENT (not a length count) of "did every connected
//     beacon answer this round", the eclipse/silent-beacon guard.
//   - fullyConnectedBeacons — "have we reached the ENTIRE external beacon set" (the self-vote
//     pre-condition).
//   - withSelfVote          — appends the node's OWN accepted tip as a beacon reply, ONLY when
//     the node is itself a beacon and holds a tip.
//
// These are exercised end-to-end by the FrontierTip tests in bootstrap_sync_test.go and the
// adversarial probe in zz_red_probe_test.go; here we pin each function's CONTRACT directly so a
// regression in any one of them is localized, not buried in a full-loop failure.
package chains

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

// TestRepliedCovers pins the CONTAINMENT semantics of repliedCovers — the heart of the
// connected-but-silent eclipse defense. The decisive case is "count-passes, containment-fails":
// a length check (len(replies) >= len(connected)) would admit a round where a CONNECTED beacon
// stayed silent but a stale/non-connected reply padded the slice. Containment correctly rejects
// it, so a silent ahead-beacon blocks caught-up and the node fails safe.
func TestRepliedCovers(t *testing.T) {
	a, b, c := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	tip := ids.GenerateTestID()
	reply := func(id ids.NodeID) BeaconReply { return BeaconReply{NodeID: id, Tip: tip, Weight: 1} }

	tests := []struct {
		name      string
		replies   []BeaconReply
		connected []ids.NodeID
		want      bool
	}{
		{"empty connected is vacuously covered", []BeaconReply{reply(a)}, nil, true},
		{"no replies, no connected", nil, nil, true},
		{"no replies but connected expected", nil, []ids.NodeID{a}, false},
		{"all connected replied", []BeaconReply{reply(a), reply(b)}, []ids.NodeID{a, b}, true},
		{"one connected silent", []BeaconReply{reply(a)}, []ids.NodeID{a, b}, false},
		{"extra non-connected replies still cover", []BeaconReply{reply(a), reply(b), reply(c)}, []ids.NodeID{a, b}, true},
		// THE PADDING ATTACK: |replies| == |connected| == 2, so a COUNT check passes — but the
		// connected beacon b is SILENT and c (stale / not connected this round) pads the slice.
		// Containment must REJECT: b never answered, so caught-up cannot be concluded.
		{"count-passes containment-fails: silent b padded by stale c", []BeaconReply{reply(a), reply(c)}, []ids.NodeID{a, b}, false},
		// Duplicate replies from the same connected beacon must not "cover" for a different one.
		{"duplicate of a does not cover for b", []BeaconReply{reply(a), reply(a)}, []ids.NodeID{a, b}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, repliedCovers(tt.replies, tt.connected))
		})
	}
}

// TestFullyConnectedBeacons pins the eclipse-free pre-condition for the self-vote: the node has
// reached EVERY beacon other than itself. The external-set size is the beacon set minus self; a
// degenerate self-only set has an empty external set and must return false (the self-vote can
// never be the SOLE basis for caught-up).
func TestFullyConnectedBeacons(t *testing.T) {
	self := ids.GenerateTestNodeID()
	a, b := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	bh := &blockHandler{selfNodeID: self}

	tests := []struct {
		name      string
		weights   map[ids.NodeID]uint64
		connected []ids.NodeID
		want      bool
	}{
		// self IS a beacon: external set = {a, b}; both connected → full.
		{"self-beacon, full external set reached", map[ids.NodeID]uint64{self: 60, a: 20, b: 20}, []ids.NodeID{a, b}, true},
		// self IS a beacon but one external beacon is suppressed (eclipse) → not full.
		{"self-beacon, one external suppressed", map[ids.NodeID]uint64{self: 60, a: 20, b: 20}, []ids.NodeID{a}, false},
		// degenerate self-only beacon set: external set is empty → never full (self-vote can't stand alone).
		{"self-only set is never full", map[ids.NodeID]uint64{self: 100}, nil, false},
		// self is NOT a beacon (e.g. a P-chain CustomBeacons set omitting self): external = whole set.
		{"non-beacon self, full set reached", map[ids.NodeID]uint64{a: 50, b: 50}, []ids.NodeID{a, b}, true},
		{"non-beacon self, partial", map[ids.NodeID]uint64{a: 50, b: 50}, []ids.NodeID{a}, false},
		{"empty beacon set", map[ids.NodeID]uint64{}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, bh.fullyConnectedBeacons(tt.weights, tt.connected))
		})
	}
}

// TestWithSelfVote pins when the node vouches for its OWN accepted tip. self only ever vouches
// for a tip it has ACCEPTED (lastID), and only when it is itself a beacon (in weights) — so it
// can never name a forged tip (C1 untouched) and is inert on a set that omits self.
func TestWithSelfVote(t *testing.T) {
	self := ids.GenerateTestNodeID()
	a := ids.GenerateTestNodeID()
	lastID := ids.GenerateTestID()
	bh := &blockHandler{selfNodeID: self}
	base := []BeaconReply{{NodeID: a, Tip: ids.GenerateTestID(), Weight: 40}}

	t.Run("self is a beacon with an accepted tip — appends the self-vote", func(t *testing.T) {
		weights := map[ids.NodeID]uint64{self: 60, a: 40}
		out := bh.withSelfVote(base, weights, lastID)
		require.Len(t, out, 2, "self-vote appended")
		require.Len(t, base, 1, "input slice must not be mutated")

		var selfReply *BeaconReply
		for i := range out {
			if out[i].NodeID == self {
				selfReply = &out[i]
			}
		}
		require.NotNil(t, selfReply, "the self-vote must be present")
		require.Equal(t, lastID, selfReply.Tip, "self vouches for its OWN accepted tip")
		require.Equal(t, uint64(60), selfReply.Weight, "self-vote carries self's beacon stake")
	})

	t.Run("self is not in the beacon set — returned unchanged", func(t *testing.T) {
		weights := map[ids.NodeID]uint64{a: 40}
		out := bh.withSelfVote(base, weights, lastID)
		require.Equal(t, base, out, "a set that omits self leaves the self-vote inert")
	})

	t.Run("no accepted tip — returned unchanged", func(t *testing.T) {
		weights := map[ids.NodeID]uint64{self: 60, a: 40}
		out := bh.withSelfVote(base, weights, ids.Empty)
		require.Equal(t, base, out, "without an accepted tip self has nothing to vouch for")
	})
}
