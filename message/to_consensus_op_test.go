// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	handler "github.com/luxfi/consensus/networking/handler"
)

// TestToConsensusOpGossipRoutes is the regression guard for the finality wedge:
// the α-of-K quorum vote/cert envelopes (BroadcastVote / GossipCert) ride on
// p2p app-Gossip (GossipOp). Before the fix, ToConsensusOp had no GossipOp case
// and returned (0, false), so the inbound chain router dropped every vote/cert
// as "unhandled message op" — the chain handler's Gossip demux
// (HandleIncomingVote / HandleIncomingCert) was unreachable, no quorum cert ever
// assembled, and cert-gated finality never fired (zero Chits, frozen height) on
// every K>1 network.
//
// This test asserts GossipOp now maps to a consensus op AND that the mapped byte
// equals handler.Gossip, so the node-side router converts GossipOp into exactly
// the consensus op the per-chain handler dispatches to its Gossip method.
func TestToConsensusOpGossipRoutes(t *testing.T) {
	consensusOp, ok := ToConsensusOp(GossipOp)
	if !ok {
		t.Fatal("GossipOp must map to a consensus op (vote/cert envelopes ride on app-gossip); got ok=false -> inbound votes/certs are dropped and finality stalls")
	}
	if got, want := consensusOp, byte(handler.Gossip); got != want {
		t.Fatalf("GossipOp must map to handler.Gossip: got byte %d, want %d", got, want)
	}
}

// TestToConsensusOpWireBytesStable pins the byte values of the consensus ops that
// cross the wire so the Gossip addition (appended last) cannot silently renumber
// an existing op. PushQuery=6, Qbit/Vote=8, Context=10 are the load-bearing
// polling/finality ops; Gossip is 11 (appended after Context).
func TestToConsensusOpWireBytesStable(t *testing.T) {
	cases := []struct {
		op   Op
		want byte
	}{
		{PutOp, 5},
		{PushQueryOp, 6},
		{PullQueryOp, 7},
		{QbitOp, 8},
		{GetAncestorsOp, 9},  // GetContext on the consensus side
		{AncestorsOp, 10},    // Context on the consensus side
		{GossipOp, 11},       // Gossip (appended last)
	}
	for _, c := range cases {
		got, ok := ToConsensusOp(c.op)
		if !ok {
			t.Errorf("op %s must map to a consensus op", c.op)
			continue
		}
		if got != c.want {
			t.Errorf("op %s mapped to byte %d, want %d (wire compatibility / op renumber guard)", c.op, got, c.want)
		}
	}
	// handler.Gossip must equal the byte ToConsensusOp emits for GossipOp, or the
	// node router would convert GossipOp to a different handler op than the
	// per-chain handler's Gossip-demux case expects.
	if byte(handler.Gossip) != 11 {
		t.Errorf("handler.Gossip = %d, want 11 (must match ToConsensusOp(GossipOp))", byte(handler.Gossip))
	}
}
