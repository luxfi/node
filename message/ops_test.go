// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/core/router"
	"github.com/luxfi/ids"
)

// TestToConsensusOp_TableAlignedWithRouter guards the whole class, not the one
// Gossip instance. The chain router (node/chain_router.go) forwards an inbound
// message only if ToConsensusOp maps it, then dispatches on handler.Op(value).
// The node op table and the consensus router op enum are therefore two halves of
// ONE routing contract: if they diverge, every message for the divergent op is
// silently dropped. An unmapped router.Gossip is enough to stop α-of-K finality
// dead — blocks ride PutOp and verify, broadcast votes ride the unmapped GossipOp
// and vanish before blockHandler.Gossip -> engine.HandleIncomingVote, so the cert
// never reaches alpha.
//
// The test proves ToConsensusOp is a BIJECTION onto the router op space
// [0, router.NumOps), so divergence is un-shippable:
//   - exhaustive/surjective: every router op has exactly one node-op preimage,
//     so a router op added without a node mapping fails here.
//   - injective/well-typed: no two node ops collide onto one router op and
//     nothing maps outside the op space.
//
// router.NumOps is the single source of truth for the op count, so a future op
// added to one table but not the other fails HERE, not at runtime.
func TestToConsensusOp_TableAlignedWithRouter(t *testing.T) {
	require := require.New(t)

	// want: the authoritative router-op <-> node-op correspondence.
	want := map[router.Op]Op{
		router.GetAcceptedFrontier: GetAcceptedFrontierOp,
		router.AcceptedFrontier:    AcceptedFrontierOp,
		router.GetAccepted:         GetAcceptedOp,
		router.Accepted:            AcceptedOp,
		router.Get:                 GetOp,
		router.Put:                 PutOp,
		router.PushQuery:           PushQueryOp,
		router.PullQuery:           PullQueryOp,
		router.Vote:                QbitOp,         // votes ride the Qbit wire op
		router.GetContext:          GetAncestorsOp, // wire op is still GetAncestors
		router.Context:             AncestorsOp,    // wire op is still Ancestors
		router.Gossip:              GossipOp,       // α-of-K vote/cert transport
		router.GetStateSummaryFrontier: GetStateSummaryFrontierOp,
		router.StateSummaryFrontier:    StateSummaryFrontierOp,
		router.GetAcceptedStateSummary: GetAcceptedStateSummaryOp,
		router.AcceptedStateSummary:    AcceptedStateSummaryOp,
		router.AppRequest:              RequestOp,  // AppRequest — EVM state-sync leaf/code fetch
		router.AppResponse:             ResponseOp, // AppResponse
		router.AppError:                ErrorOp,    // AppError
	}

	// EXHAUSTIVE: the table must name every router op exactly once. Pinned to
	// router.NumOps, so a new router op with no entry here fails immediately.
	require.Len(want, int(router.NumOps),
		"node op table names %d ops but the router defines router.NumOps=%d — a "+
			"router op with no node mapping is dropped by the chain router and "+
			"finality wedges; add it to want and to ToConsensusOp",
		len(want), int(router.NumOps))

	// BIJECTION: rebuild the actual inverse mapping by sweeping the whole node op
	// space (Op is a byte, so [0,256)) and require it to equal want. This one
	// comparison catches a missing mapping (router op absent from got), a wrong
	// target (got[r] != want[r]) and a stray mapping into the op space; the dup
	// guard catches a collision masked by the later write winning the slot.
	got := make(map[router.Op]Op, int(router.NumOps))
	for i := 0; i < 256; i++ {
		nodeOp := Op(i)
		v, ok := ToConsensusOp(nodeOp)
		if !ok {
			continue
		}
		require.Less(int(v), int(router.NumOps),
			"%s maps to router op %d outside [0, NumOps=%d)", nodeOp, v, int(router.NumOps))
		routerOp := router.Op(v)
		_, dup := got[routerOp]
		require.False(dup,
			"router op %d is mapped from two node ops (%s and %s) — ambiguous routing",
			v, got[routerOp], nodeOp)
		got[routerOp] = nodeOp
	}
	require.Equal(want, got,
		"node op table diverged from the router op space: every router op "+
			"[0, NumOps) must have exactly one node-op preimage and each node op must "+
			"map to its assigned router op. A divergence here is the finality-wedge "+
			"bug class — an op routed in one table and dropped in the other.")
}

// TestInboundGossip_DeliveredNotDropped reproduces the chain router's inbound
// extraction for a quorum vote envelope (the exact steps node/chain_router.go
// performs before dispatch) and asserts the vote SURVIVES routing: the op maps
// to router.Gossip AND the envelope bytes are recovered intact as the container.
// A vote the router drops here never reaches the engine, and nothing downstream
// can tell that from a vote nobody cast. The bytes that come out here are what
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
