// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// statesync_client.go — the wire under state-summary adoption.
//
// The adoption round itself lives in consensus (engine/chain/summary): it decides
// which summary the network stands behind and hands it to the VM. It carries no
// transport at all — no request ids, no pending sets, no timeouts — the same
// division bootstrap draws at BlockSource. This file is the other side of that
// line: it asks the beacons and returns what arrived before the window closed.
//
// Two rounds, two questions. Discovery asks what each beacon holds and carries no
// threshold, because a wrong height there costs one extra entry in the next
// request. Ratification asks the WHOLE beacon set which of those heights it holds
// and attaches each beacon's stake, because that is where the decision is made and
// a sample would let whoever is over-represented in it choose the node's state.
package chains

import (
	"context"
	"errors"
	"time"

	"github.com/luxfi/consensus/engine/chain/summary"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/vm/chain"
)

// errNoBeaconsToAsk separates "the beacons were asked and had nothing" from "the
// beacons could not be asked". Only the first is an answer; reading the second as
// one tells a node the network has nothing for it when nobody was ever reached.
var errNoBeaconsToAsk = errors.New("state summary: no beacon set to ask")

const (
	// summaryOfferWindow bounds discovery. A beacon that has not answered a
	// question this cheap inside it is not going to.
	summaryOfferWindow = 10 * time.Second
	// summaryBallotWindow bounds ratification. Longer than discovery: this round
	// asks every beacon rather than a sample, and its answer decides whether the
	// node keeps its state.
	summaryBallotWindow = 30 * time.Second
)

// summarySource is summary.Source over this chain's network.
type summarySource struct{ b *blockHandler }

var _ summary.Source = (*summarySource)(nil)

