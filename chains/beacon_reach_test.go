// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// beacon_reach_test.go — the peer that can serve must be reachable by the sample.
//
// A node that has fallen behind recovers by asking a peer that holds what it is
// missing. Everything downstream of that — the descent, the fold, the accept —
// is only as good as whether the ask reaches such a peer at all. These pin the
// rule that keeps it possible: a connected, staked beacon is never dropped from
// the candidate set by a predicate whose job is to ORDER it.

package chains

import (
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/log"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/network/peer"
)

// reachNet reports every peer as connected, and lets each one decide whether it
// advertises the chain — which is the only axis these tests vary.
type reachNet struct {
	network.Network
	peers     []ids.NodeID
	advertise map[ids.NodeID]bool
	tracked   ids.ID
}

func (n *reachNet) PeerInfo(nodeIDs []ids.NodeID) []peer.Info {
	want := map[ids.NodeID]bool{}
	for _, id := range nodeIDs {
		want[id] = true
	}
	var out []peer.Info
	for _, p := range n.peers {
		if len(nodeIDs) != 0 && !want[p] {
			continue
		}
		info := peer.Info{ID: p}
		if n.advertise[p] {
			info.TrackedChains = set.Of(n.tracked)
		}
		out = append(out, info)
	}
	return out
}

// reachHandler wires a handler whose staked beacon set is exactly `staked` and
// whose network reports `advertise` for the tracked-chains predicate.
func reachHandler(t *testing.T, staked []ids.NodeID, advertise map[ids.NodeID]bool) (*blockHandler, map[ids.NodeID]uint64) {
	t.Helper()
	netID := ids.GenerateTestID()
	weights := map[ids.NodeID]uint64{}
	for _, id := range staked {
		weights[id] = 1
	}
	bh := &blockHandler{
		logger:    log.NewNoOpLogger(),
		networkID: netID,
	}
	bh.net = &reachNet{peers: staked, advertise: advertise, tracked: netID}
	return bh, weights
}

// TestPartialFilterKeepsEveryStakedBeacon is the failure this file exists for.
//
// The tracked-chains predicate is an optimisation: a peer advertising the chain
// is likelier to serve it. Rescuing only the case where it admits NOBODY leaves
// the partial case, and the partial case is worse — some beacons pass, so the
// candidate set is non-empty, the rescue never fires, and the peer holding the
// exact ancestry we lack is excluded for the life of the process.
func TestPartialFilterKeepsEveryStakedBeacon(t *testing.T) {
	advertises := ids.GenerateTestNodeID()
	holdsTheGap := ids.GenerateTestNodeID()

	bh, weights := reachHandler(t, []ids.NodeID{advertises, holdsTheGap}, map[ids.NodeID]bool{
		advertises: true, // holdsTheGap does not advertise, but is staked and connected
	})

	got := bh.connectedBeacons(weights)

	if !hasID(got, holdsTheGap) {
		t.Fatal("a connected staked beacon was dropped for not advertising the chain — " +
			"if it is the only peer holding the missing ancestry, the node can never rejoin")
	}
	if !hasID(got, advertises) {
		t.Fatal("the beacon that does advertise the chain must still be a candidate")
	}
	if got[0] != advertises {
		t.Fatal("advertising peers must come FIRST — the predicate orders, it just may not exclude")
	}
}

// TestNoFilterMatchStillAsksEveryone: the all-or-nothing case, which already
// worked and must keep working.
func TestNoFilterMatchStillAsksEveryone(t *testing.T) {
	a, b := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	bh, weights := reachHandler(t, []ids.NodeID{a, b}, map[ids.NodeID]bool{})

	if got := bh.connectedBeacons(weights); len(got) != 2 {
		t.Fatalf("predicate admitted nobody, so every connected staked beacon is a candidate; got %d", len(got))
	}
}

// TestEveryBeaconPassesAsksEachOnce: a peer must not be asked twice for
// appearing on both sides of the rule.
func TestEveryBeaconPassesAsksEachOnce(t *testing.T) {
	a, b := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	bh, weights := reachHandler(t, []ids.NodeID{a, b}, map[ids.NodeID]bool{a: true, b: true})

	if got := bh.connectedBeacons(weights); len(got) != 2 {
		t.Fatalf("every beacon passed, so the set is unchanged; got %d", len(got))
	}
}

// TestUnstakedPeerIsNeverAdmitted: widening the predicate must not widen who
// counts as a beacon. A connected peer outside the staked set stays out.
func TestUnstakedPeerIsNeverAdmitted(t *testing.T) {
	staked := ids.GenerateTestNodeID()
	stranger := ids.GenerateTestNodeID()

	bh, weights := reachHandler(t, []ids.NodeID{staked}, map[ids.NodeID]bool{})
	// The network sees the stranger; the staked set does not contain it.
	bh.net.(*reachNet).peers = []ids.NodeID{staked, stranger}

	if got := bh.connectedBeacons(weights); hasID(got, stranger) {
		t.Fatal("a peer outside the staked validator set became a candidate")
	}
}

func hasID(xs []ids.NodeID, x ids.NodeID) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
