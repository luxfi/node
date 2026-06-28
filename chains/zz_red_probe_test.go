// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// zz_red_probe_test.go — RED adversarial security-regression probe for the fresh-net self-vote
// caught-up path. Demonstrates the connected-vs-replied divergence: the fix gates the self-vote
// on fullyConnectedBeacons (CONNECTIVITY, from net.PeerInfo) but tallies CaughtUp over `replies`
// (from collectFrontierReplies). A beacon that is CONNECTED but does NOT answer the frontier
// query this round counts as "fully connected" yet contributes nothing to the caught-up tally —
// and the self-vote backfills its missing weight, so a HEAVY validator self-completes caught-up
// at a STALE height while an honest connected beacon is genuinely ahead. Blue's bsBeaconNet
// cannot express this (its Send answers for EVERY connected beacon), so the regression slipped
// through. These assertions encode the DESIRED safe behavior: they FAIL on the current code (the
// break) and will PASS once the self-vote gate also requires every connected beacon to have
// REPLIED this round.
package chains

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/network/peer"
)

// redSilentNet reports FULL connectivity (every beacon in `connected` is returned by PeerInfo)
// but a designated `silent` beacon — though connected — withholds its GetAcceptedFrontier reply
// this round. This is the exact capability an on-path adversary has (keep a beacon's
// TCP/handshake alive so it shows connected, while dropping/delaying its application-level
// frontier response past the 3s window) AND a natural occurrence during a mass co-restart (an
// ahead beacon replaying state answers the frontier query slowly).
type redSilentNet struct {
	network.Network
	bh        *blockHandler
	connected []ids.NodeID          // all reported connected (the full set MINUS self)
	silent    set.Set[ids.NodeID]   // connected but withhold their frontier reply
	tipFor    map[ids.NodeID]ids.ID // what each VOCAL beacon reports
}

func (n *redSilentNet) PeerInfo(nodeIDs []ids.NodeID) []peer.Info {
	want := map[ids.NodeID]bool{}
	for _, id := range nodeIDs {
		want[id] = true
	}
	var out []peer.Info
	for _, b := range n.connected {
		if len(nodeIDs) == 0 || want[b] {
			out = append(out, peer.Info{ID: b, TrackedChains: set.Of(n.bh.networkID)})
		}
	}
	return out
}

func (n *redSilentNet) Send(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ uint32) set.Set[ids.NodeID] {
	m, ok := msg.(*bsOutMsg)
	if !ok || m.op != "frontier" {
		return nil
	}
	for id := range nodeIDs {
		if n.silent.Contains(id) {
			continue // CONNECTED, but withholds its frontier reply this round
		}
		if tip, ok := n.tipFor[id]; ok {
			n.bh.deliverBootstrapFrontier(id, tip)
		}
	}
	return nil
}