// Offers asks the beacons which summary each of them holds.
//
// An error here means the beacons could not be ASKED — the one condition the
// round must not read as "the network has nothing for me". An empty result with
// no error is an answer: they were asked and had nothing.
func (s *summarySource) Offers(ctx context.Context) ([]summary.Offer, error) {
	b := s.b
	if b.net == nil || b.msgCreator == nil {
		return nil, nil
	}
	weights, _, ok := b.beaconWeights()
	if !ok || len(weights) == 0 {
		return nil, errNoBeaconsToAsk
	}
	asked := set.NewSet[ids.NodeID](len(weights))
	for id := range weights {
		asked.Add(id)
	}

	requestID, ch, done := b.openSummaryRound(asked)
	defer done()

	msg, err := b.msgCreator.GetStateSummaryFrontier(b.chainID, requestID, summaryOfferWindow)
	if err != nil {
		return nil, err
	}
	b.net.Send(msg, asked, b.networkID, 0)

	// One reply per beacon, and stop as soon as every beacon has answered rather
	// than sitting out the window for replies that cannot arrive.
	offers := make([]summary.Offer, 0, asked.Len())
	seen := set.NewSet[ids.NodeID](asked.Len())
	deadline := time.After(summaryOfferWindow)
	for seen.Len() < asked.Len() {
		select {
		case o := <-ch:
			if seen.Contains(o.NodeID) {
				continue
			}
			seen.Add(o.NodeID)
			if len(o.Bytes) > 0 {
				offers = append(offers, o)
			}
		case <-deadline:
			return offers, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return offers, nil
}

// Ballots asks every beacon which of these heights it holds, and returns the whole
// set's stake as the denominator. The denominator is the whole set and not the
// responders: shrinking it to whoever answered is what lets an eclipse make a
// sliver of stake look like a supermajority.
func (s *summarySource) Ballots(ctx context.Context, heights []uint64) ([]summary.Ballot, uint64, error) {
	b := s.b
	if b.net == nil || b.msgCreator == nil {
		return nil, 0, nil
	}
	weights, total, ok := b.beaconWeights()
	if !ok || len(weights) == 0 {
		return nil, 0, errNoBeaconsToAsk
	}
	asked := set.NewSet[ids.NodeID](len(weights))
	for id := range weights {
		asked.Add(id)
	}

	requestID, ch, done := b.openBallotRound(asked)
	defer done()

	msg, err := b.msgCreator.GetAcceptedStateSummary(b.chainID, requestID, summaryBallotWindow, heights)
	if err != nil {
		return nil, 0, err
	}
	b.net.Send(msg, asked, b.networkID, 0)

	ballots := make([]summary.Ballot, 0, asked.Len())
	seen := set.NewSet[ids.NodeID](asked.Len())
	deadline := time.After(summaryBallotWindow)
	for seen.Len() < asked.Len() {
		select {
		case v := <-ch:
			if seen.Contains(v.NodeID) {
				continue
			}
			seen.Add(v.NodeID)
			// The stake is attached here because the consensus module can weigh a
			// vote but cannot look one up. A reply from a node the beacon set does
			// not name weighs nothing, so it is not a ballot.
			w, isBeacon := weights[v.NodeID]
			if !isBeacon {
				continue
			}
			v.Weight = w
			ballots = append(ballots, v)
		case <-deadline:
			return ballots, total, nil
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	return ballots, total, nil
}

// openSummaryRound registers a collector for one discovery round and returns the
// request id, the channel replies land on, and the teardown.
func (b *blockHandler) openSummaryRound(asked set.Set[ids.NodeID]) (uint32, chan summary.Offer, func()) {
	b.contextRequestMu.Lock()
	b.requestIDCounter++
	requestID := b.requestIDCounter
	b.contextRequestMu.Unlock()

	ch := make(chan summary.Offer, asked.Len())
	b.bsMu.Lock()
	if b.summaryOfferCh == nil {
		b.summaryOfferCh = map[uint32]chan summary.Offer{}
		b.summaryPeers = map[uint32]set.Set[ids.NodeID]{}
	}
	b.summaryOfferCh[requestID] = ch
	// Remember who was asked. A request id is ours, not a secret: any connected
	// peer can send a reply carrying it, and a stranger's answer to a question we
	// never put to it is not evidence.
	b.summaryPeers[requestID] = asked
	b.bsMu.Unlock()

	return requestID, ch, func() {
		b.bsMu.Lock()
		delete(b.summaryOfferCh, requestID)
		delete(b.summaryPeers, requestID)
		b.bsMu.Unlock()
	}
}

// openBallotRound is openSummaryRound for the ratification round.
func (b *blockHandler) openBallotRound(asked set.Set[ids.NodeID]) (uint32, chan summary.Ballot, func()) {
	b.contextRequestMu.Lock()
	b.requestIDCounter++
	requestID := b.requestIDCounter
	b.contextRequestMu.Unlock()

	ch := make(chan summary.Ballot, asked.Len())
	b.bsMu.Lock()
	if b.summaryBallotCh == nil {
		b.summaryBallotCh = map[uint32]chan summary.Ballot{}
		b.summaryPeers = map[uint32]set.Set[ids.NodeID]{}
	}
	b.summaryBallotCh[requestID] = ch
	b.summaryPeers[requestID] = asked
	b.bsMu.Unlock()

	return requestID, ch, func() {
		b.bsMu.Lock()
		delete(b.summaryBallotCh, requestID)
		delete(b.summaryPeers, requestID)
		b.bsMu.Unlock()
	}
}

// deliverOffer routes a discovery reply to the round that asked for it. A reply
// carrying a request id we are not collecting, or from a peer we did not ask, is
// dropped.
func (b *blockHandler) deliverOffer(requestID uint32, nodeID ids.NodeID, bytes []byte) {
	b.bsMu.Lock()
	ch, collecting := b.summaryOfferCh[requestID]
	peers, known := b.summaryPeers[requestID]
	b.bsMu.Unlock()
	if !collecting || !known || !peers.Contains(nodeID) {
		return
	}
	select {
	case ch <- summary.Offer{NodeID: nodeID, Bytes: bytes}:
	default: // the round already has this beacon's answer or has moved on
	}
}

// deliverBallot is deliverOffer for the ratification round.
func (b *blockHandler) deliverBallot(requestID uint32, nodeID ids.NodeID, held []ids.ID) {
	b.bsMu.Lock()
	ch, collecting := b.summaryBallotCh[requestID]
	peers, known := b.summaryPeers[requestID]
	b.bsMu.Unlock()
	if !collecting || !known || !peers.Contains(nodeID) {
		return
	}
	select {
	case ch <- summary.Ballot{NodeID: nodeID, Held: held}:
	default:
	}
}

// adoptSummary runs the adoption round before bootstrap.
//
// It runs at all only for a VM that can sync state and says it wants to; every
// other chain goes straight to the descent. Whatever the round decides, the
// caller bootstraps afterwards — an adopted summary moves the floor the descent
// starts from, and a skipped one leaves it where it was. Nothing here is fatal:
// a node that cannot reach the beacons for this still has the descent, which is
// the same answer it had before this pass existed.
func (b *blockHandler) adoptSummary(ctx context.Context) {
	syncable, ok := b.vm.(chain.StateSyncableVM)
	if !ok {
		// Said out loud. A VM that cannot sync state is ordinary, but so is a VM
		// that can and whose summary type this assertion did not match — and those
		// two look identical from a silent return, which is how a wired client can
		// sit in a build doing nothing at all.
		b.logger.Info("state-summary adoption skipped — this VM does not offer the sync surface")
		return
	}
	enabled, err := b.stateSyncEnabled(ctx)
	switch {
	case errors.Is(err, chain.ErrStateSyncableVMNotImplemented):
		// The same fact as a failed assertion, learned one layer in: this chain's
		// VM does not carry the surface. Reporting it as a VM that could not
		// answer would send a reader looking for a fault where there is none.
		b.logger.Info("state-summary adoption skipped — this VM does not offer the sync surface")
		return
	case err != nil:
		b.logger.Info("state-summary adoption skipped — the VM could not say whether it syncs", log.Err(err))
		return
	}
	if !enabled {
		b.logger.Info("state-summary adoption skipped — the VM has state sync turned off")
		return
	}
	outcome, err := summary.New(summary.Config[chain.StateSummary]{
		Source: &summarySource{b: b},
		VM:     &syncVM{StateSyncableVM: syncable, b: b},
		Log:    b.logger,
	}).Run(ctx)
	if err != nil {
		b.logger.Info("state-summary adoption did not run; bootstrapping from the local tip",
			log.Err(err))
		return
	}
	b.logger.Info("state-summary adoption finished", log.String("outcome", outcome.String()))
}

// syncVM answers the round's two summary questions from the VM and the third —
// how far this node stands — from the node itself. The height is what the node
// has EXECUTED, not what its wrapper committed, because adopting a summary
// throws away everything below it and the wrapper's head is not a floor this
// node can vouch for.
type syncVM struct {
	chain.StateSyncableVM
	b *blockHandler
}

func (v *syncVM) Tip(ctx context.Context) (uint64, error) { return v.b.appliedHeight(ctx) }

// stateSyncEnabled asks the VM whether it wants a summary at all. A VM that does
// not answer is one that does not want one.
func (b *blockHandler) stateSyncEnabled(ctx context.Context) (bool, error) {
	type enabler interface {
		StateSyncEnabled(context.Context) (bool, error)
	}
	e, ok := b.vm.(enabler)
	if !ok {
		return false, nil
	}
	return e.StateSyncEnabled(ctx)
}
