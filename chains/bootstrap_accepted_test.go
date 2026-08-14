// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// bootstrap_accepted_test.go — the second naming question, end to end.
//
// A live chain moves while frontier answers are in flight, so healthy honest peers report
// different tips and no set of tips clears an agreement threshold. Acceptance is cumulative and
// does not split that way: every one of those peers has accepted the lower tips. These tests pin
// the three halves of that round trip — a node answers which of the asked blocks it has ACCEPTED
// (never merely holds), a behind node names the highest block a stake quorum answers for, and a
// node that is not behind asks nothing at all — plus the drop rules that keep an answer nobody
// asked for out of the tally.
package chains

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
)

// ----- policy-level helpers -------------------------------------------------

// stubAcceptance answers the second question the way a real beacon does: each responder has
// ACCEPTED every candidate at or below its own height, and nothing above it. `heightOf` resolves a
// candidate to its height on the one honest chain.
type stubAcceptance struct {
	at       map[ids.NodeID]uint64 // each beacon's accepted height
	heightOf map[ids.ID]uint64     // candidate -> height
	asked    [][]ids.ID            // every question, in order
	from     [][]ids.NodeID        // who each question went to
	silent   set.Set[ids.NodeID]   // connected, but answers nothing
}

func (s *stubAcceptance) Acceptance(_ context.Context, candidates []ids.ID, from []ids.NodeID) []AcceptedReply {
	s.asked = append(s.asked, candidates)
	s.from = append(s.from, from)
	out := make([]AcceptedReply, 0, len(from))
	for _, id := range from {
		if s.silent.Contains(id) {
			continue
		}
		var accepted []ids.ID
		for _, c := range candidates {
			if s.heightOf[c] <= s.at[id] {
				accepted = append(accepted, c)
			}
		}
		out = append(out, AcceptedReply{NodeID: id, Accepted: accepted})
	}
	return out
}

// spreadFleet builds the shape a live chain produces: `n` equal-weight beacons whose accepted
// heights ascend by one from `base`, each reporting its own tip. Returns the policy (with no
// AncestrySource — the answer must come from acceptance alone), the frontier replies, and the
// per-height tips.
func spreadFleet(t *testing.T, n int, base uint64, own uint64) (*BootstrapPolicy, []BeaconReply, []BlockRef, *stubAcceptance) {
	t.Helper()
	const w uint64 = 100

	refs, _ := refChain(int(base) + n)
	beacons := nodeIDs(n)
	acc := &stubAcceptance{at: map[ids.NodeID]uint64{}, heightOf: map[ids.ID]uint64{}, silent: set.NewSet[ids.NodeID](1)}
	for _, r := range refs {
		acc.heightOf[r.ID] = r.Height
	}
	replies := make([]BeaconReply, 0, n)
	for i, id := range beacons {
		h := base + uint64(i)
		acc.at[id] = h
		replies = append(replies, reply(id, refs[h].ID, w))
	}
	policy := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(beacons, w),
		MinResponses:      n/2 + 1,
		MinResponders:     bootstrapMinAgreeingBeacons,
		MinFrontierHeight: own,
		Tip:               refs[own].ID,
		Acceptance:        acc,
	}
	return policy, replies, refs, acc
}

// ----- the tally ------------------------------------------------------------

// TestAccepted_NamesTheHighestBlockAQuorumHolds is the whole fix at the decision layer. Four
// beacons sit on four different heads — the ordinary state of a live chain, and the state three
// stopped networks were left in — so no tip carries a supermajority and nothing is nameable from
// the tips. Asking which of those tips each beacon has ACCEPTED breaks the tie immediately: all
// four accepted the lowest, three the next, and the highest block backed by more than ⅔ of
// responder stake is named.
func TestAccepted_NamesTheHighestBlockAQuorumHolds(t *testing.T) {
	policy, replies, refs, acc := spreadFleet(t, 4, 100, 90)

	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err, "four beacons on four heads must still name a frontier")
	require.Equal(t, refs[101].ID, f.ID,
		"101 is the highest block a >⅔ responder-stake quorum has accepted (3 of 4); 100 is lower, 102 is held by 2 of 4")

	require.Len(t, acc.asked, 1, "one question, asked once")
	require.Len(t, acc.asked[0], 4, "the question names the collected tips")
	require.Len(t, acc.from[0], 4, "and goes to the beacons that reported them")
}

