// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// nova_poll_test.go — proof that the Nova poll path and the Quasar signed-vote
// path are separated AT THE TYPE LEVEL, and that separating them changed nothing
// about which votes are counted.
//
// The incident this encodes: every peer answered the poll with an unsigned Chits,
// the node widened each one into a signature-less consensuschain.Vote, and the
// engine dropped all of them at its authentication gate — 807/807 votes UNSIGNED,
// zero finalizations, every node-side counter healthy. alpha was unreachable by
// construction.
package chains

import (
	"context"
	"reflect"
	"testing"
	"time"

	consensusconfig "github.com/luxfi/consensus/config"
	consensuschain "github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
)

// --- (1) the type-level proof ------------------------------------------------

// TestNovaPollResponseCannotBecomeAQuasarVote is the compile-level guarantee,
// asserted structurally: a poll response has neither of the two fields that make
// a value countable, and it is neither assignable nor convertible to the vote
// type. So `consensuschain.Vote(resp)` and `var v Vote = resp` cannot compile,
// and no field-by-field copy can accidentally produce a signed-looking vote.
func TestNovaPollResponseCannotBecomeAQuasarVote(t *testing.T) {
	respT := reflect.TypeOf(NovaPollResponse{})
	voteT := reflect.TypeOf(consensuschain.Vote{})

	for _, forbidden := range []string{"Signature", "Accept"} {
		if _, ok := respT.FieldByName(forbidden); ok {
			t.Fatalf("NovaPollResponse carries a %q field — a Chits has no signature, and the "+
				"accept bit was never the peer's: re-adding it restores the unsigned-vote braid", forbidden)
		}
	}
	if respT.AssignableTo(voteT) || respT.ConvertibleTo(voteT) {
		t.Fatal("NovaPollResponse is assignable/convertible to consensuschain.Vote — an unsigned poll " +
			"response must not be usable where a Quasar vote is required")
	}

	// POSITIVE CONTROL for both probes: the vote type DOES carry the fields the
	// probe looks for, and IS convertible to itself. Without this, a typo in the
	// field names or a broken reflect call would pass the assertions above while
	// proving nothing.
	for _, present := range []string{"Signature", "Accept"} {
		if _, ok := voteT.FieldByName(present); !ok {
			t.Fatalf("positive control failed: consensuschain.Vote has no %q field — the probe above "+
				"cannot detect anything", present)
		}
	}
	if !voteT.ConvertibleTo(voteT) {
		t.Fatal("positive control failed: reflect says Vote is not convertible to Vote")
	}
}

// --- (2) the behavioural proof on a chain that authenticates votes -----------

// votesReceived reads the engine's inbound-vote counter. handleVote increments it
// at the TOP, before the signature gate, so it moves for ANY value that reaches
// the engine — which is exactly what makes it the right observable here: it
// detects the unsigned vote being handed over even though the engine would then
// drop it.
func votesReceived(t *testing.T, rt *consensuschain.Runtime) uint64 {
	t.Helper()
	n, ok := rt.Stats()["votes_received"].(uint64)
	if !ok {
		t.Fatal("engine Stats() has no uint64 votes_received — the observable this test relies on is gone")
	}
	return n
}

// waitVotesReceived polls until the counter reaches want or the deadline passes.
// The engine tallies on its own goroutine, so a bare read races the delivery.
func waitVotesReceived(t *testing.T, rt *consensuschain.Runtime, want uint64) uint64 {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := votesReceived(t, rt)
		if got >= want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// newPollFixture wires a REAL consensus runtime over a real block, plus the
// blockHandler under test. signedVotes selects the chain shape: true = a quorum
// chain (K>1, VoteVerifier wired — the engine authenticates every vote), false =
// a sole-validator chain (K==1, no verifier — an unsigned preference IS the Nova
// signal there and must keep working).
func newPollFixture(t *testing.T, signedVotes bool) (*consensuschain.Runtime, *blockHandler, *fakeInnerBlock) {
	t.Helper()

	netID := ids.GenerateTestID()
	self := newBLSValidator(t, 100)
	state := stateByHeight(netID, 1, map[uint64][]blsValidator{
		0: {self},
		1: {self},
	})

	vmStub := newFakeInnerVM()
	blk := newFakeInner(1, ids.GenerateTestID(), "poll-target")
	vmStub.register(blk)

	cfg := consensuschain.NetworkConfig{
		ChainID:   ids.GenerateTestID(),
		NetworkID: netID,
		NodeID:    self.nodeID,
		Logger:    log.NewNoOpLogger(),
		VM:        vmStub,
	}
	if signedVotes {
		p := params5()
		cfg.Params = &p
		cfg.VoteVerifier = newBLSVoteVerifier(state, netID)
	} else {
		p := consensusconfig.Parameters{K: 1, AlphaPreference: 1, AlphaConfidence: 1, Beta: 1}
		cfg.Params = &p
	}

	rt := consensuschain.NewRuntime(cfg)
	if err := rt.Start(context.Background(), true); err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(context.Background()) })

	bh := &blockHandler{
		logger:               log.NewNoOpLogger(),
		vm:                   vmStub,
		engine:               rt,
		chainID:              cfg.ChainID,
		networkID:            netID,
		signedVotesRequired:  signedVotes,
		pendingPollResponses: make(map[ids.ID][]NovaPollResponse),
	}
	return rt, bh, blk
}

