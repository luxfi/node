// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/core/router"
	"github.com/luxfi/ids"
)

// TestToConsensusOp_GossipIsRouted is the regression guard for the finality
// wedge: the α-of-K quorum vote/cert transport rides on app-gossip (GossipOp).
// The chain router (node/chain_router.go) gates EVERY inbound message through
// ToConsensusOp; an op that does not map is dropped as "unhandled message op"
// BEFORE it can reach the engine. GossipOp was missing from the table, so every
// broadcast vote was silently dropped at the router — blocks (PutOp) propagated
// and verified, but no vote ever reached HandleIncomingVote, the cert never
// assembled, and the chain wedged at "verified but never accepted".
//
// This asserts GossipOp maps to the consensus router's Gossip op so votes are
// delivered to blockHandler.Gossip -> engine.HandleIncomingVote.
func TestToConsensusOp_GossipIsRouted(t *testing.T) {
	require := require.New(t)

	got, ok := ToConsensusOp(GossipOp)
	require.True(ok, "GossipOp MUST map to a consensus router op — votes ride app-gossip; "+
		"an unmapped GossipOp is dropped by the chain router and finality wedges")
	require.Equal(byte(router.Gossip), got,
		"GossipOp must map to router.Gossip so blockHandler routes it to engine.HandleIncomingVote")
}

// TestToConsensusOp_TableAlignedWithRouter pins the full message-op -> router-op
// table. Every op the chain router forwards to a consensus handler must map to
// the SAME byte the consensus side decodes it as (handler.Op == router.Op). A
// drift here re-breaks routing for that op exactly as the missing GossipOp broke
// vote delivery.
func TestToConsensusOp_TableAlignedWithRouter(t *testing.T) {
	require := require.New(t)

	cases := []struct {
		name string
		in   Op
		want router.Op
	}{
		{"GetAcceptedFrontier", GetAcceptedFrontierOp, router.GetAcceptedFrontier},
		{"AcceptedFrontier", AcceptedFrontierOp, router.AcceptedFrontier},
		{"GetAccepted", GetAcceptedOp, router.GetAccepted},
		{"Accepted", AcceptedOp, router.Accepted},
		{"Get", GetOp, router.Get},
		{"Put", PutOp, router.Put},
		{"PushQuery", PushQueryOp, router.PushQuery},
		{"PullQuery", PullQueryOp, router.PullQuery},
		{"Qbit/Vote", QbitOp, router.Vote},
		{"GetAncestors/GetContext", GetAncestorsOp, router.GetContext},
		{"Ancestors/Context", AncestorsOp, router.Context},
		{"Gossip", GossipOp, router.Gossip},
	}

	for _, tc := range cases {
		got, ok := ToConsensusOp(tc.in)
		require.True(ok, "%s must map to a consensus router op", tc.name)
		require.Equal(byte(tc.want), got, "%s maps to the wrong router op", tc.name)
	}
}

// TestInboundGossip_DeliveredNotDropped reproduces the chain router's inbound
// extraction for a quorum vote envelope (the exact steps node/chain_router.go
// performs before dispatch) and asserts the vote SURVIVES routing: the op maps
// to router.Gossip AND the envelope bytes are recovered intact as the container.
// This is the seam the finality wedge lived in — a vote that the router dropped
// here never reached the engine. The bytes that come out here are what
// blockHandler.Gossip demuxes into engine.HandleIncomingVote.
func TestInboundGossip_DeliveredNotDropped(t *testing.T) {
	require := require.New(t)

	chainID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	// Stand-in for an encodeQuorumGossip envelope: magic + kind + blockID + vote.
	envelope := []byte("LXQ\x01" + "<signed-vote-payload>")

	inbound := InboundGossip(chainID, envelope, nodeID)

	// 1) The router maps the op (else "unhandled message op" -> dropped).
	consensusOp, ok := ToConsensusOp(inbound.Op())
	require.True(ok, "inbound vote gossip must route to a consensus op")
	require.Equal(byte(router.Gossip), consensusOp)

	// 2) The router recovers the envelope as the handler container bytes.
	got := GetContainerBytes(inbound.Message())
	require.Equal(envelope, got, "the quorum envelope must survive router extraction intact")
}
