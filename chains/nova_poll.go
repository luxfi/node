// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// nova_poll.go — the NOVA poll-response path, held STRUCTURALLY apart from the
// QUASAR (signed-vote) path.
//
// A Chits message carries NO signature: the wire type is (ChainId, RequestId,
// PreferredId, PreferredIdAtHeight, AcceptedId) and there is no field for one.
// It is a peer saying "this is my preference" — nothing more.
//
// Quasar finality counts only SIGNED votes. The engine authenticates every vote
// before tallying it,
//
//	if len(vote.Signature) == 0 || !voteVerifier.VerifyVote(...) { return }
//
// and a VoteVerifier is wired for exactly the chains with K>1 (see the K>1 arm
// of buildChain). So on any multi-validator chain a Chits-derived Vote is
// dropped BEFORE pending.VoteCount++ — alpha is unreachable by construction and
// the chain polls K-of-K forever with every node-side counter healthy.
//
// The old path braided the two concerns: it synthesised an Accept bit from a
// LOCAL re-Verify, attributed that answer to the sending peer, and handed the
// resulting signature-less consensuschain.Vote to the engine, which threw it
// away. This file makes the braid unrepresentable. A poll response is a
// NovaPollResponse — no Signature field, no Accept field — so it cannot be
// widened into a Quasar vote by assignment, conversion, or a forgotten field.
// The one remaining conversion is unsignedVoteForSoleValidatorChain, and
// receivePollResponse returns before it on any chain that requires signed votes.
package chains

import (
	"context"
	"time"

	consensuschain "github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/vm/chain"
)

// NovaPollResponse is a peer's UNSIGNED answer to a poll — the normalized
// internal form of an inbound Chits (router op handler.Vote).
//
// It has NO Signature field and NO Accept field, BY CONSTRUCTION. Both omissions
// carry weight:
//
//   - No Signature. A Chits cannot carry one, so a struct with the field would
//     be a lie that the tally path acts on — which is exactly how an unsigned
//     preference used to reach ReceiveVote.
//   - No Accept. The peer never said "accept". The prior code derived that bit
//     from THIS node's re-Verify and then attributed it to the peer, so a
//     systematic local verify failure (already-verified, missing parent, state
//     conflict) manufactured a NEGATIVE vote from an honest validator.
//
// PreferredID is the only payload that survives the router: inbound *p2p.Chits
// is flattened through message.GetContainerBytes, which returns
// m.GetPreferredId() alone — AcceptedId and AcceptedHeight are computed by both
// poll responders and then discarded before any handler sees them. Fields for
// values that can never arrive are omitted rather than shipped as permanent
// zeros; carrying them requires stopping that flattening first.
type NovaPollResponse struct {
	From        ids.NodeID // the peer whose preference this is
	PreferredID ids.ID     // the block that peer prefers
	RequestID   uint32     // the poll this answers (dedup / stale detection)
	ReceivedAt  time.Time  // when this node received it
}

const (
	// maxPollResponsesPerBlock bounds the per-block buffer so a peer streaming
	// poll responses for a block we do not hold cannot grow memory without limit.
	maxPollResponsesPerBlock = 100
	// pollResponseTTL is the age past which a buffered poll response answers a
	// poll that is long over, and is dropped at the drain.
	pollResponseTTL = 30 * time.Second
)

// unsignedVoteForSoleValidatorChain is the only legal conversion from a Chits
// preference into a vote. It is confined to K==1, where there is no remote quorum
// to impersonate and no portable certificate to assemble.
func (r NovaPollResponse) unsignedVoteForSoleValidatorChain(accept bool) consensuschain.Vote {
	return consensuschain.Vote{
		BlockID:  r.PreferredID,
		NodeID:   r.From,
		Accept:   accept,
		SignedAt: r.ReceivedAt,
	}
}