// TestRED_PROBE_ConnectedButSilentAheadBeacon_SelfVoteFalseCompletesAtStale is a TWO-SIDED
// CONTRAST: the ONLY difference between the two runs is whether the CONNECTED ahead-beacon B
// delivers its frontier reply. B is fully connected in both runs, so fullyConnectedBeacons is
// TRUE in both. Yet B-replies → safe (status != FrontierCaughtUp); B-connected-but-silent →
// FrontierCaughtUp at the stale height (the break). This falsifies Blue's safety claim ("a
// genuinely behind node has an ahead peer in the full set → CaughtUp is false → it keeps
// waiting/syncing"): the gate keys on CONNECTIVITY while the caught-up decision keys on REPLIES,
// and the two diverge.
//
// Why K is genuinely finalized-ahead (a real safety break, not a minority fork): a heavy
// validator (self=60%) finalizes K WITH a minority (B=33%, so self+B=93% ≥ ⅔), then LOSES K to a
// persistence lag (this codebase documents exactly this — the ZAP fire-and-forget Accept /
// bsTestVM.frozenLastAccepted) and restarts STALE at M < K. B retained the finalized K. The node
// MUST recover K; self-completing at M abandons finalized history and lets it build a conflicting
// fork. The node cannot tell "M is the frontier" from "I lost finalized K" — which is precisely
// why it must HEAR from connected B before concluding caught-up. self is NOT an independent
// witness to its own caught-up-ness; the self-vote lets the node vouch for its own staleness.
func TestRED_PROBE_ConnectedButSilentAheadBeacon_SelfVoteFalseCompletesAtStale(t *testing.T) {
	const N = 10 // K: the finalized-ahead height B retained (self voted it, then lost it)
	const M = 5  // the node's STALE accepted height after the persistence-lag crash

	run := func(t *testing.T, bSilent bool) chainbootstrap.FrontierStatus {
		chain, _ := buildBSChain(N, -1)
		vm := newBSVMAt(chain, M) // node stale at M; it does NOT hold chain[N]=K

		self := ids.GenerateTestNodeID()
		a := ids.GenerateTestNodeID() // co-stale light beacon, at M
		b := ids.GenerateTestNodeID() // AHEAD beacon, retained finalized K
		// HEAVY self (60 of 100): self > total/2 - 1, so peers alone (40) < the 51 stake-majority
		// floor → the self-vote branch. self+B=93% could finalize K; B=33% retains it.
		weights := map[ids.NodeID]uint64{self: 60, a: 7, b: 33}

		bh, _ := newBSHandlerWeighted(t, vm, weights)
		bh.selfNodeID = self
		bh.msgCreator = bsMsgBuilder{}

		// FULL connectivity in BOTH runs: A and B are connected (B is in `connected` either way).
		tipFor := map[ids.NodeID]ids.ID{a: chain[M].id} // A reports the stale tip M
		silent := set.NewSet[ids.NodeID](1)
		if bSilent {
			silent.Add(b) // B connected but withholds its frontier reply this round
		} else {
			tipFor[b] = chain[N].id // B replies its genuine ahead tip K
		}
		bh.net = &redSilentNet{bh: bh, connected: []ids.NodeID{a, b}, silent: silent, tipFor: tipFor}

		bh.bsActive.Store(true)
		_, status := bh.FrontierTip(context.Background())
		bh.bsActive.Store(false)
		return status
	}

	bReplies := run(t, false)
	bSilent := run(t, true)
	t.Logf("B replies its ahead tip → status=%v (3=FrontierConnecting, safe)", bReplies)
	t.Logf("B connected but SILENT  → status=%v (5=FrontierCaughtUp, the BREAK)", bSilent)

	// Sanity: when the ahead beacon REPLIES, the node correctly fails safe (does not conclude caught-up).
	require.NotEqual(t, chainbootstrap.FrontierCaughtUp, bReplies,
		"sanity: when the ahead beacon REPLIES, the node correctly does NOT conclude caught-up")

	// THE SECURITY REGRESSION ASSERTION. B is fully CONNECTED in both runs. The node must NOT
	// self-complete caught-up while a connected beacon's position is unknown — that is a stale
	// go-live. FAILS today (the break); PASSES once the self-vote gate also requires every connected
	// beacon to have REPLIED this round (not merely be connected).
	require.NotEqual(t, chainbootstrap.FrontierCaughtUp, bSilent,
		"BREAK: suppressing only the CONNECTED ahead-beacon's frontier reply flips the heavy node to "+
			"FrontierCaughtUp at the STALE height — the self-vote backfills the floor and the "+
			"full-connectivity gate cannot see the reply suppression")
}

// TestRED_PROBE_EqualStakeNeedsNoSelfVote answers deploy-question #5: 5 EQUAL-stake beacons, node
// a beacon, all four peers connected and reporting a common tip — the node concludes caught-up via
// the ORDINARY AcceptsFrontier path (peers clear the stake-majority floor: 4·w of 5·w = 80% >
// 50%). The self-vote is NEVER needed for equal stake, so the equal-stake devnet hang is NOT this
// self-exclusion floor (look at primaryNetworkReady / P-chain bootstrap / beacon connectivity).
func TestRED_PROBE_EqualStakeNeedsNoSelfVote(t *testing.T) {
	chain, byID := buildBSChain(8, -1)
	vm := newBSVM(chain) // node at genesis (height 0)

	self := ids.GenerateTestNodeID()
	p1, p2, p3, p4 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	weights := map[ids.NodeID]uint64{self: 100, p1: 100, p2: 100, p3: 100, p4: 100}

	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.selfNodeID = self
	bh.msgCreator = bsMsgBuilder{}
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3, p4}, byID: byID, tip: chain[0]}

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(context.Background())
	bh.bsActive.Store(false)

	t.Logf("equal-stake fresh net: status=%v tip=%v", status, tip)
	require.Contains(t, []chainbootstrap.FrontierStatus{chainbootstrap.FrontierNamed, chainbootstrap.FrontierCaughtUp}, status,
		"equal-stake peers clear the stake-majority floor unaided — no self-vote needed")
	require.Equal(t, chain[0].id, tip, "caught up at genesis")
}