// TestPollResponseNeverReachesTheEngineOnAQuorumChain is the regression guard for
// the live incident.
//
// fails-before: restore the old applyQbit body (build a signature-less
// consensuschain.Vote from the Chits and call engine.ReceiveVote on it) and the
// counter moves to 1 — the node hands the engine a vote it will drop, which is
// precisely how a chain polls K-of-K forever and never accepts.
func TestPollResponseNeverReachesTheEngineOnAQuorumChain(t *testing.T) {
	rt, bh, blk := newPollFixture(t, true /* signedVotesRequired */)
	ctx := context.Background()
	peer := ids.GenerateTestNodeID()

	before := votesReceived(t, rt)

	bh.receivePollResponse(ctx, NovaPollResponse{
		From:        peer,
		PreferredID: blk.ID(),
		RequestID:   7,
		ReceivedAt:  time.Now(),
	})

	// Give the engine's vote goroutine every chance to record a delivery.
	time.Sleep(250 * time.Millisecond)
	if got := votesReceived(t, rt); got != before {
		t.Fatalf("an UNSIGNED poll response reached the engine: votes_received %d -> %d. "+
			"A Chits carries no signature; the engine drops it at the authentication gate, so handing "+
			"it over is how alpha becomes unreachable while every counter reads healthy", before, got)
	}

	// Nothing may be parked either: buffering exists only to re-deliver later, and
	// on this chain there is nothing to re-deliver to.
	bh.pendingPollMu.Lock()
	parked := len(bh.pendingPollResponses)
	bh.pendingPollMu.Unlock()
	if parked != 0 {
		t.Fatalf("poll responses were buffered on a quorum chain (%d blocks parked) — the drain can only "+
			"lead back to the same boundary", parked)
	}

	// POSITIVE CONTROL: the observable is live. A value delivered through the
	// engine's own vote entry point DOES move the counter, so the assertion above
	// is measuring delivery, not a dead counter.
	if !rt.ReceiveVote(consensuschain.Vote{
		BlockID:  blk.ID(),
		NodeID:   peer,
		Accept:   true,
		SignedAt: time.Now(),
	}) {
		t.Fatal("positive control failed: engine.ReceiveVote refused to queue — the counter could not " +
			"have moved for any input, so the assertion above proves nothing")
	}
	if got := waitVotesReceived(t, rt, before+1); got != before+1 {
		t.Fatalf("positive control failed: votes_received stayed at %d after a direct ReceiveVote — "+
			"the observable is dead", got)
	}
}

// TestSoleValidatorChainStillFeedsThePollResponseToTheEngine proves the split is
// SCOPED: on a K==1 chain no verifier is wired, the engine's authentication
// branch is skipped, and the peer's unsigned preference is the Nova signal — so
// it must still be delivered exactly as before. This is the test that fails if
// the fix is over-applied and silently kills single-validator consensus.
func TestSoleValidatorChainStillFeedsThePollResponseToTheEngine(t *testing.T) {
	rt, bh, blk := newPollFixture(t, false /* signedVotesRequired */)
	ctx := context.Background()

	before := votesReceived(t, rt)

	bh.receivePollResponse(ctx, NovaPollResponse{
		From:        ids.GenerateTestNodeID(),
		PreferredID: blk.ID(),
		ReceivedAt:  time.Now(),
	})

	if got := waitVotesReceived(t, rt, before+1); got != before+1 {
		t.Fatalf("a sole-validator chain stopped delivering poll responses: votes_received %d -> %d", before, got)
	}
}