// receivePollResponse is the SINGLE ingestion point for an inbound Chits, and
// the single site where the Nova/Quasar split is decided.
func (b *blockHandler) receivePollResponse(ctx context.Context, resp NovaPollResponse) {
	// THE SPLIT. On a chain that requires signed votes there is nothing this node
	// may legally do with a peer's unsigned preference: the engine counts only
	// authenticated votes, and no sink exists here that consumes a preference
	// without inflating the alpha tally. So the response stops at the boundary —
	// no Vote is constructed, no block is re-verified per peer, nothing is
	// buffered against a block arrival that could only lead back to this point.
	//
	// The predicate is the SAME one the engine gates its tally on (a VoteVerifier
	// is wired, i.e. K>1) — deliberately NOT the engine's Mode(), which
	// additionally requires a cert gossiper and a stake source: a degraded engine
	// drops these responses identically while reporting ModeUnknown, and the old
	// warning keyed on Mode() went silent in exactly that case.
	if b.signedVotesRequired {
		// A TLS-authenticated sender identity proves who sent this packet, but it does
		// not turn the packet's preference into a signed statement over the block's
		// execution result. Multi-validator finality therefore stays exclusively on
		// BroadcastVote -> signature verification -> quorum certificate.
		if n, loud := catchupSample(&b.pollUnsignedCount); loud {
			b.logger.Debug("poll response is an unsigned preference; finality uses signed BroadcastVote",
				log.Stringer("from", resp.From),
				log.Stringer("preferredID", resp.PreferredID),
				log.Uint64("count", n))
		}
		return
	}

	// Sole-validator chain: buffer until the block is local, then apply.
	if !b.hasBlock(ctx, resp.PreferredID) {
		b.bufferPollResponse(resp)
		b.logger.Info("buffered poll response — block not in pending or VM",
			log.Stringer("from", resp.From),
			log.Stringer("preferredID", resp.PreferredID))
		return
	}
	b.applyPollResponse(ctx, resp)
}

// bufferPollResponse parks a poll response until its block arrives locally.
func (b *blockHandler) bufferPollResponse(resp NovaPollResponse) {
	b.pendingPollMu.Lock()
	defer b.pendingPollMu.Unlock()

	existing := b.pendingPollResponses[resp.PreferredID]
	if len(existing) >= maxPollResponsesPerBlock {
		return // bounded: drop rather than grow
	}
	b.pendingPollResponses[resp.PreferredID] = append(existing, resp)
}

// popBufferedPollResponses removes and returns everything parked for a block.
func (b *blockHandler) popBufferedPollResponses(blockID ids.ID) []NovaPollResponse {
	b.pendingPollMu.Lock()
	defer b.pendingPollMu.Unlock()

	resps := b.pendingPollResponses[blockID]
	delete(b.pendingPollResponses, blockID)
	return resps
}

// onBlockArrived drains the poll responses parked against a block that just
// became available locally. On a signed-vote chain nothing is ever parked (see
// receivePollResponse), so this is a no-op there.
func (b *blockHandler) onBlockArrived(ctx context.Context, blockID ids.ID) {
	resps := b.popBufferedPollResponses(blockID)
	if len(resps) == 0 {
		return
	}

	b.logger.Info("draining buffered poll responses for arrived block",
		log.Stringer("blockID", blockID),
		log.Int("count", len(resps)))

	for _, resp := range resps {
		b.applyPollResponse(ctx, resp)
	}
}

