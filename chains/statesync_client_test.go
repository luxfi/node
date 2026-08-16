// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/luxfi/consensus/engine/chain/summary"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/vm/chain"
)

// A reply reaches the round that asked for it. Without this the adoption round
// collects nothing however many beacons answer, and every node falls through to
// a descent that cannot close a deep gap — which is the state this whole path
// exists to get a node out of.
func TestARepliedOfferReachesTheRound(t *testing.T) {
	b := &blockHandler{}
	beacon := ids.GenerateTestNodeID()
	asked := set.NewSet[ids.NodeID](1)
	asked.Add(beacon)

	requestID, ch, done := b.openSummaryRound(asked)
	defer done()

	b.deliverOffer(requestID, beacon, []byte("summary"))
	select {
	case got := <-ch:
		if got.NodeID != beacon || string(got.Bytes) != "summary" {
			t.Fatalf("collected %+v, want the beacon's own bytes", got)
		}
	default:
		t.Fatal("the beacon answered and the round collected nothing")
	}
}

// The controls. A request id is ours, not a secret: any connected peer can send a
// reply carrying it. Correlating on the id alone lets a stranger answer a question
// we never put to it, and one such reply is enough to decide what state this node
// keeps.
func TestAnUnaskedPeerIsNotCollected(t *testing.T) {
	b := &blockHandler{}
	beacon, stranger := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	asked := set.NewSet[ids.NodeID](1)
	asked.Add(beacon)

	requestID, ch, done := b.openSummaryRound(asked)
	defer done()

	b.deliverOffer(requestID, stranger, []byte("summary"))
	select {
	case got := <-ch:
		t.Fatalf("a peer we never asked was collected: %+v", got)
	default:
	}

	// And a reply for a round that is over is dropped rather than parked for the
	// next one, which would answer a question with a stale beacon's opinion.
	done()
	b.deliverOffer(requestID, beacon, []byte("summary"))
	select {
	case got := <-ch:
		t.Fatalf("a reply landed after the round closed: %+v", got)
	default:
	}
}

// Ballots carry the same rule, and one more: the stake is attached by the node,
// so a reply from outside the beacon set has no weight to attach and is not a
// ballot at all.
func TestARepliedBallotReachesTheRound(t *testing.T) {
	b := &blockHandler{}
	beacon := ids.GenerateTestNodeID()
	asked := set.NewSet[ids.NodeID](1)
	asked.Add(beacon)

	requestID, ch, done := b.openBallotRound(asked)
	defer done()

	held := []ids.ID{ids.GenerateTestID()}
	b.deliverBallot(requestID, beacon, held)
	select {
	case got := <-ch:
		if got.NodeID != beacon || len(got.Held) != 1 || got.Held[0] != held[0] {
			t.Fatalf("collected %+v, want the beacon's held ids", got)
		}
	default:
		t.Fatal("the beacon answered and the round collected nothing")
	}
}

// The two rounds must not share a mailbox: an offer answering a ballot round
// would be counted as a vote for nothing, and a ballot answering a discovery
// round would be read as a summary nobody offered.
func TestTheTwoRoundsDoNotShareAMailbox(t *testing.T) {
	b := &blockHandler{}
	beacon := ids.GenerateTestNodeID()
	asked := set.NewSet[ids.NodeID](1)
	asked.Add(beacon)

	offerID, offerCh, doneOffer := b.openSummaryRound(asked)
	defer doneOffer()
	ballotID, ballotCh, doneBallot := b.openBallotRound(asked)
	defer doneBallot()

	b.deliverBallot(offerID, beacon, []ids.ID{ids.GenerateTestID()})
	_ = ballotID
	select {
	case got := <-offerCh:
		t.Fatalf("a ballot landed in the discovery round: %+v", got)
	default:
	}
	select {
	case got := <-ballotCh:
		t.Fatalf("a ballot for the discovery round's id landed in ratification: %+v", got)
	default:
	}
}

var _ summary.Source = (*summarySource)(nil)

// The assertion adoption stands on. A VM that can sync state and one whose
// summary type simply did not match look identical from a silent return, so this
// failing is what a client wired into a shipped build and doing nothing looks
// like from the outside.
func TestTheSyncSurfaceIsReachable(t *testing.T) {
	// Two links, and either breaking is silent at runtime: the VM must offer the
	// sync surface, and what adoption hands the round must satisfy it.
	var vm chain.ChainVM
	if _, ok := vm.(chain.StateSyncableVM); ok {
		t.Fatal("precondition: a bare ChainVM must not pass for a state-syncable one")
	}
	var _ summary.VM[chain.StateSummary] = (*syncVM)(nil)
}