// TestUnheldBlockIsBufferedThenDrainedOnArrival keeps the K==1 buffer/drain
// behaviour honest: a preference for a block this node does not hold is parked,
// and reaches the engine when the block lands.
func TestUnheldBlockIsBufferedThenDrainedOnArrival(t *testing.T) {
	rt, bh, _ := newPollFixture(t, false /* signedVotesRequired */)
	ctx := context.Background()

	late := newFakeInner(2, ids.GenerateTestID(), "arrives-late")
	before := votesReceived(t, rt)

	bh.receivePollResponse(ctx, NovaPollResponse{
		From:        ids.GenerateTestNodeID(),
		PreferredID: late.ID(),
		ReceivedAt:  time.Now(),
	})

	bh.pendingPollMu.Lock()
	parked := len(bh.pendingPollResponses[late.ID()])
	bh.pendingPollMu.Unlock()
	if parked != 1 {
		t.Fatalf("preference for an unheld block was not parked: %d buffered", parked)
	}
	if got := votesReceived(t, rt); got != before {
		t.Fatalf("a parked preference reached the engine early: %d -> %d", before, got)
	}

	// The block arrives.
	bh.vm.(*fakeInnerVM).register(late)
	bh.onBlockArrived(ctx, late.ID())

	if got := waitVotesReceived(t, rt, before+1); got != before+1 {
		t.Fatalf("the drain did not deliver the parked preference: votes_received %d -> %d", before, got)
	}
	bh.pendingPollMu.Lock()
	stillParked := len(bh.pendingPollResponses)
	bh.pendingPollMu.Unlock()
	if stillParked != 0 {
		t.Fatalf("drain left %d blocks parked", stillParked)
	}
}

// --- (3) the transports: unsigned Chits out one door, signed votes the other --

// pollStubNet records what actually left the node. Every Network method other
// than Send/Gossip is inherited from the embedded nil interface and never called.
type pollStubNet struct {
	network.Network
	sent     []message.OutboundMessage
	gossiped []message.OutboundMessage
	peers    set.Set[ids.NodeID]
}

func (s *pollStubNet) Send(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ uint32) set.Set[ids.NodeID] {
	s.sent = append(s.sent, msg)
	return nodeIDs
}

func (s *pollStubNet) Gossip(msg message.OutboundMessage, _ set.Set[ids.NodeID], _ ids.ID, _ int, _ int, _ int) set.Set[ids.NodeID] {
	s.gossiped = append(s.gossiped, msg)
	return s.peers
}

// chitsRecord is one captured Chits call.
type chitsRecord struct {
	requestID           uint32
	preferredID         ids.ID
	preferredIDAtHeight ids.ID
	acceptedID          ids.ID
}

// pollStubMsg records the two builder calls the poll/vote transports make.
type pollStubMsg struct {
	message.OutboundMsgBuilder
	chits  []chitsRecord
	gossip [][]byte
}

func (m *pollStubMsg) Chits(_ ids.ID, requestID uint32, preferredID, preferredIDAtHeight, acceptedID ids.ID, _ uint64) (message.OutboundMessage, error) {
	m.chits = append(m.chits, chitsRecord{requestID, preferredID, preferredIDAtHeight, acceptedID})
	return nil, nil
}

func (m *pollStubMsg) Gossip(_ ids.ID, msg []byte) (message.OutboundMessage, error) {
	m.gossip = append(m.gossip, msg)
	return nil, nil
}