// TestAccepted_NeverAboveWhatAQuorumHolds is the safety line. The two highest heads are held by a
// minority, and a minority — even one that answers loudly and consistently — cannot make this node
// start above the block a quorum actually has. Naming stops exactly where the stake does.
func TestAccepted_NeverAboveWhatAQuorumHolds(t *testing.T) {
	policy, replies, refs, _ := spreadFleet(t, 4, 100, 90)

	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err)
	require.NotEqual(t, refs[103].ID, f.ID, "the top head is one beacon's — never a start point")
	require.NotEqual(t, refs[102].ID, f.ID, "102 is held by 2 of 4: not a quorum")
}

// TestAccepted_BelowTheResponseFloorNamesNothing: the response floor is the SAME one the tips are
// judged by, so an eclipse that suppresses the honest answers cannot capture the second question
// either. Two of four beacons answering is below the floor — nothing is named, whatever they say.
func TestAccepted_BelowTheResponseFloorNamesNothing(t *testing.T) {
	policy, replies, _, acc := spreadFleet(t, 4, 100, 90)
	for i, r := range replies {
		if i >= 2 {
			acc.silent.Add(r.NodeID)
		}
	}

	_, err := policy.AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrNoBootstrapQuorum,
		"below the response floor the second question names nothing — the same floor the first is held to")
}

// TestAccepted_UnconfiguredAnswersAreIgnored: eligibility is the trust anchor, and it is the same
// one for both questions. A peer that is not a configured beacon can answer anything, at any
// weight, and contribute nothing.
func TestAccepted_UnconfiguredAnswersAreIgnored(t *testing.T) {
	policy, replies, refs, acc := spreadFleet(t, 4, 100, 90)
	forged := ids.GenerateTestID()
	acc.heightOf[forged] = 500
	stranger := ids.GenerateTestNodeID()
	acc.at[stranger] = 500

	// The stranger answers for a block far above every beacon's head.
	base := acc.Acceptance
	policy.Acceptance = acceptanceFunc(func(ctx context.Context, candidates []ids.ID, from []ids.NodeID) []AcceptedReply {
		return append(base(ctx, candidates, from), AcceptedReply{NodeID: stranger, Accepted: []ids.ID{forged}})
	})

	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err)
	require.Equal(t, refs[101].ID, f.ID, "a peer outside the configured beacon set names nothing")
}

// TestAccepted_OneBeaconCannotVoteTwice: a beacon repeating an id inside one answer, or answering
// twice, is worth its stake once. Otherwise a single beacon could clear a threshold by talking.
func TestAccepted_OneBeaconCannotVoteTwice(t *testing.T) {
	beacons := nodeIDs(3)
	block := ids.GenerateTestID()
	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(beacons, 100), MinResponses: 2}

	responders, weight, held, heldBy := policy.tally([]AcceptedReply{
		{NodeID: beacons[0], Accepted: []ids.ID{block, block, block}},
		{NodeID: beacons[0], Accepted: []ids.ID{block}},
	})
	require.Equal(t, 1, responders)
	require.Equal(t, uint64(100), weight)
	require.Equal(t, uint64(100), held[block], "one beacon, one vote, however often it says it")
	require.Len(t, heldBy[block], 1)
}

// acceptanceFunc adapts a function to AcceptanceSource.
type acceptanceFunc func(context.Context, []ids.ID, []ids.NodeID) []AcceptedReply

func (f acceptanceFunc) Acceptance(ctx context.Context, candidates []ids.ID, from []ids.NodeID) []AcceptedReply {
	return f(ctx, candidates, from)
}

// ----- the responder --------------------------------------------------------

// captureNet records what a handler sends, so a test can read the answer a peer would receive.
type captureNet struct {
	network.Network
	sent []*bsOutMsg
	to   []set.Set[ids.NodeID]
}

func (n *captureNet) Send(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ uint32) set.Set[ids.NodeID] {
	if m, ok := msg.(*bsOutMsg); ok {
		n.sent = append(n.sent, m)
		n.to = append(n.to, nodeIDs)
	}
	return nodeIDs
}