// TestRED_PROBE_SilentAheadBeacon_ExactlyFrontierConnecting pins the POSITIVE post-fix outcome of
// the silent-beacon break: it is not merely "not CaughtUp" — it must be EXACTLY FrontierConnecting,
// the fail-safe WAIT. A heavy self (60 of 100) crashed STALE at M while a connected-but-silent
// beacon B (33) retained finalized K at N; the peer-only responder weight (A's 7) is far below the
// stake-majority naming floor (51), so the decision rides the self-vote branch — which repliedCovers
// now blocks because B did not answer. The node WAITS (the loop re-samples / fails safe at the
// connect deadline), never going live at the stale height.
func TestRED_PROBE_SilentAheadBeacon_ExactlyFrontierConnecting(t *testing.T) {
	const N = 10 // B's retained finalized height
	const M = 5  // self's stale height after the persistence-lag crash
	chain, _ := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M)

	self := ids.GenerateTestNodeID()
	a := ids.GenerateTestNodeID() // co-stale light beacon, vocal at M
	b := ids.GenerateTestNodeID() // AHEAD beacon, CONNECTED but SILENT this round
	weights := map[ids.NodeID]uint64{self: 60, a: 7, b: 33}

	bh, _ := newBSHandlerWeighted(t, vm, weights)
	bh.selfNodeID = self
	bh.msgCreator = bsMsgBuilder{}

	silent := set.NewSet[ids.NodeID](1)
	silent.Add(b)
	bh.net = &redSilentNet{
		bh: bh, connected: []ids.NodeID{a, b}, silent: silent,
		tipFor: map[ids.NodeID]ids.ID{a: chain[M].id},
	}

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(context.Background())
	bh.bsActive.Store(false)

	require.Equal(t, chainbootstrap.FrontierConnecting, status,
		"a CONNECTED but silent ahead-beacon must leave the node WAITING (FrontierConnecting), never caught-up at the stale height")
	require.Equal(t, ids.Empty, tip, "no tip is named while connecting")
}

// TestRED_PROBE_SilentCoStaleBeacon_BlocksCaughtUp is the eclipse/partition INVERSION that proves
// the guard keys on REPLY-COVERAGE, not on whether the silent peer is ahead. A two-sided contrast
// on a genuine FRESH net (every node at genesis): the ONLY variable is whether the connected
// co-stale beacon B answers.
//
//   - B vocal at genesis → full set + self all hold genesis, nobody ahead → FrontierCaughtUp (the
//     legitimate fresh-net completion).
//   - B connected but SILENT → even though B is NOT ahead (it is at genesis like everyone), the node
//     cannot KNOW that without hearing from it, so repliedCovers blocks the self-vote and the node
//     fails safe to FrontierConnecting.
//
// This is the precise eclipse defense: an adversary need not hide an ahead-beacon's TCP — merely
// suppressing its application-level frontier reply must NOT let a heavy node self-complete.
func TestRED_PROBE_SilentCoStaleBeacon_BlocksCaughtUp(t *testing.T) {
	const N = 5
	chain, _ := buildBSChain(N, -1)

	self := ids.GenerateTestNodeID()
	a := ids.GenerateTestNodeID() // vocal co-stale beacon (genesis)
	b := ids.GenerateTestNodeID() // co-stale beacon (genesis), connected — silence is the variable
	// HEAVY self (60 of 100): peers alone (40) < the 51 stake-majority floor → the self-vote branch,
	// so the outcome is decided entirely by repliedCovers over the connected set {a, b}.
	weights := map[ids.NodeID]uint64{self: 60, a: 30, b: 10}

	run := func(t *testing.T, bSilent bool) chainbootstrap.FrontierStatus {
		vm := newBSVM(chain) // node at genesis
		bh, _ := newBSHandlerWeighted(t, vm, weights)
		bh.selfNodeID = self
		bh.msgCreator = bsMsgBuilder{}

		silent := set.NewSet[ids.NodeID](1)
		tipFor := map[ids.NodeID]ids.ID{a: chain[0].id} // A always reports genesis
		if bSilent {
			silent.Add(b)
		} else {
			tipFor[b] = chain[0].id // B also reports genesis (co-stale, NOT ahead)
		}
		bh.net = &redSilentNet{bh: bh, connected: []ids.NodeID{a, b}, silent: silent, tipFor: tipFor}

		bh.bsActive.Store(true)
		_, status := bh.FrontierTip(context.Background())
		bh.bsActive.Store(false)
		return status
	}

	bVocal := run(t, false)
	bSilent := run(t, true)
	t.Logf("B vocal co-stale → %v ; B silent co-stale → %v", bVocal, bSilent)

	require.Equal(t, chainbootstrap.FrontierCaughtUp, bVocal,
		"sanity: when every connected co-stale beacon REPLIES genesis, the fresh net completes (CaughtUp)")
	require.Equal(t, chainbootstrap.FrontierConnecting, bSilent,
		"a SILENT connected beacon — even one that is NOT ahead — blocks caught-up: the guard requires REPLY coverage, not ahead-ness")
}