// TestSendPollResponseIsAnUnsignedChitsAndSendVoteIsTheSameThing proves the
// rename is a rename: SendVote's ONLY reason to exist is the consensuschain.
// Gossiper interface, whose method name claims a vote and whose body can only put
// an unsigned Chits on the wire. Both doors emit the identical message shape —
// requestID 0 for the interface method, the caller's requestID otherwise.
func TestSendPollResponseIsAnUnsignedChitsAndSendVoteIsTheSameThing(t *testing.T) {
	stubNet := &pollStubNet{}
	stubMsg := &pollStubMsg{}
	g := &networkGossiper{net: stubNet, msgCreator: stubMsg, networkID: ids.GenerateTestID(), log: log.NewNoOpLogger()}

	chainID := ids.GenerateTestID()
	peer := ids.GenerateTestNodeID()
	blockID := ids.GenerateTestID()

	if err := g.SendPollResponse(chainID, peer, 42, blockID); err != nil {
		t.Fatalf("SendPollResponse: %v", err)
	}
	if err := g.SendVote(chainID, peer, blockID); err != nil {
		t.Fatalf("SendVote: %v", err)
	}

	if len(stubMsg.chits) != 2 {
		t.Fatalf("expected 2 Chits builds, got %d", len(stubMsg.chits))
	}
	if len(stubMsg.gossip) != 0 {
		t.Fatalf("a poll response must never ride app-gossip (that is the SIGNED vote/cert wire): %d gossip builds", len(stubMsg.gossip))
	}
	if len(stubNet.sent) != 2 || len(stubNet.gossiped) != 0 {
		t.Fatalf("expected 2 unicast sends and 0 gossips, got %d/%d", len(stubNet.sent), len(stubNet.gossiped))
	}

	want := chitsRecord{requestID: 42, preferredID: blockID, preferredIDAtHeight: blockID, acceptedID: blockID}
	if stubMsg.chits[0] != want {
		t.Fatalf("SendPollResponse built %+v, want %+v", stubMsg.chits[0], want)
	}
	// SendVote is the same unsigned Chits with the interface's fixed requestID 0 —
	// byte-for-byte what the pre-rename body produced.
	want.requestID = 0
	if stubMsg.chits[1] != want {
		t.Fatalf("SendVote built %+v, want %+v (it must be the SAME unsigned Chits, not a vote)", stubMsg.chits[1], want)
	}
}

// TestBroadcastVoteStillCarriesTheSignedVoteOnAppGossip pins the REAL signed path
// unchanged: a signed vote goes out as a quorum-envelope app-gossip to ALL
// validators, carrying the signature bytes verbatim — it is not a Chits, it did
// not become one, and it is not unicast to a proposer.
func TestBroadcastVoteStillCarriesTheSignedVoteOnAppGossip(t *testing.T) {
	peers := set.NewSet[ids.NodeID](3)
	for i := 0; i < 3; i++ {
		peers.Add(ids.GenerateTestNodeID())
	}
	stubNet := &pollStubNet{peers: peers}
	stubMsg := &pollStubMsg{}
	g := &networkGossiper{net: stubNet, msgCreator: stubMsg, networkID: ids.GenerateTestID(), log: log.NewNoOpLogger()}

	chainID := ids.GenerateTestID()
	blockID := ids.GenerateTestID()
	voteBytes := []byte("signed-vote-bytes")

	sent := g.BroadcastVote(chainID, g.networkID, blockID, voteBytes)
	if sent != peers.Len() {
		t.Fatalf("BroadcastVote reached %d peers, want %d", sent, peers.Len())
	}
	if len(stubMsg.chits) != 0 {
		t.Fatalf("BroadcastVote built a Chits — the signed path must never degrade to an unsigned preference")
	}
	if len(stubMsg.gossip) != 1 || len(stubNet.gossiped) != 1 || len(stubNet.sent) != 0 {
		t.Fatalf("expected exactly 1 gossip and 0 unicast sends, got gossip=%d/%d send=%d",
			len(stubMsg.gossip), len(stubNet.gossiped), len(stubNet.sent))
	}

	kind, gotBlock, payload, err := decodeQuorumGossip(stubMsg.gossip[0])
	if err != nil {
		t.Fatalf("decodeQuorumGossip: %v", err)
	}
	if kind != quorumKindVote {
		t.Fatalf("envelope kind %d, want quorumKindVote %d", kind, quorumKindVote)
	}
	if gotBlock != blockID {
		t.Fatalf("envelope blockID %s, want %s", gotBlock, blockID)
	}
	if string(payload) != string(voteBytes) {
		t.Fatalf("envelope payload %q, want the signed vote bytes %q", payload, voteBytes)
	}
}

// compile-time: the interface surface consensus depends on is intact after the
// rename — networkGossiper is still both a Gossiper (poll transport) and a
// QuorumGossiper (the signed vote/cert transport).
var (
	_ consensuschain.Gossiper       = (*networkGossiper)(nil)
	_ consensuschain.QuorumGossiper = (*networkGossiper)(nil)
)