// TestAccepted_ResponderAnswersOnlyForBlocksItAccepted: holding a block is not having accepted it.
// A block gossiped into the store ahead of acceptance is not on this node's chain, and answering
// for it would vouch for a block this node never finalized — the same store-versus-acceptance
// distinction the frontier path turns on.
func TestAccepted_ResponderAnswersOnlyForBlocksItAccepted(t *testing.T) {
	const N, M = 20, 10
	chain, _ := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M) // accepted 0..M
	vm.store(chain[M+1])      // present in the store, NOT accepted
	bh, _, _ := newBSHandlerAndEngine(t, vm, 1)
	net := &captureNet{}
	bh.net, bh.msgCreator = net, bsMsgBuilder{}

	peer := ids.GenerateTestNodeID()
	asked := []ids.ID{chain[M].id, chain[M-3].id, chain[M+1].id, chain[N].id}
	require.NoError(t, bh.GetAccepted(context.Background(), peer, 9, timeSoon(), asked))

	require.Len(t, net.sent, 1, "the question is answered")
	require.Equal(t, "accepted", net.sent[0].op)
	require.Equal(t, uint32(9), net.sent[0].requestID, "answered under the id it was asked with")
	require.Equal(t, []ids.ID{chain[M].id, chain[M-3].id}, net.sent[0].containerIDs,
		"only blocks on the accepted chain: the stored-but-unaccepted M+1 and the unheld N are not vouched for")
	require.True(t, net.to[0].Contains(peer), "answered to the asker alone")
}

// TestAccepted_ResponderRefusesAnOversizedQuestion: the id list is chosen by the sender, so it is
// bounded — one message must not become an unbounded number of block lookups. The refusal is whole:
// nothing is walked and nothing is answered.
func TestAccepted_ResponderRefusesAnOversizedQuestion(t *testing.T) {
	chain, _ := buildBSChain(4, -1)
	vm := newBSVMAt(chain, 4)
	bh, _, _ := newBSHandlerAndEngine(t, vm, 1)
	net := &captureNet{}
	bh.net, bh.msgCreator = net, bsMsgBuilder{}

	oversized := make([]ids.ID, bootstrapAcceptedCandidates+1)
	for i := range oversized {
		oversized[i] = chain[0].id
	}
	require.NoError(t, bh.GetAccepted(context.Background(), ids.GenerateTestNodeID(), 1, timeSoon(), oversized))
	require.Empty(t, net.sent, "a question naming more ids than a naming round can ask is refused whole")

	atLimit := oversized[:bootstrapAcceptedCandidates]
	require.NoError(t, bh.GetAccepted(context.Background(), ids.GenerateTestNodeID(), 2, timeSoon(), atLimit))
	require.Len(t, net.sent, 1, "the bound itself is answerable")
}

// TestAccepted_WireCarriesTheIDList proves the router's decode agrees with the wire form the
// builder writes: a concatenated run of 32-byte ids, bounded on the way in.
func TestAccepted_WireCarriesTheIDList(t *testing.T) {
	want := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID()}
	var wire []byte
	for _, id := range want {
		wire = append(wire, id[:]...)
	}
	require.Equal(t, want, decodeIDs(wire, 8))
	require.Equal(t, want[:2], decodeIDs(wire, 2), "the decode stops at the bound")
	require.Empty(t, decodeIDs(wire[:20], 8), "a partial id names no block")
	require.Len(t, decodeIDs(append(wire, 7), 8), 3, "a trailing fragment is ignored")
}

// TestAccepted_RouterDispatchesBothHalves: the two ops the router hands a chain are dispatched —
// the question is answered, and an answer reaches the round that asked. Both cases were missing
// from the switch, so these messages arrived and fell through it: the round trip was absent on
// both sides at once, which is why neither half could be noticed missing.
func TestAccepted_RouterDispatchesBothHalves(t *testing.T) {
	const N, M = 10, 6
	chain, _ := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M)
	bh, _, _ := newBSHandlerAndEngine(t, vm, 1)
	net := &captureNet{}
	bh.net, bh.msgCreator = net, bsMsgBuilder{}
	peer := ids.GenerateTestNodeID()

	// The question, in the wire form the router extracts: a concatenated run of 32-byte ids.
	var wire []byte
	for _, id := range []ids.ID{chain[M].id, chain[N].id} {
		wire = append(wire, id[:]...)
	}
	require.NoError(t, bh.HandleInbound(context.Background(), handler.Message{
		NodeID: peer, RequestID: 5, Op: handler.GetAccepted, Message: wire,
	}))
	require.Len(t, net.sent, 1, "a node answers the question whether or not it is itself syncing")
	require.Equal(t, []ids.ID{chain[M].id}, net.sent[0].containerIDs)

	// The answer, likewise.
	bh.bsActive.Store(true)
	defer bh.bsActive.Store(false)
	ch := make(chan bsAcceptedReply, 1)
	bh.bsAccepted = map[uint32]bsAcceptedRound{7: {asked: set.Of(peer), ch: ch}}
	require.NoError(t, bh.HandleInbound(context.Background(), handler.Message{
		NodeID: peer, RequestID: 7, Op: handler.Accepted, Message: wire,
	}))
	got := <-ch
	require.Equal(t, peer, got.nodeID)
	require.Equal(t, []ids.ID{chain[M].id, chain[N].id}, got.accepted)
}