// applyPollResponse feeds a poll response to a SOLE-VALIDATOR (K==1) engine,
// where no verifier is wired and the unsigned preference is the Nova signal.
// receivePollResponse is the only caller reachable on a signed-vote chain and it
// returns before here, so the unsigned conversion below cannot be reached on one.
func (b *blockHandler) applyPollResponse(ctx context.Context, resp NovaPollResponse) {
	if b.engine == nil || b.vm == nil {
		b.logger.Warn("poll response dropped — engine or vm is nil",
			log.Stringer("from", resp.From),
			log.Stringer("preferredID", resp.PreferredID))
		return
	}

	if age := time.Since(resp.ReceivedAt); age > pollResponseTTL {
		b.logger.Debug("skipping stale poll response",
			log.Stringer("from", resp.From),
			log.Stringer("preferredID", resp.PreferredID),
			log.Duration("age", age))
		return
	}

	// First check if the block is in consensus pending (recently proposed but not
	// yet finalized), then fall back to VM storage for already-verified blocks.
	var blk chain.Block
	if pendingBlk, ok := b.engine.GetPendingBlock(resp.PreferredID); ok {
		blk = pendingBlk
		b.logger.Debug("found block in consensus pending",
			log.Stringer("from", resp.From),
			log.Stringer("preferredID", resp.PreferredID))
	} else {
		vmBlk, err := b.vm.GetBlock(ctx, resp.PreferredID)
		if err != nil {
			b.logger.Warn("block not found in pending or VM",
				log.Stringer("from", resp.From),
				log.Stringer("preferredID", resp.PreferredID),
				log.Err(err))
			return
		}
		blk = vmBlk
		b.logger.Debug("found block in VM storage",
			log.Stringer("from", resp.From),
			log.Stringer("preferredID", resp.PreferredID))
	}

	// Derive Accept from verification. The error is otherwise DISCARDED, so a
	// systematic re-verify failure (already-verified, missing parent, state
	// conflict) is indistinguishable from an honest reject.
	verifyErr := blk.Verify(ctx)
	accept := (verifyErr == nil)
	if verifyErr != nil {
		b.logger.Warn("vote is NEGATIVE — local re-verify failed",
			log.Stringer("preferredID", resp.PreferredID),
			log.Stringer("from", resp.From),
			log.Err(verifyErr))
	}

	queued := b.engine.ReceiveVote(resp.unsignedVoteForSoleValidatorChain(accept))

	// accept and queued are the two booleans that decide whether a poll can ever
	// conclude. accept=false is a NEGATIVE vote synthesised from a local
	// re-Verify, so a block every node already verified once can still be voted
	// down; queued=false means the vote never reached the engine at all.
	b.logger.Info("sent sole-validator Nova vote to consensus engine",
		log.Stringer("from", resp.From),
		log.Stringer("preferredID", resp.PreferredID),
		log.Bool("accept", accept),
		log.Bool("queued", queued))
}

// SendPollResponse answers a peer's poll with THIS node's preference. It is what
// the wire actually carries: an UNSIGNED Chits. It is not a vote, cannot be
// counted toward a quorum, and must never be described as one.
func (g *networkGossiper) SendPollResponse(chainID ids.ID, toNodeID ids.NodeID, requestID uint32, preferredID ids.ID) error {
	if g.net == nil || g.msgCreator == nil {
		return nil
	}

	// Wire: p2p.Chits. preferredID is used for preferred / preferred-at-height /
	// accepted alike — this node has verified the block it names.
	msg, err := g.msgCreator.Chits(chainID, requestID, preferredID, preferredID, preferredID, 0)
	if err != nil {
		return err
	}

	g.net.Send(msg, set.Of(toNodeID), g.networkID, 0)
	return nil
}

// SendVote exists ONLY to satisfy consensuschain.Gossiper, whose method of that
// name is a misnomer: it is documented as "sends a vote response back to the
// proposer" but every implementation can only put an UNSIGNED Chits on the wire,
// and it has ZERO production callers in either repo (the signed path is
// QuorumGossiper.BroadcastVote). It forwards to SendPollResponse so there is one
// implementation, not two.
//
// The lie lives in the interface declaration, which is outside this package's
// scope to change; renaming it there (or deleting the method — it is called by
// nothing) removes the last place the word "vote" is attached to a Chits.
func (g *networkGossiper) SendVote(chainID ids.ID, toNodeID ids.NodeID, blockID ids.ID) error {
	return g.SendPollResponse(chainID, toNodeID, 0, blockID)
}