// ----- the asker ------------------------------------------------------------

// TestAccepted_NotBehindAsksNothing is the steady state. A node at the top of every reported tip
// has accepted all of them, so there is nothing left to ask about and no message is sent. The
// second question exists for a node that is behind and is invisible to one that is not.
func TestAccepted_NotBehindAsksNothing(t *testing.T) {
	const N = 12
	chain, _ := buildBSChain(N, -1)
	vm := newBSVMAt(chain, N) // accepted everything
	bh, _, beacons := newBSHandlerAndEngine(t, vm, 3)
	net := &captureNet{}
	bh.net, bh.msgCreator = net, bsMsgBuilder{}

	answers := bh.Acceptance(context.Background(), []ids.ID{chain[N].id, chain[N-1].id}, beacons)
	require.Empty(t, answers)
	require.Empty(t, net.sent, "a node that is not behind sends no second question")
}

// TestAccepted_AsksOnlyForBlocksItLacks: candidates already on this node's chain are dropped before
// asking. Every block the second question can name is therefore one this node lacks, so a named
// block always makes the loop descend and fetch — it can never be read as "already synced" at a
// height the node is stuck on, which is the property the own-height exclusion protects for the
// ancestry walk.
func TestAccepted_AsksOnlyForBlocksItLacks(t *testing.T) {
	const N, M = 20, 10
	chain, _ := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M)
	bh, _, beacons := newBSHandlerAndEngine(t, vm, 3)
	net := &captureNet{}
	bh.net, bh.msgCreator = net, bsMsgBuilder{}
	bh.bsActive.Store(true)
	defer bh.bsActive.Store(false)

	go bh.Acceptance(context.Background(), []ids.ID{chain[M].id, chain[M-1].id, chain[N].id}, beacons)
	require.Eventually(t, func() bool { return len(net.sent) == 1 }, time.Second, time.Millisecond)
	require.Equal(t, "getaccepted", net.sent[0].op)
	require.Equal(t, []ids.ID{chain[N].id}, net.sent[0].containerIDs,
		"the two blocks already accepted are not asked about")
}

// TestAccepted_DropsAnswersNobodyAsked: an answer counts only if this lane asked its sender, under
// the id it asked with. A peer that was not in the round, or an answer to a round already over,
// contributes nothing — otherwise any connected peer could join a tally by talking.
func TestAccepted_DropsAnswersNobodyAsked(t *testing.T) {
	const N, M = 8, 3
	chain, _ := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M)
	bh, _, beacons := newBSHandlerAndEngine(t, vm, 3)
	bh.msgCreator = bsMsgBuilder{}
	bh.bsActive.Store(true)
	defer bh.bsActive.Store(false)

	stranger := ids.GenerateTestNodeID()
	relay := &answerNet{bh: bh, asked: beacons[0], stranger: stranger, tip: chain[N].id}
	bh.net = relay

	answers := bh.Acceptance(context.Background(), []ids.ID{chain[N].id}, beacons[:1])
	require.Len(t, answers, 1, "exactly the asked beacon's answer is taken")
	require.Equal(t, beacons[0], answers[0].NodeID)

	// A reply for a request this lane is not running belongs to nobody.
	require.False(t, bh.deliverBootstrapAccepted(relay.requestID, beacons[0], []ids.ID{chain[N].id}),
		"the round is over — its request id no longer accepts answers")
	require.False(t, bh.deliverBootstrapAccepted(relay.requestID+404, beacons[0], nil),
		"an id this lane never issued is not a lane it can deliver to")
}

// answerNet answers one asked beacon and also relays an unasked stranger's answer plus a duplicate,
// so the collector's drop rules are exercised on the real path.
type answerNet struct {
	network.Network
	bh        *blockHandler
	asked     ids.NodeID
	stranger  ids.NodeID
	tip       ids.ID
	requestID uint32
}

func (n *answerNet) Send(msg message.OutboundMessage, _ set.Set[ids.NodeID], _ ids.ID, _ uint32) set.Set[ids.NodeID] {
	m, ok := msg.(*bsOutMsg)
	if !ok || m.op != "getaccepted" {
		return nil
	}
	n.requestID = m.requestID
	n.bh.deliverBootstrapAccepted(m.requestID, n.stranger, []ids.ID{n.tip})
	n.bh.deliverBootstrapAccepted(m.requestID, n.asked, []ids.ID{n.tip})
	n.bh.deliverBootstrapAccepted(m.requestID, n.asked, []ids.ID{n.tip})
	return nil
}

// ----- end to end -----------------------------------------------------------

// TestAccepted_BehindNodeReachesTheTipWithNoCert is the criterion, over the real message path: a
// node behind the fleet reaches the network tip without a cert existing for that height.
//
// The fleet is spread over four heads, as a live chain always is, and the two highest are held by
// too few nodes for their ancestry to be fetchable — the bleeding edge, where a walk that must
// compare heads by fetching them gets empties. So the tips agree on nothing and the ancestry walk
// finds nothing: without the second question this node names no frontier, retries, and fails safe
// at its stale height forever. Asking which of those tips the beacons have ACCEPTED names the
// highest one a stake quorum holds, and the descent — which needs no cert, only blocks — carries
// the node there; the fleet then settles on its top head and the node follows it up.
func TestAccepted_BehindNodeReachesTheTipWithNoCert(t *testing.T) {
	const N, M = 40, 20 // fleet heads at 37..40; this node stale at 20
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M)

	const w uint64 = 100
	beacons := nodeIDs(4)
	weights := map[ids.NodeID]uint64{}
	heads := map[ids.NodeID]ids.ID{}
	settled := map[ids.NodeID]ids.ID{}
	for i, id := range beacons {
		weights[id] = w
		heads[id] = chain[N-3+i].id // 37, 38, 39, 40 — four heads, no two the same
		settled[id] = chain[N].id   // finality propagates: every beacon ratchets up to 40
	}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.bootstrapRetryInterval = 2 * time.Millisecond
	bh.bootstrapConnectDeadline = time.Second
	bh.msgCreator = bsMsgBuilder{}
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[N],
		serveAncestors: true,
		tipFor:         heads,
		// The top two heads are the bleeding edge: while the fleet is spread, too few nodes hold
		// them for a sample to fetch their ancestry.
		withhold:          set.Of(chain[N].id, chain[N-1].id),
		tipFor2:           settled,
		propagateAtHeight: N - 2,
	}
	ctx := context.Background()

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status,
		"a fleet spread over four heads must still name a frontier — that is the state a live chain is always in")
	require.Equal(t, chain[N-2].id, tip, "38 is the highest head a >⅔ responder-stake quorum has accepted")

	require.NoError(t, runBS(t, bh), "the behind node must converge")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last, "reached the network tip from a stale %d", M)
	require.True(t, bh.Accepted(ctx, chain[N].id), "and it is on the accepted chain")

	// The criterion's second half: no cert exists for any of it. Bootstrap accepts on frontier
	// trust — it fetches ancestry and accepts blocks directly — so a range whose certs were never
	// persisted is not a range that cannot be recovered.
	for h := M + 1; h <= N; h++ {
		require.False(t, bh.engine.IsAccepted(chain[h].id),
			"no quorum certificate finalized height %d — the node reached the tip without one", h)
	}
}

// TestAccepted_WithoutTheSecondQuestionTheSameFleetNamesNothing is the control for the test above,
// run against the same fleet with the acceptance step unavailable: the tips split, the bleeding
// edge is unfetchable, and nothing can be named. It is what every node on a live chain saw.
func TestAccepted_WithoutTheSecondQuestionTheSameFleetNamesNothing(t *testing.T) {
	const N, M = 40, 20
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M)

	const w uint64 = 100
	beacons := nodeIDs(4)
	weights := map[ids.NodeID]uint64{}
	heads := map[ids.NodeID]ids.ID{}
	for i, id := range beacons {
		weights[id] = w
		heads[id] = chain[N-3+i].id
	}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.msgCreator = bsMsgBuilder{}
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[N],
		serveAncestors: true, tipFor: heads,
		withhold: set.Of(chain[N].id, chain[N-1].id),
	}

	weightsCopy, _, ok := bh.beaconWeights()
	require.True(t, ok)
	policy := bh.bootstrapPolicy(weightsCopy)
	policy.Acceptance = nil // the only difference from the test above

	bh.bsActive.Store(true)
	replies := bh.collectFrontierReplies(context.Background(), beacons, weightsCopy)
	bh.bsActive.Store(false)
	require.Len(t, replies, 4)

	_, err := policy.AcceptsFrontier(context.Background(), replies)
	require.Error(t, err, "four heads, an unfetchable bleeding edge, and no second question: nothing is nameable")
}

func timeSoon() time.Time { return time.Now().Add(10 * time.Second) }
