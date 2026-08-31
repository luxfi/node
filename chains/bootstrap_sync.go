// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// bootstrap_sync.go — the node-side wiring for INITIAL SYNC. The blockHandler is both
// the fetch transport (chainbootstrap.BlockSource) and the execute sink
// (chainbootstrap.Chain) the engine/chain/bootstrap loop drives, so an EMPTY or BEHIND
// node converges from its local last-accepted to the network frontier by fetch+execute.
// On a quorum chain, every fetched block must carry the same signed certificate used
// by live catch-up; only the sole-validator topology uses the legacy cert-less path.
// When the loop reaches the frontier the driver ends the engine's bootstrap phase and
// flips bootstrapDone, the REAL ready
// signal monitorBootstrap gates the chain on. Decomplected from the live cert/vote
// path: when bsActive is false every method below is inert and the handler behaves
// exactly as it did before initial sync existed.
package chains

import (
	"context"
	"encoding/binary"
	"errors"
	"time"

	cblock "github.com/luxfi/consensus/engine/chain/block"
	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/proto/p2p"
)

// The blockHandler IS the bootstrap loop's fetch transport AND execute sink. If it
// ever stops satisfying either, the chain cannot initial-sync — catch it at compile
// time, not in production (the same discipline as the ancestorRequester assertion). It is
// also the BootstrapPolicy's AncestrySource (the content-addressed ancestry the responder-
// agreement tally walks), keeping the trust DECISION (bootstrap_trust.go) separate from the
// transport that feeds it.
var (
	_ chainbootstrap.BlockSource = (*blockHandler)(nil)
	_ chainbootstrap.Chain       = (*blockHandler)(nil)
	_ AncestrySource             = (*blockHandler)(nil)
	_ AcceptanceSource           = (*blockHandler)(nil)
)

// Keep the certificate attached to its block between the wire decoder and the
// node's bootstrap adapter. The consensus bootstrap interface is intentionally
// kept source-compatible; Ancestors caches the proof by content-addressed block ID
// before returning bare block bytes to that interface.
type bootstrapFetchedBlock struct {
	Bytes       []byte
	Certificate []byte
}

type catchupCertificateVerifier interface {
	VerifyCatchupCertificate(context.Context, []byte, []byte) error
}

var errBootstrapCertificateRequired = errors.New("chains: signed quorum certificate required for multi-validator bootstrap block")

const (
	// bootstrapFrontierWindow bounds how long FrontierTip collects weighted beacon
	// replies before tallying the ⅔-by-stake quorum. A beacon answering later just
	// misses this round (the driver re-samples) — it is not abandoned.
	bootstrapFrontierWindow = 3 * time.Second
	// bootstrapAncestorsTimeout bounds how long Ancestors waits for a peer to serve ONE
	// batch. It is a PER-REQUEST bound and must stay well below the per-attempt budget
	// (bootstrapNamingTimeout), because the ancestor-tolerant descent issues MANY sequential
	// requests inside one attempt: a child request that can consume the whole parent budget
	// starves every later chunk.
	//
	// A per-request bound above its parent budget is worse than no chunking: the attempt
	// spends itself inside the FIRST chunk and never reaches a second, and a chunked walk
	// that never completes a walk reports FrontierNoQuorum. Nothing hangs — the ctx.Done()
	// case in the fetch loop keeps a request inside its attempt — but the descent gets
	// nowhere.
	//
	// Kept in step with the GetAncestors WIRE deadline below: asking a peer for more time than
	// we will wait just wastes its work.
	bootstrapAncestorsTimeout = 3 * time.Second
	// bootstrapAcceptedCandidates bounds the SECOND naming question in both directions: an asker
	// names at most this many candidate tips, and a responder refuses a larger question rather
	// than walking a list of ids someone else chose. Honest beacons report one tip each and
	// cluster on a handful, so the bound is never why a frontier is not named; it is what stops
	// one message from turning into an unbounded number of block lookups on the answering node.
	bootstrapAcceptedCandidates = 64
	// bootstrapAncestorSample is how many beacons one Ancestors round asks. The sample
	// ROTATES (bsRotor) so no single beacon monopolizes the descent or can repeatedly
	// stall it (M1). Content addressing in the loop makes the fetched ancestry safe
	// regardless of WHICH beacon serves it.
	bootstrapAncestorSample = 4
	// bootstrapMinAgreeingBeacons floors the DISTINCT beacons that must name the same
	// tip, so a single (even >⅔-stake) validator cannot alone determine the frontier.
	// Capped at the beacon-set size for tiny networks.
	bootstrapMinAgreeingBeacons = 2
	// bootstrapNamingWindow is the PER-FETCH ancestry chunk of the ANCESTOR-TOLERANT frontier
	// tally. Ancestry returns FULL BLOCKS, so one fetch must stay inside a network message —
	// this is a TRANSPORT bound, matching the consensus descent's per-round fetch size.
	//
	// It was previously ALSO doing two other jobs: the total ancestry budget, and a HEALTH
	// HEURISTIC ("a ⅔-common height more than this far below the highest tip is not a healthy
	// bleeding-edge split"). That heuristic is true for a LIVE chain and FALSE for a HALTED
	// one, where the skew between a straggler and a node that ran on alone is arbitrarily large
	// and perfectly healthy — no fork, just a stopped chain. Conflating the three WEDGES exactly
	// the split a halt produces: responders divide between a high tip and a low one that IS an
	// ancestor of it, the ⅔-of-responder floor would name the low block, but when the gap exceeds
	// one fetch the high tip never reaches down far enough to vouch for it and the tally names
	// nothing on every retry, forever. Recovery from a halt is exactly when this path matters
	// most, and it was disabled precisely then.
	// The total budget is now maxNamingDepth; the health heuristic is gone.
	bootstrapNamingWindow = 256
	// maxNamingAnchors bounds how many DISTINCT reported tips the ancestor-tolerant tally will
	// fetch ancestry for in one round. Honest beacons cluster on a handful of adjacent tips, so
	// this is never reached in practice; it caps the work a Byzantine swarm reporting many
	// distinct forged tips could induce. Anchors are tried most-stake-first (honest tips hold ⅔
	// of the stake, so they always fall within the cap) and a tip already seen on a
	// previously-fetched chain is skipped, so the common split costs ONE or TWO fetches.
	maxNamingAnchors = 8
	// bootstrapNamingTimeout TOTAL-bounds the ancestor-tolerant resolution (all anchor fetches
	// combined) so a genuine eclipse/partition — beacons that answer the frontier query but do
	// not serve ancestry — cannot make FrontierTip hang. When it elapses the resolution returns
	// what it found (or nothing → FrontierNoQuorum), and the loop's bounded retry (F2) tries
	// again next round against a fresh, rotated beacon sample. Honest beacons that JUST answered
	// the frontier serve the small skew ancestry near-instantly, well within this window.
	//
	// PER-ATTEMPT, and it must exceed bootstrapAncestorsTimeout by enough to fit the SEQUENTIAL
	// chunk fetches a deep descent needs — at 3s per request this is 10 chunks, i.e. 2560 blocks
	// of skew resolved in one attempt, with the bounded retry covering more. It was itself 3s,
	// equal to a single request, so the chunked descent could never advance past its first
	// fetch. The invariant is simply: a parent operation must be able to outlive its children.
	bootstrapNamingTimeout = 30 * time.Second
)

// beaconWeights returns the beacon set's nodeID→stake map and total stake under this
// chain's validation network, or ok=false when no beacon set is configured (single-node
// / --skip-bootstrap). This is the TRUST ANCHOR for initial sync: only a node in this
// set, weighted by its stake, can contribute to naming the frontier — the C1
// forged-chain gate. An arbitrary connected peer is not in this map and is ignored.
func (b *blockHandler) beaconWeights() (weights map[ids.NodeID]uint64, total uint64, ok bool) {
	if b.beacons == nil {
		return nil, 0, false
	}
	vmap := b.beacons.GetMap(b.networkID)
	if len(vmap) == 0 {
		return nil, 0, false
	}
	weights = make(map[ids.NodeID]uint64, len(vmap))
	for id, v := range vmap {
		w := v.Weight
		if w == 0 {
			w = v.Light // Weight is an alias for Light
		}
		weights[id] = w
		total += w
	}
	if total == 0 {
		return nil, 0, false
	}
	return weights, total, true
}

// connectedBeacons returns the beacons from `weights` currently CONNECTED and tracking
// this chain's validation NETWORK. The frontier quorum is tallied over these, but the FULL
// beacon stake (the `total` from beaconWeights) stays the quorum DENOMINATOR — so a node
// must hear a ⅔-stake supermajority. An eclipse that reaches < ⅔ of beacon stake cannot
// name a frontier. Returns nil when none are connected (or there is no transport).
//
// The tracking filter matches against b.networkID (the NET the beacon set is anchored to —
// constants.PrimaryNetworkID for native chains, the L1 net ID for sovereign chains), NOT
// b.chainID. The filter must match what a peer actually advertises: a peer names the NETS it
// tracks in its handshake (network/peer/peer.go adds constants.PrimaryNetworkID plus every
// tracked L1 net ID to peer.Info.TrackedChains) — it NEVER advertises an individual native chain ID like
// the C-Chain's. Filtering on b.chainID therefore matched ZERO real peers, so connectedStake
// stayed 0, FrontierTip reported FrontierConnecting forever, and a healthy stale node failed
// safe at the connect deadline instead of converging. Matching on b.networkID counts exactly
// the beacons participating in this chain's validation network. C1 is untouched: a peer is
// still admitted ONLY if it is in `weights` (the staked validator set) — a non-staked peer
// that tracks the network is still ignored.
func (b *blockHandler) connectedBeacons(weights map[ids.NodeID]uint64) []ids.NodeID {
	if b.net == nil || len(weights) == 0 {
		return nil
	}
	beaconIDs := make([]ids.NodeID, 0, len(weights))
	for id := range weights {
		beaconIDs = append(beaconIDs, id)
	}
	var connected []ids.NodeID
	var beaconPeers []ids.NodeID
	for _, p := range b.net.PeerInfo(beaconIDs) {
		if _, isBeacon := weights[p.ID]; !isBeacon {
			continue
		}
		// Connected AND staked — this peer counts unless the tracked-nets filter
		// below can positively exclude it.
		beaconPeers = append(beaconPeers, p.ID)
		if p.TrackedChains.Contains(b.networkID) {
			connected = append(connected, p.ID)
		}
	}
	// THE FILTER IS AN OPTIMISATION, NOT A SAFETY PROPERTY, so it must never be the
	// thing that decides a node cannot bootstrap. There is no other such decision: the
	// startup gate accumulates ConnectedWeight from the network's own Connected()
	// callbacks and LATCHES (`shouldStart = shouldStart || weight >= threshold`), so
	// once enough stake has connected the node starts and never un-starts. Ours
	// re-derives the answer from PeerInfo on every poll, which means a predicate that
	// matches nothing produces zero connected stake forever, however many peers are
	// genuinely there.
	//
	// That is not hypothetical: the b.chainID case above (see the note there) is exactly
	// it. A validator sits frozen at its own height reporting a healthy peer count while
	// FrontierTip answers FrontierConnecting on every pass, because the beacon set is
	// populated and the filter still admits nobody.
	//
	// So: if the filter admits NO ONE while staked beacons are demonstrably connected,
	// trust the connection. C1 is preserved either way — a peer is admitted only if it
	// is in `weights`, the staked validator set, so a non-staked peer never counts.
	// The filter ORDERS; it does not EXCLUDE. Rescuing only the all-or-nothing case
	// leaves the partial one, which is worse because it looks like it worked: some
	// beacons pass, `connected` is non-empty, the rescue never fires, and a peer that
	// holds exactly the ancestry we are missing is dropped for the life of the
	// process. A node connected to a peer at the tip then samples only peers behind it
	// and reports that no peer served the missing ancestry, without ever once asking
	// the one that could.
	//
	// So: peers that advertise the chain come first, and the remaining staked beacons
	// follow. Asking a peer that turns out not to serve costs one empty reply; not
	// asking the only peer that can costs the bootstrap.
	//
	// C1 is unchanged — every id here came from `weights`, the staked validator set,
	// so a non-staked peer is still never admitted.
	if len(connected) < len(beaconPeers) {
		if len(connected) == 0 {
			b.logger.Warn("tracked-nets filter admitted no beacon; asking the connected staked beacons instead",
				log.Stringer("networkID", b.networkID),
				log.Int("connectedStakedBeacons", len(beaconPeers)))
		}
		seen := set.NewSet[ids.NodeID](len(beaconPeers))
		seen.Add(connected...)
		for _, id := range beaconPeers {
			if !seen.Contains(id) {
				connected = append(connected, id)
			}
		}
	}
	return connected
}

// sampleAncestorBeacons returns a small ROTATED sample of connected beacons to fetch
// ancestry from (M1 — bsRotor advances each call so the sample walks the beacon set and
// no single beacon monopolizes the descent). Safety is independent of WHICH beacon
// serves: the loop's content-addressed descent rejects any block off the agreed
// frontier's parent chain, so a withholding/forging beacon costs only a re-sample.
func (b *blockHandler) sampleAncestorBeacons() (set.Set[ids.NodeID], bool) {
	weights, _, ok := b.beaconWeights()
	if !ok {
		return nil, false
	}
	connected := b.connectedBeacons(weights)
	if len(connected) == 0 {
		return nil, false
	}
	// Prefer beacons that reported an ahead tip in the last frontier round — they HOLD the
	// ancestry the descent needs, so asking them (rather than a rotated peer that may be at
	// genesis) is what lets a re-bootstrapping node obtain ancestry even when ≥2 peers are still
	// at genesis. This is ava's PeerTracker "ask a prover" bias sourced from the frontier replies
	// we already have (no separate tracker). Fill any remaining slots from the rotated full set so
	// the sample never shrinks below what blind rotation would pick (defense against a stale/empty
	// ahead-set). Empty ahead-set ⇒ pure rotation, identical to prior behavior.
	b.bsMu.Lock()
	ahead := b.bsAheadBeacons
	b.bsMu.Unlock()

	sample := set.NewSet[ids.NodeID](bootstrapAncestorSample)
	for _, id := range connected {
		if sample.Len() >= bootstrapAncestorSample {
			break
		}
		if ahead != nil && ahead.Contains(id) {
			sample.Add(id)
		}
	}
	start := int(b.bsRotor.Add(1)-1) % len(connected)
	for i := 0; i < len(connected) && sample.Len() < bootstrapAncestorSample; i++ {
		sample.Add(connected[(start+i)%len(connected)])
	}
	return sample, sample.Len() > 0
}

// fullyConnectedBeacons reports whether the node has reached its ENTIRE EXTERNAL beacon set — every
// beacon OTHER than itself is connected and tracking this chain's network. This is the eclipse-free
// condition that makes the FrontierTip SELF-VOTE safe: when the full set is reached no beacon can be
// suppressed, so a unanimous "nobody ahead" from the full set PLUS the node itself is a TRUE caught-up
// — an eclipse hiding any ahead-producer would drop that beacon from `connected` and break full-set,
// falling back to the partition-capture floor (fail safe). `connected` is connectedBeacons(weights);
// the external-set size is the beacon set minus the node itself (a node cannot, and need not, connect
// to itself). Returns false for an empty external set (a degenerate single-beacon / self-only set, so
// the self-vote is never the SOLE basis for caught-up).
func (b *blockHandler) fullyConnectedBeacons(weights map[ids.NodeID]uint64, connected []ids.NodeID) bool {
	external := len(weights)
	if _, selfIsBeacon := weights[b.selfNodeID]; selfIsBeacon {
		external-- // the node is in its own beacon set but is never one of its own peers
	}
	return external > 0 && len(connected) >= external
}

// hasExternalBeacons reports whether the staked beacon set contains any validator OTHER than this
// node. False for a self-only (single-validator) set — there is then no peer to sync from, so the
// node's own tip IS the frontier (FrontierTip returns FrontierNoBeacons). The set is read from
// P-chain STATE, not connectivity, so this is a TOPOLOGY fact (a genuine single-validator net),
// never an eclipse artifact (an eclipse drops peers from `connected`, never from `weights`).
func (b *blockHandler) hasExternalBeacons(weights map[ids.NodeID]uint64) bool {
	external := len(weights)
	if _, selfIsBeacon := weights[b.selfNodeID]; selfIsBeacon {
		external--
	}
	return external > 0
}

// withSelfVote returns `replies` plus the node's OWN accepted frontier as a beacon reply — the
// SELF-VOTE. The node is itself a beacon (selfNodeID in `weights`, the trust anchor) and knows its
// own accepted tip (lastID) with certainty, so it vouches for it exactly as a connected peer's reply
// would (deduped by NodeID in the policy's tally; collectFrontierReplies samples only PEERS, never
// self, so there is no double-count). Returned UNCHANGED when the node is not in the beacon set
// (degenerate / single-node / a P-chain whose CustomBeacons omit self) or has no accepted tip — the
// self-vote is then inert. self only ever vouches for the block IT HAS ACCEPTED, so it can never name
// a forged tip (C1 untouched); the SOLE caller gates its use on FULL connectivity, so it can never
// tip a partial (eclipsed) view into caught-up.
func (b *blockHandler) withSelfVote(replies []BeaconReply, weights map[ids.NodeID]uint64, lastID ids.ID) []BeaconReply {
	w, selfIsBeacon := weights[b.selfNodeID]
	if !selfIsBeacon || lastID == ids.Empty {
		return replies
	}
	out := make([]BeaconReply, 0, len(replies)+1)
	out = append(out, replies...)
	out = append(out, BeaconReply{NodeID: b.selfNodeID, Tip: lastID, Weight: w})
	return out
}

// repliedCovers reports whether EVERY connected beacon answered THIS frontier
// round — set containment {reply.NodeID} ⊇ connected, NOT a count (frontier
// replies are not yet round/connected-filtered, so a stale/non-connected reply
// could pad a length check). The self-vote caught-up shortcut requires this:
// without it the gate keys on CONNECTIVITY (fullyConnectedBeacons) while CaughtUp
// tallies REPLIES, and the two diverge for a beacon that is CONNECTED but SILENT
// — TCP up, its frontier reply delayed or withheld. That is a capability strictly
// WEAKER than a full eclipse (no TCP drop needed) and it occurs NATURALLY when an
// ahead beacon replays state on a mass co-restart. Such a beacon counts as "fully
// connected" yet is absent from the tally, so self's (heavy) weight backfills the
// floor and the node goes live at a forged STALE height — the precise failure the
// frontier-quorum gate exists to prevent. Requiring every connected
// beacon to have replied means a silent ahead-beacon blocks caught-up → the node
// waits / fails safe, while a genuine fresh net (every connected beacon answers
// genesis) still completes.
func repliedCovers(replies []BeaconReply, connected []ids.NodeID) bool {
	replied := make(map[ids.NodeID]struct{}, len(replies))
	for _, r := range replies {
		replied[r.NodeID] = struct{}{}
	}
	for _, id := range connected {
		if _, ok := replied[id]; !ok {
			return false
		}
	}
	return true
}

// FrontierTip implements chainbootstrap.BlockSource. It returns a FrontierStatus that
// decomplects the THREE reasons a tip may not be named. A single ok=false meant both "no
// beacon quorum reachable yet" and "nothing ahead — done", so a freshly-booted STALE node
// asking the frontier BEFORE any beacon had connected concluded "caught up" at its local
// stale height:
//
//   - FrontierNoBeacons   — no beacon set configured (single-node / dev). Nothing to sync to.
//   - FrontierConnecting  — beacons configured but not enough stake CONNECTED yet to even
//     FORM a ⅔ quorum (the boot race). The node cannot ASK — the loop must WAIT, never
//     conclude caught-up here.
//   - FrontierNoQuorum    — enough beacon stake IS connected to ask, but no ⅔-by-stake
//     agreement is reachable (genuine eclipse / partition). The loop fails SAFE.
//   - FrontierNamed       — a ⅔-by-stake SUPERMAJORITY of the configured beacons agreed on
//     the SAME accepted tip (the C1 forged-chain gate, unchanged). The tip is meaningful
//     ONLY in this case.
//
// A non-beacon peer, a single peer, or any sub-⅔-stake set can NEVER reach FrontierNamed,
// so a forged chain can never be the thing an empty/behind node syncs to.
func (b *blockHandler) FrontierTip(ctx context.Context) (ids.ID, chainbootstrap.FrontierStatus) {
	// A measurement from the previous round says nothing about this one, and a round that
	// never reaches the floor check (no beacon set yet, no transport, P-chain still loading)
	// measures nothing at all. Dropping it here is what keeps runInitialSync from reporting a
	// count that some earlier round took.
	b.shortfall.Store(nil)
	// Every wait says what it is waiting for. The status this returns carries a
	// state and no numbers, and the wait an operator reads is assembled from
	// whatever was recorded here — so a round that returns without recording
	// leaves the ambiguous form, which reads the same whether the whole beacon
	// set answered or none of it did.
	waiting := func(why string) { b.shortfall.Store(&why) }
	weights, _, haveBeacons := b.beaconWeights()
	if !haveBeacons {
		if b.expectsStakedBeacons {
			// This chain syncs against the STAKED primary-network validator set, which the
			// already-bootstrapped P-chain populates — an empty set here means it is not yet
			// loaded (sequencing) or misconfigured, NOT a single-node network. WAIT (bounded by
			// the loop's ConnectDeadline → fail safe). Critically we do NOT conclude caught-up
			// at the local stale height, and we do NOT fall back to unweighted / endpoint trust:
			// a connected peer can name the frontier ONLY through the ⅔-by-stake quorum below,
			// which is empty here. The sequencing case resolves (initValidatorSets populates the
			// set) and the node converges; a genuine empty/misconfigured set fails safe.
			waiting("the validator set is empty; no beacon can be asked yet")
			return ids.Empty, chainbootstrap.FrontierConnecting
		}
		// No beacon set expected (single-node / dev / --skip-bootstrap / P-chain endpoint-only
		// bootstrappers). Nothing to sync to.
		return ids.Empty, chainbootstrap.FrontierNoBeacons
	}
	if b.net == nil || b.msgCreator == nil {
		// Beacons configured but no transport to ask them — treat as still connecting
		// (bounded by the loop's connect deadline). In practice runBootstrapThenPoll
		// short-circuits a transport-less handler before Run is ever called.
		waiting("beacons are configured but there is no transport to ask them over")
		return ids.Empty, chainbootstrap.FrontierConnecting
	}

	// THE PARTIAL-SET GATE. A native chain's beacon set is the STAKED primary-network
	// validator set the P-chain populates as it replays its blocks (genesis stakers + every later
	// AddValidatorTx). This bootstrap goroutine starts right after the chain's VM.Initialize — NOT
	// after the P-chain has finished syncing — so without this gate FrontierTip can run while that
	// set is PARTIAL. A partial set is a NON-REPRESENTATIVE stake denominator: the MinResponseWeight
	// stake-majority floor (total/2+1) is fail-secure only when `total` is the TRUE full-set stake,
	// and under a partial denominator a degenerate set of at-or-below responders (the genuinely-ahead
	// producers not yet loaded / not yet connected) clears the floor and the decision below
	// false-completes the node at its stale local height, leaving a validator serving and voting
	// behind the network with nothing left converging it. WAIT (→ the loop's ConnectDeadline →
	// fail safe) until the staked set is fully loaded, so every branch below
	// judges the TRUE full validator set. Deadlock-free: the P-chain converges independently from its
	// own configured CustomBeacons; a native chain only waits the bounded extra moment for it.
	if b.expectsStakedBeacons && b.primaryNetworkReady != nil && !b.primaryNetworkReady() {
		b.logger.Debug("bootstrap frontier: staked validator set not yet fully loaded (P-chain still syncing) — waiting, NOT concluding caught-up",
			log.Int("partialBeaconCount", len(weights)))
		return ids.Empty, chainbootstrap.FrontierConnecting
	}

	// SELF-ONLY staked set: the configured validator set is exactly THIS node — a genuine
	// single-validator network (a sybil-protected devnet, or the sole validator of an L1). There
	// is no OTHER beacon to sync from, so the node's own accepted tip IS the network frontier →
	// immediate start (FrontierNoBeacons). This is NOT an eclipse: the staked set is read from
	// P-chain STATE, not connectivity, so a hidden peer would still appear in `weights`; only a
	// genuine single-validator topology reaches here. Placed AFTER the P-ready gate so a boot-race
	// partial set (transiently self-only mid-P-replay) WAITS above rather than false-completing here.
	// This is the sybil-protected single-validator counterpart to the empty-set FrontierNoBeacons
	// path: a multi-validator staked set (the ≥2 case) still runs the ⅔-by-stake quorum below.
	if !b.hasExternalBeacons(weights) {
		b.logger.Debug("bootstrap frontier: self-only staked set (single validator) — NoBeacons, nothing to sync to")
		return ids.Empty, chainbootstrap.FrontierNoBeacons
	}

	// THE MASS-RECOVERY FIX. The acceptance decision is the BootstrapPolicy (a SEPARATE object
	// with a SEPARATE threat model — bootstrap_trust.go), NOT the ⅔-of-CURRENT-total-stake
	// consensus floor. The prior code required a ⅔-by-stake quorum of the WHOLE validator set to
	// be CONNECTED before naming a frontier; when the recovery targets are themselves validators
	// (a crashed node IS one of the 5), that floor is mathematically unsatisfiable during a mass
	// outage — the deadlock. The policy instead requires MinResponses authenticated CONFIGURED
	// beacons to respond and a ⅔-of-RESPONDERS agreement, so 3 of 5 reachable beacons all agreeing
	// recover the network even though 3 of 5 STAKE is not a finalizing supermajority. Finality is
	// untouched (it lives in consensus and still needs > ⅔ of current stake).
	connected := b.connectedBeacons(weights)
	policy := b.bootstrapPolicy(weights)
	replies := b.collectFrontierReplies(ctx, connected, weights)

	// INSTRUMENTATION (ground truth for the decision below). Per attempt, record the FULL
	// beacon-set size + total stake (the floor DENOMINATOR — a partial value here is the smoking
	// gun), the connected count, and every reply's tip + LOCALLY-resolved height + held-or-not.
	// Debug level so production is silent; raise the chain's log level to Debug to capture it.
	b.logFrontierInputs(weights, connected, replies)

	lastID, lastH, lastErr := b.LastAccepted(ctx)
	haveLast := lastErr == nil && lastID != ids.Empty

	// Full-coverage judgement for the own-tip frontier exemption: every configured beacon connected
	// AND replied this round (the same pair of guards the self-vote shortcut uses), so no ahead
	// beacon can be hidden. Reusing both is deliberate — connectivity alone admits a CONNECTED but
	// SILENT beacon, which is a strictly weaker capability than an eclipse.
	if haveLast {
		policy.Tip = lastID
		policy.Covered = b.fullyConnectedBeacons(weights, connected) && repliedCovers(replies, connected)
	}

	// A signed quorum certificate outranks the unsigned responder tally, because a
	// tally counts peers and a certificate carries proof. A majority naming an
	// older tip does not make it the tip: a minority holding newer finalized
	// blocks can prove it, and those blocks carry portable certs that say so.
	//
	// Probe every distinct ahead claim and choose the highest one whose certificate
	// independently verifies against our validator set. An arbitrary peer can now
	// advertise any ID it likes, but it cannot make that ID a frontier without the
	// same signed proof required by live finality. Invalid/unserved claims are ignored
	// and fall through to the bounded bootstrap policy below.
	if b.signedVotesRequired {
		localHeight := uint64(0)
		if haveLast {
			localHeight = lastH
		}
		if certifiedID, certifiedHeight, ok := b.highestCertifiedFrontier(ctx, replies, localHeight); ok {
			b.logger.Info("bootstrap frontier: selected cryptographically certified tip over unsigned responder tally",
				log.Stringer("tip", certifiedID),
				log.Uint64("height", certifiedHeight),
				log.Uint64("localHeight", localHeight))
			b.namingProgress = nil
			return certifiedID, chainbootstrap.FrontierNamed
		}
	}

	frontier, err := policy.AcceptsFrontier(ctx, replies)
	switch {
	case err == nil:
		// A configured-beacon quorum named a safe sync anchor (NOT a finality cert — the loop
		// re-executes the descent and re-enters consensus, where FinalityQuorum alone governs).
		// With the P-ready gate above, this judgement is over the TRUE FULL staked set, so a tip the
		// quorum actively names is a real frontier (when it equals our own held tip, a ⅔-of-responders
		// supermajority is AT our height, so we ARE at the network frontier — the loop's Accepted()-shortcut
		// then correctly concludes caught-up). A Byzantine minority reporting an unheld forged sibling
		// cannot reach the ⅔ agreement, so it is never named (C1) and — crucially — never BLOCKS the
		// honest named tip either (we do NOT require holding every reported tip to accept the named
		// one; that stricter rule belongs to the dual CaughtUp path below, which decides the
		// no-quorum/own-height case).
		b.logger.Debug("bootstrap frontier: NAMED",
			log.Stringer("tip", frontier.ID),
			log.Uint64("namedHeight", frontier.Height),
			log.Int("responders", frontier.Responders),
			// ACCEPTED, not merely held: a gossiped-but-unaccepted frontier logs
			// accepted=false here, so the loop's Accepted()-shortcut DESCENDS instead of completing
			// at the stale tip. The diagnostic now distinguishes "in the store" from "finalized".
			log.Bool("accepted", b.Accepted(ctx, frontier.ID)))
		// The descent is over, so its retained refs are released.
		b.namingProgress = nil
		return frontier.ID, chainbootstrap.FrontierNamed
	case errors.Is(err, ErrInsufficientBootstrapResponses):
		// Below the PEER-ONLY response floor. Before WAITING, apply the SELF-VOTE under FULL
		// connectivity — THE FRESH-NET FIX. The node is itself a beacon and knows its OWN accepted
		// tip with certainty. When it has reached its ENTIRE beacon set (fullyConnectedBeacons — so no
		// eclipse can be hiding an ahead-tip) and that full set PLUS itself unanimously hold a tip the
		// node has ALREADY ACCEPTED, the node IS at the network frontier — even though the peer-only
		// responder weight could not reach the stake-majority NAMING floor. That floor is unsatisfiable
		// by PEERS ALONE precisely when the node's own stake is large relative to the rest (a heavy
		// validator, a small beacon set like the P-chain's CustomBeacons, or skewed stake): peers =
		// total − self, so peers < total/2+1 ⟺ self > total/2−1. On a fresh net every validator holds
		// only genesis, so such a node would otherwise hang in FrontierConnecting forever with the
		// WHOLE set connected. SAFE: self is counted ONLY under full connectivity, so an eclipse that
		// suppresses ANY beacon breaks full-set and we fall through to the partition-capture floor
		// (FrontierConnecting → fail safe) — self can never tip a PARTIAL (eclipsed) view into a false
		// caught-up. self also only ever vouches for its OWN accepted tip, so C1 (forged-frontier
		// naming) is untouched, and a genuinely behind node has an ahead peer in the full set → CaughtUp
		// is false → it keeps waiting/syncing.
		// EXECUTED, not merely DECIDED. The acceptance oracle answers from the consensus
		// ledger, which is the right source for "has this block been accepted" and the
		// wrong one for "may this node go live": the ledger advances when a quorum
		// decides, and the VM advances when this node runs the block. They diverge, and
		// on the divergence the ledger is HIGHER — so a node whose VM is behind reads as
		// caught up, goes live at its stale height, and then serves and votes there
		// while never converging. That is the failure this whole gate exists to make
		// impossible, arriving through the one input nobody checked.
		//
		// A validator can therefore report itself bootstrapped with its ledger at the
		// network's height and its VM hundreds of blocks below it — live, polling
		// normally, and behind the whole time.
		//
		// So going live additionally requires the VM to have EXECUTED to the head we are
		// claiming. This can only ever REFUSE, never permit: a node that has genuinely
		// executed to its head is unaffected, and a node that has not stays syncing,
		// which is the safe side of a go-live decision. Both caught-up branches consult
		// the same rule — there is one way to be allowed live.
		if haveLast && b.executedTo(ctx, lastH) && b.fullyConnectedBeacons(weights, connected) &&
			repliedCovers(replies, connected) &&
			policy.CaughtUp(b.withSelfVote(replies, weights, lastID), lastH, b.acceptedHeight) {
			b.logger.Debug("bootstrap frontier: CAUGHT UP at own tip (full beacon set + self all at/below it, peer-only weight below the naming floor)",
				log.Stringer("tip", lastID),
				log.Uint64("height", lastH),
				log.Int("beaconSetSize", len(weights)),
				log.Int("connected", len(connected)))
			return lastID, chainbootstrap.FrontierCaughtUp
		}
		// Fewer than MinResponses configured beacons have RESPONDED (and not provably caught-up under
		// full connectivity) — not a partition-capture-safe quorum yet. More may connect: WAIT (bounded
		// by the loop's ConnectDeadline, then fail safe). Never false-complete; never trust the few.
		//
		// This is the only place the shortfall is known: the status this returns carries a state and
		// no numbers, and the loop above it fails safe on a bare sentinel. So record it for
		// runInitialSync, which is where the wait an operator asks about is declared.
		responders, responderWeight, _, _ := policy.tallyResponders(replies)
		short := policy.shortfall(responders, responderWeight)
		waiting(short)
		b.logger.Debug("bootstrap frontier: CONNECTING (below the MinResponses floor)",
			log.String("shortfall", short),
			log.Int("beaconSetSize", len(weights)),
			log.Int("connected", len(connected)),
			log.Int("replies", len(replies)))
		return ids.Empty, chainbootstrap.FrontierConnecting
	default:
		// ErrNoBootstrapQuorum: enough beacons responded (the FLOOR is met) but no ⅔-of-responders
		// agreement named a common committed block this round. Before failing safe, check whether we
		// are CAUGHT UP. A TIP-HOLDER on a mixed-height co-restart sees the responders SPLIT below ⅔
		// (the tip-holders are only half), so NOTHING is named, yet it is plainly not behind — and
		// the ancestor-tolerant tally cannot name the ⅔-backed COMMON ancestor either, because that
		// ancestor is AT OR BELOW the node's own last-accepted (MinFrontierHeight names only STRICTLY
		// ABOVE it). That own-height exclusion is also the M1 eclipse-stale fix: a block at the node's
		// height that accrues ⅔ purely as the shared ANCESTOR of ahead-tips an eclipse throttled below
		// the naming threshold is NOT named — so this same CaughtUp gate, not nameFrontier, decides the
		// at-own-height case, and it REFUSES when any responder is genuinely ahead (the node lacks that
		// tip). Without this the tip-holder fails safe DOWN at its own tip — the opposite of the
		// stale-go-live bug.
		//
		// policy.CaughtUp is true ONLY when the floor is met AND no responder reports an accepted tip
		// above our height AND we hold every reported tip (its full safety argument lives on the
		// method). When it holds, the network frontier IS our own accepted tip: return it NAMED so
		// the loop's existing Accepted()-shortcut (bootstrapper "named a tip we have ACCEPTED → synced")
		// concludes caught-up and we go Ready at our OWN height — never below it, never at a stale
		// height. A genuinely stale node has an honest responder ahead (or lacks its tip) → CaughtUp
		// is false → it keeps syncing; an eclipse hiding the ahead-nodes drops below the floor →
		// CaughtUp is false → it fails safe. Distinct from the transient finality-skew retry the loop
		// runs on a bare FrontierNoQuorum ("connected but momentarily split" — keep polling): this is
		// "split AND I am at the top of every responder" — provably DONE, not waiting.
		if haveLast && b.executedTo(ctx, lastH) && policy.CaughtUp(replies, lastH, b.acceptedHeight) {
			b.logger.Debug("bootstrap frontier: CAUGHT UP at own tip (floor met, no responder ahead, hold every reported tip)",
				log.Stringer("tip", lastID),
				log.Uint64("height", lastH))
			return lastID, chainbootstrap.FrontierNamed
		}
		// WHY nothing was named decides whether retrying can ever succeed, so the two
		// reasons are logged as the different facts they are. ErrNoBootstrapQuorum is a
		// statement about the network — the responders agree on nothing we may name —
		// and retrying polls a fresh sample. ErrNamingIncomplete is a statement about
		// our own budget: the descent ran out mid-walk, its cursors are saved, and the
		// next round resumes DEEPER. Reported as no-quorum, a wide gap looks like a
		// permanent disagreement and the descent restarts at the tip every round, so a
		// gap wider than one attempt's budget is never crossed.
		if errors.Is(err, ErrNamingIncomplete) {
			deepest := 0
			for _, prog := range b.namingProgress {
				if prog.Traversed > deepest {
					deepest = prog.Traversed
				}
			}
			b.logger.Debug("bootstrap frontier: INCOMPLETE (attempt budget spent, not a disagreement) — resuming deeper next round",
				log.Uint64("lastAccepted", lastH),
				log.Int("anchorsInProgress", len(b.namingProgress)),
				log.Int("deepestTraversed", deepest))
			return ids.Empty, chainbootstrap.FrontierNoQuorum
		}
		b.logger.Debug("bootstrap frontier: NO QUORUM (responders split, not caught up) — retry/fail safe",
			log.Uint64("lastAccepted", lastH),
			log.Int("replies", len(replies)))
		return ids.Empty, chainbootstrap.FrontierNoQuorum
	}
}

// highestCertifiedFrontier returns the highest strict-ahead responder tip backed
// by a locally verified quorum certificate. It is read-only: certificate checking
// neither tracks nor finalizes the block, so ordered execution remains the
// bootstrapper's responsibility.
func (b *blockHandler) highestCertifiedFrontier(ctx context.Context, replies []BeaconReply, localHeight uint64) (ids.ID, uint64, bool) {
	if b.engine == nil || b.vm == nil {
		return ids.Empty, 0, false
	}
	verifier, ok := any(b.engine).(catchupCertificateVerifier)
	if !ok {
		return ids.Empty, 0, false
	}
	return selectHighestCertifiedFrontier(replies, localHeight, func(tip ids.ID) (uint64, bool) {
		if b.Accepted(ctx, tip) {
			return 0, false
		}
		fetched, err := b.fetchBootstrapAncestors(ctx, tip, 1)
		if err != nil || len(fetched) == 0 {
			return 0, false
		}
		candidate := fetched[len(fetched)-1]
		blk, err := b.vm.ParseBlock(ctx, candidate.Bytes)
		if err != nil || blk.ID() != tip {
			return 0, false
		}
		if err := verifier.VerifyCatchupCertificate(ctx, candidate.Bytes, candidate.Certificate); err != nil {
			b.logger.Debug("bootstrap frontier: responder tip has no valid quorum certificate",
				log.Stringer("tip", tip),
				log.Uint64("height", blk.Height()),
				log.Err(err))
			return 0, false
		}
		return blk.Height(), true
	})
}

// selectHighestCertifiedFrontier is the deterministic policy core: responder
// multiplicity is irrelevant once a tip supplies its own quorum proof. probe must
// return true only after cryptographic verification.
func selectHighestCertifiedFrontier(replies []BeaconReply, localHeight uint64, probe func(ids.ID) (uint64, bool)) (ids.ID, uint64, bool) {
	seen := make(map[ids.ID]struct{}, len(replies))
	var bestID ids.ID
	var bestHeight uint64
	for _, reply := range replies {
		if reply.Tip == ids.Empty {
			continue
		}
		if _, duplicate := seen[reply.Tip]; duplicate {
			continue
		}
		seen[reply.Tip] = struct{}{}
		height, verified := probe(reply.Tip)
		if !verified || height <= localHeight {
			continue
		}
		if bestID == ids.Empty || height > bestHeight || (height == bestHeight && reply.Tip.Compare(bestID) < 0) {
			bestID = reply.Tip
			bestHeight = height
		}
	}
	return bestID, bestHeight, bestID != ids.Empty
}

// logFrontierInputs records, at Debug level, the raw inputs to one FrontierTip decision — the
// ground truth for it. The FULL beacon-set size + total stake is the floor's
// DENOMINATOR: if it is a PARTIAL value (the staked set still loading), the stake-majority floor
// is non-representative and a degenerate at-or-below responder subset can false-complete the node
// — so logging the denominator alongside every reply's tip + locally-resolved height + held-or-not
// is exactly what distinguishes "caught up" from "tricked by a partial set". Silent in production
// (Debug); raise the chain's log level to Debug to capture it.
func (b *blockHandler) logFrontierInputs(weights map[ids.NodeID]uint64, connected []ids.NodeID, replies []BeaconReply) {
	var total uint64
	for _, w := range weights {
		total += w
	}
	for _, r := range replies {
		h, accepted := b.acceptedHeight(r.Tip)
		b.logger.Debug("bootstrap frontier reply",
			log.Stringer("from", r.NodeID),
			log.Stringer("reportedTip", r.Tip),
			log.Uint64("beaconWeight", r.Weight),
			// ACCEPTED (on our finalized chain), not merely present in the store: a beacon tip we
			// hold-but-have-not-accepted logs accepted=false with resolvedHeight 0 — the smoking gun
			// that distinguishes a genuine catch-up from a stored-but-unaccepted tip.
			log.Bool("accepted", accepted),
			log.Uint64("resolvedHeight", h))
	}
	b.logger.Debug("bootstrap frontier inputs",
		log.Int("beaconSetSize", len(weights)),
		log.Uint64("beaconSetTotalStake", total),
		log.Int("connectedBeacons", len(connected)),
		log.Int("replies", len(replies)))
}

// bootstrapPolicy builds the BootstrapTrust DECISION object for this round from the configured
// beacon set (the trust anchor — INVARIANT 1: configured/checkpoint/genesis, never peer
// self-report). MinResponses defaults to a MAJORITY of the configured beacons (the largest floor
// that still lets the node recover when a minority of validators is down); the agreement is ⅔ of
// the RESPONDERS; MinFrontierHeight is the node's last-accepted height (the ancestor-tolerant path
// names only a block STRICTLY ABOVE it — never at or beneath where the node already is, so an
// eclipse cannot make the node's own height a false frontier; that case routes to CaughtUp — fail
// safe, not false-complete). All are operator-overridable via the blockHandler fields; zero ⇒ the
// documented default.
func (b *blockHandler) bootstrapPolicy(weights map[ids.NodeID]uint64) *BootstrapPolicy {
	minResp := b.bootstrapMinResponses
	if minResp <= 0 {
		minResp = len(weights)/2 + 1 // MAJORITY of the configured beacon set
	}
	if minResp > len(weights) {
		minResp = len(weights)
	}
	var lastH uint64
	if _, h, err := b.LastAccepted(context.Background()); err == nil {
		lastH = h
	}
	// STAKE-MAJORITY FLOOR (closes the skewed-weight partition-capture forgery). MinResponses is a
	// COUNT of beacons; AgreementThreshold is over responder WEIGHT. Under SKEWED validator stake
	// these diverge: an attacker who eclipses the HEAVY honest beacons but lets enough LIGHT honest
	// beacons through to satisfy the count floor can shrink the responder-weight denominator until
	// his < ⅓-of-total Byzantine stake clears the ⅔-OF-RESPONDERS agreement and names a forged
	// frontier. Requiring the responders to also carry > ½ of TOTAL configured beacon stake bounds
	// that denominator from below, so a < ⅓-stake adversary can never reach a responder-weight ⅔
	// majority. Proven safe AND deadlock-free: equal-weight recovery needs only > ½ (e.g. 3/5 =
	// 0.6·total ≥ 0.5), NOT the > ⅔ that caused the original mass-recovery deadlock. Disabled only
	// when no weights are known (degenerate single-node / pre-P-chain).
	var minRespWeight uint64
	var total uint64
	for _, w := range weights {
		total += w
	}
	if total > 0 {
		minRespWeight = total/2 + 1 // ⌈total/2⌉ stake-majority floor
	}
	return &BootstrapPolicy{
		TrustedBeacons:     weights,
		AgreementThreshold: b.bootstrapAgreement, // zero ⇒ policy default ⅔
		MinResponses:       minResp,
		MinResponseWeight:  minRespWeight, // stake-majority floor (anti skewed-weight partition-capture)
		MinResponders:      bootstrapMinAgreeingBeacons,
		MinFrontierHeight:  lastH,
		Checkpoint:         b.bootstrapCheckpoint,
		NamingWindow:       bootstrapNamingWindow,
		MaxAnchors:         maxNamingAnchors,
		Source:             b,
		Acceptance:         b,
		// The descent state is the HANDLER's and outlives this policy, so an attempt
		// that runs out of budget resumes deeper next round instead of restarting at
		// the tip. Budget fields default in namingBudget().
		Progress: b.ancestryProgress(),
	}
}

// collectFrontierReplies sends GetAcceptedFrontier to the connected beacons and gathers ONE
// reply per beacon within the window, returning them as []BeaconReply for the BootstrapPolicy to
// JUDGE. This is pure TRANSPORT — it does not decide. The DECISION (which beacons count, the
// MinResponses floor, the ⅔-of-responders agreement, the ancestor-tolerant tally) is the
// policy's (bootstrap_trust.go), keeping the trust object separate from the wire.
//
// It early-returns the instant every connected beacon has answered (no need to wait out the
// window on the common fully-connected path), bounding a slow/silent minority by the window.
// Non-beacon and empty replies are dropped here too (the policy re-filters — defense in depth).
func (b *blockHandler) collectFrontierReplies(ctx context.Context, connected []ids.NodeID, weights map[ids.NodeID]uint64) []BeaconReply {
	if len(connected) == 0 || b.net == nil || b.msgCreator == nil {
		return nil
	}
	ch := make(chan bsFrontierReply, len(connected))
	b.bsMu.Lock()
	b.bsFrontierCh = ch
	b.bsMu.Unlock()
	defer func() {
		b.bsMu.Lock()
		b.bsFrontierCh = nil
		b.bsMu.Unlock()
	}()

	b.contextRequestMu.Lock()
	b.requestIDCounter++
	requestID := b.requestIDCounter
	b.contextRequestMu.Unlock()

	msg, err := b.msgCreator.GetAcceptedFrontier(b.chainID, requestID, 10*time.Second)
	if err != nil {
		return nil
	}
	sample := set.NewSet[ids.NodeID](len(connected))
	sample.Add(connected...)
	b.net.Send(msg, sample, b.networkID, 0)

	replies := make([]BeaconReply, 0, len(connected))
	seen := make(map[ids.NodeID]struct{}, len(connected))
	// ahead = beacons reporting a tip this node has NOT accepted (they hold blocks we lack, so
	// they can SERVE the ancestry the descent needs). sampleAncestorBeacons prefers them so the
	// GetAncestors sample targets peers that can serve — never a peer still at genesis (our own
	// tip). Recorded per round; publishes into b.bsAheadBeacons before returning.
	ahead := set.NewSet[ids.NodeID](len(connected))
	record := func() []BeaconReply {
		b.bsMu.Lock()
		b.bsAheadBeacons = ahead
		b.bsMu.Unlock()
		return replies
	}
	deadline := time.After(bootstrapFrontierWindow)
	for {
		select {
		case rep := <-ch:
			w, isBeacon := weights[rep.nodeID]
			if !isBeacon || rep.tip == ids.Empty {
				continue // only a configured beacon's non-empty reply counts
			}
			if _, dup := seen[rep.nodeID]; dup {
				continue // one reply per beacon
			}
			seen[rep.nodeID] = struct{}{}
			replies = append(replies, BeaconReply{NodeID: rep.nodeID, Tip: rep.tip, Weight: w})
			if _, accepted := b.acceptedHeight(rep.tip); !accepted {
				ahead.Add(rep.nodeID) // reported a tip we have not accepted → genuinely ahead
			}
			if len(seen) >= len(connected) {
				return record() // every connected beacon answered — resolve now, do not wait the window
			}
		case <-deadline:
			return record()
		case <-ctx.Done():
			return record()
		}
	}
}

// Acceptance implements AcceptanceSource: ask `from` which of `candidates` they have ACCEPTED and
// gather ONE answer per beacon within the reply window. Pure TRANSPORT — which answers count and
// which candidate is named is the policy's (bootstrap_trust.go), the same split collectFrontierReplies
// keeps for the first question. It reuses that question's window: it is the same round trip to the
// same peers, moments later.
//
// A candidate this node has ALREADY ACCEPTED is dropped before asking. That is what keeps the second
// question from ever moving the node sideways or backwards: every block it can name is one this node
// lacks, so a named block always makes the loop DESCEND and fetch it, and can never be read as
// "already synced" at a height the node is stuck on. A node at the top of every reported tip
// therefore asks NOTHING and sends no message at all.
func (b *blockHandler) Acceptance(ctx context.Context, candidates []ids.ID, from []ids.NodeID) []AcceptedReply {
	if b.net == nil || b.msgCreator == nil || len(from) == 0 {
		return nil
	}
	want := make([]ids.ID, 0, len(candidates))
	for _, id := range candidates {
		if _, accepted := b.acceptedHeightCtx(ctx, id); !accepted {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return nil
	}

	b.contextRequestMu.Lock()
	b.requestIDCounter++
	requestID := b.requestIDCounter
	b.contextRequestMu.Unlock()

	sample := set.NewSet[ids.NodeID](len(from))
	sample.Add(from...)
	round := bsAcceptedRound{asked: sample, ch: make(chan bsAcceptedReply, len(from))}
	b.bsMu.Lock()
	// Self-arming, as the serve limiter is: a handler assembled as a struct literal rather than
	// through newBlockHandler must not nil-map-panic on a path that only recovery reaches.
	if b.bsAccepted == nil {
		b.bsAccepted = make(map[uint32]bsAcceptedRound)
	}
	b.bsAccepted[requestID] = round
	b.bsMu.Unlock()
	defer func() {
		b.bsMu.Lock()
		delete(b.bsAccepted, requestID)
		b.bsMu.Unlock()
	}()

	msg, err := b.msgCreator.GetAccepted(b.chainID, requestID, bootstrapFrontierWindow, want)
	if err != nil {
		return nil
	}
	b.net.Send(msg, sample, b.networkID, 0)

	answers := make([]AcceptedReply, 0, len(from))
	seen := make(map[ids.NodeID]struct{}, len(from))
	deadline := time.After(bootstrapFrontierWindow)
	for {
		select {
		case rep := <-round.ch:
			if _, dup := seen[rep.nodeID]; dup {
				continue // one answer per beacon
			}
			seen[rep.nodeID] = struct{}{}
			answers = append(answers, AcceptedReply{NodeID: rep.nodeID, Accepted: rep.accepted})
			if len(seen) >= len(from) {
				return answers // everyone asked has answered — resolve now, do not wait the window
			}
		case <-deadline:
			return answers
		case <-ctx.Done():
			return answers
		}
	}
}

// Ancestors implements chainbootstrap.BlockSource: fetch up to maxBlocks blocks ending
// at blockID, OLDEST-FIRST, from a ROTATED sample of beacons (wire: GetAncestors ->
// Ancestors). An empty result (no error) means the sampled beacon did not serve — the
// loop re-samples. The fetched ancestry is made safe by the loop's content-addressed
// descent (off-path blocks ignored), not by trusting the serving peer.
func (b *blockHandler) Ancestors(ctx context.Context, blockID ids.ID, maxBlocks int) ([][]byte, error) {
	fetched, err := b.fetchBootstrapAncestors(ctx, blockID, maxBlocks)
	if err != nil || len(fetched) == 0 {
		return nil, err
	}
	raw := make([][]byte, 0, len(fetched))
	certs := make(map[ids.ID][]byte, len(fetched))
	for _, entry := range fetched {
		raw = append(raw, entry.Bytes)
		if len(entry.Certificate) == 0 || b.vm == nil {
			continue
		}
		blk, parseErr := b.vm.ParseBlock(ctx, entry.Bytes)
		if parseErr != nil {
			continue
		}
		certs[blk.ID()] = append([]byte(nil), entry.Certificate...)
	}
	if len(certs) > 0 {
		b.bsMu.Lock()
		if b.bsCertificates == nil {
			b.bsCertificates = make(map[ids.ID][]byte)
		}
		for id, cert := range certs {
			b.bsCertificates[id] = cert
		}
		b.bsMu.Unlock()
	}
	return raw, nil
}

func (b *blockHandler) fetchBootstrapAncestors(ctx context.Context, blockID ids.ID, maxBlocks int) ([]bootstrapFetchedBlock, error) {
	if b.net == nil || b.msgCreator == nil {
		return nil, nil
	}
	sample, ok := b.sampleAncestorBeacons()
	if !ok {
		return nil, nil
	}

	b.contextRequestMu.Lock()
	b.requestIDCounter++
	requestID := b.requestIDCounter
	b.contextRequestMu.Unlock()

	// Buffer the whole sample so EVERY sampled beacon's reply can queue — the loop
	// then skips the EMPTY ones (a beacon that lacks the requested block, e.g. a peer
	// still at genesis) and returns the first NON-EMPTY batch. With a size-1 channel a
	// fast empty reply won the race and starved a slower peer that actually held the
	// ancestry, so a re-bootstrapping node with ≥2 peers at genesis could keep drawing
	// empties and stall. The contract is the settled one: an empty Ancestors
	// reply means "this peer can't serve — take another's," never "done" (getter serves an
	// EXPLICIT empty batch when it lacks the block; see GetContext).
	ch := make(chan []bootstrapFetchedBlock, bootstrapAncestorSample)
	b.bsMu.Lock()
	b.bsAncestorCh[requestID] = ch
	// Remember WHO was asked. A request id is ours, not a secret: any connected peer
	// can send a reply carrying it. Correlating on the id alone lets an unsampled peer
	// answer a fetch we never made of it, and one such reply abandons the pass — a
	// stranger can stall a node's initial sync indefinitely by answering promptly.
	if b.bsAncestorPeers == nil {
		b.bsAncestorPeers = make(map[uint32]set.Set[ids.NodeID])
	}
	b.bsAncestorPeers[requestID] = sample
	b.bsMu.Unlock()
	defer func() {
		b.bsMu.Lock()
		delete(b.bsAncestorCh, requestID)
		delete(b.bsAncestorPeers, requestID)
		b.bsMu.Unlock()
	}()

	// The WIRE deadline we grant the peer is the SAME bound we will actually wait
	// (bootstrapAncestorsTimeout) — one number, so a peer is never asked to spend longer
	// building a batch than we are prepared to accept it for.
	msg, err := b.msgCreator.GetAncestors(b.chainID, requestID, bootstrapAncestorsTimeout, blockID, p2p.EngineType_ENGINE_TYPE_CHAIN)
	if err != nil {
		return nil, err
	}
	b.net.Send(msg, sample, b.networkID, 0)

	deadline := time.After(bootstrapAncestorsTimeout)
	for {
		select {
		case blocks := <-ch:
			if len(blocks) == 0 {
				continue // this beacon can't serve the block — wait for a peer that can
			}
			return blocks, nil
		case <-deadline:
			return nil, nil // no beacon in the sample served — the loop re-samples (rotated)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Ancestry implements the BootstrapPolicy's AncestrySource: fetch a tip's ancestry over the wire
// and parse each block to its CONTENT-ADDRESSED (id, height, parent). It reuses the SAME Ancestors
// transport + ParseBlock the sync loop trusts, so a forging peer cannot fake the parent linkage
// the responder-agreement tally walks — and the trust DECISION (bootstrap_trust.go) stays free of
// any VM/block dependency, taking only []BlockRef. An empty fetch (peer did not serve) yields nil
// so that anchor simply contributes nothing; a malformed served block fails the whole anchor (it
// is not safe to trust a chain we cannot parse).
func (b *blockHandler) Ancestry(ctx context.Context, tip ids.ID, max int) ([]BlockRef, error) {
	raw, err := b.Ancestors(ctx, tip, max)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	refs := make([]BlockRef, 0, len(raw))
	for _, bz := range raw {
		blk, perr := b.vm.ParseBlock(ctx, bz)
		if perr != nil {
			return nil, perr
		}
		refs = append(refs, BlockRef{ID: blk.ID(), Height: blk.Height(), Parent: blk.ParentID()})
	}
	return refs, nil
}

// ParseBlock implements chainbootstrap.Chain: decode block bytes through the SAME
// builder the engine parses through (identity-preserving), so the loop reads each
// block's height + parent for ordering and the descent.
func (b *blockHandler) ParseBlock(ctx context.Context, raw []byte) (cblock.Block, error) {
	return b.vm.ParseBlock(ctx, raw)
}

// LastAccepted implements chainbootstrap.Chain: the node's ACCEPTED tip id + height, read
// from the IN-PROCESS consensus finalized ledger — the SINGLE advancing source of truth the
// loop drives convergence off (THE one-source decomplect). It is NOT read from the VM cache.
//
// THE FREEZE-STALL: the descent DOES accept the run-up (the consensus finalized
// ledger AND the EVM's on-disk store both advance), but the ZAP VM client caches its
// LastAccepted at Initialize and a fire-and-forget Accept never refreshes it (client.go) —
// so VM.LastAccepted is FROZEN for the process life. Reading lastH off that frozen cache made
// every pass after the first re-descend from the stale height while AcceptBootstrapBlock
// no-oped each block (height ≤ the ADVANCED finalizedHeight) → advanced=false → ErrStalled:
// "unfreezes but stalls". AcceptBootstrapBlock's own contiguity guard already trusts the
// in-process ledger (consensus.GetFinalizedHeight); driving lastH/the caught-up oracle off the
// SAME ledger is the decomplect — the loop trusts the ledger it is building, not a VM cache.
// executedTo reports whether this node's VM has actually RUN the chain up to h.
//
// The acceptance oracle answers from the consensus ledger — what a quorum decided —
// which is right for "has this block been accepted" and wrong for "may this node go
// live". The two diverge, and on the divergence the ledger is HIGHER: a quorum
// decides a block before this node executes it, and if execution then fails or lags
// the ledger keeps the higher number. A gate reading only the ledger therefore lets
// a node whose VM is far below the head declare itself at the frontier, go live, and
// serve and vote at a stale height while nothing is left trying to converge it.
//
// A VM that cannot be read is not a VM that has executed: an unproven measurement is
// not a passing one, so it refuses. This can only ever REFUSE go-live, never permit
// it, which is the safe side of the decision.
func (b *blockHandler) executedTo(ctx context.Context, h uint64) bool {
	if b.vm == nil {
		return false
	}
	applied, err := b.appliedHeight(ctx)
	if err != nil {
		return false // cannot prove execution ⇒ do not go live
	}
	return applied >= h
}

// innerLastAccepted is the head of the VM that actually executes blocks.
// appliedHeight is how far this node has actually RUN the chain — a different
// question from LastAccepted, which names the chain's head.
//
// On a wrapped chain the wrapper is committed before its inner block is accepted,
// so it leads during every accept and keeps leading if the inner never lands. A
// caller that asks the wrapper how far the node has run is told yes for blocks
// the inner VM never got: it lowers its position onto a height it does not hold,
// skips exactly the band it must fetch, and asks forever while reporting full
// batches landed. A VM that cannot speak for an inner self is its own inner self,
// and answers with its head.
func (b *blockHandler) appliedHeight(ctx context.Context) (uint64, error) {
	type innerHeight interface {
		InnerLastAcceptedHeight(context.Context) (uint64, error)
	}
	if inner, ok := b.vm.(innerHeight); ok {
		return inner.InnerLastAcceptedHeight(ctx)
	}
	_, h, err := b.vmLastAccepted(ctx)
	return h, err
}

func (b *blockHandler) LastAccepted(ctx context.Context) (ids.ID, uint64, error) {
	return b.finalizedTip(ctx)
}

// finalizedTip returns the in-process consensus finalized ledger position (the ADVANCING
// source), falling back to the VM's last-accepted ONLY when the ledger is unset — the
// empty-genesis boot window before the first finalize (consensus SyncState seeds the ledger
// from a NON-empty last-accepted; an empty node leaves it unset until block 1 finalizes), or a
// degenerate/test handler with no engine. The VM fallback is correct there: at genesis the VM
// last-accepted IS the anchor and has not yet had a chance to go stale (it only diverges from
// the ledger once blocks finalize, at which point the ledger takes over). This mirrors the
// engine's own M2 first-block anchor (bootstrap_accept.go localLastAccepted), keeping ONE rule.
func (b *blockHandler) finalizedTip(ctx context.Context) (ids.ID, uint64, error) {
	if b.engine != nil {
		if tip, h, set := b.engine.FinalizedLedger(); set {
			// Position is the LOWER of the two heads. The ledger records what a quorum
			// decided; the VM records what this node executed, and finalization can fold
			// the ledger across blocks the VM never ran. A loop positioned at the ledger
			// then believes it stands past blocks it has not executed, skips exactly the
			// band it must fetch, and re-asks forever while reporting full batches landed.
			//
			// Reading the VM here rests on one contract: the ZAP client refreshes
			// LastAccepted on every successful Accept (vms/rpcchainvm/zap/client.go),
			// so the applied head it reports is live rather than a value fixed at
			// Initialize. A VM naming NO block reports nothing — absence is not a
			// reading of zero — so only a VM that names a block lowers the position.
			if id, _, ierr := b.vmLastAccepted(ctx); ierr == nil && id != ids.Empty {
				if applied, aerr := b.appliedHeight(ctx); aerr == nil && applied < h {
					return id, applied, nil
				}
			}
			return tip, h, nil
		}
	}
	return b.vmLastAccepted(ctx)
}

// vmLastAccepted reads the VM's last-accepted id + height. It is the genesis-anchor fallback
// for finalizedTip (used only before the consensus ledger is seeded), NOT a convergence
// signal — a converging node's height MUST come from the finalized ledger, never this cache.
func (b *blockHandler) vmLastAccepted(ctx context.Context) (ids.ID, uint64, error) {
	if b.vm == nil {
		return ids.Empty, 0, nil
	}
	id, err := b.vm.LastAccepted(ctx)
	if err != nil {
		return ids.Empty, 0, err
	}
	if id == ids.Empty {
		return ids.Empty, 0, nil
	}
	blk, err := b.vm.GetBlock(ctx, id)
	if err != nil {
		return id, 0, nil // id known, height unknown — treat as 0 (genesis-ish)
	}
	return id, blk.Height(), nil
}

// Accepted implements chainbootstrap.Chain: reports whether id is on the node's ACCEPTED chain
// (finalized) — NOT merely PRESENT in the block store. This is the loop's caught-up predicate.
// THE FREEZE was a store-vs-acceptance conflation: the prior Has() returned true for a frontier
// GOSSIPED into the store but UNACCEPTED (height ABOVE last-accepted), short-circuiting the loop
// to caught-up at the stale last-accepted and never descending to accept it. A stored-but-
// unaccepted block returns false here, so the loop descends and DRIVES its acceptance.
func (b *blockHandler) Accepted(ctx context.Context, id ids.ID) bool {
	_, ok := b.acceptedHeightCtx(ctx, id)
	return ok
}

// acceptedHeight is the ACCEPTANCE oracle injected into BootstrapPolicy.CaughtUp (as heightOf):
// it returns id's height and TRUE iff the node has ACCEPTED id (id is on the node's finalized
// chain), and (0,false) when id is merely PRESENT IN THE STORE but NOT accepted — the store-vs-
// acceptance distinction the caught-up decision hinges on. CaughtUp uses it to (c) require the
// node to have ACCEPTED every reported tip and (b) read those tips' heights to confirm none is above the
// node's accepted height; a stored-but-unaccepted tip now makes CaughtUp FALSE (the node is behind
// in ACCEPTANCE → it syncs). It NEVER fetches over the network — an unaccepted/unheld tip simply
// makes the node not-caught-up, the safe direction.
func (b *blockHandler) acceptedHeight(id ids.ID) (uint64, bool) {
	return b.acceptedHeightCtx(context.Background(), id)
}

// acceptedHeightCtx is the ctx-honoring core of the acceptance oracle (shared by Accepted and
// acceptedHeight). Authoritative and VM-internals-light: the accepted head id+height come from
// the IN-PROCESS consensus finalized ledger (LastAccepted → finalizedTip — the ADVANCING source,
// never the frozen VM cache), the height-bound (id ABOVE finalizedHeight ⇒ not yet accepted —
// the gossiped-ahead freeze case, regardless of store presence) is the primary anchor, and the
// in-process per-height ledger is the fork-sibling oracle for a block at/below the finalized head.
func (b *blockHandler) acceptedHeightCtx(ctx context.Context, id ids.ID) (uint64, bool) {
	if id == ids.Empty || b.vm == nil {
		return 0, false
	}
	lastID, lastH, err := b.LastAccepted(ctx)
	if err != nil {
		return 0, false
	}
	if id == lastID {
		return lastH, true // the finalized head itself — the authoritative anchor
	}
	blk, err := b.vm.GetBlock(ctx, id)
	if err != nil {
		return 0, false // not even in the store → certainly not accepted
	}
	h := blk.Height()
	if h > lastH {
		// ABOVE our finalized head: a block GOSSIPED into the store ahead of acceptance. THE FREEZE
		// — present, but NOT accepted. NOT caught up; the node must descend and accept it.
		return 0, false
	}
	// h <= lastH and id != lastID: id is at a height we have finalized PAST. It is accepted IFF it
	// is the block our per-height ledger finalized at h. Ask the IN-PROCESS finalized ledger — the
	// same source LastAccepted reads — rather than the VM's height index, because finality is
	// decided in one place: the ledger is what finality means here, while a VM's index answers what
	// that VM has accepted, and the two can disagree while a decision is in flight. The ledger
	// answers authoritatively for every height finalized THIS session.
	if canonical, ok := b.finalizedBlockAtHeight(h); ok {
		if canonical != id {
			return 0, false // a stored sibling/fork at a finalized height — NOT on the accepted chain
		}
		return h, true
	}
	// The per-height ledger does not know h: it is a height BELOW the boot seed (consensus SyncState
	// seeds only the boot height + advances upward; older heights are never re-seeded). We have
	// finalized PAST h and we hold id, so on a BFT-final chain (one finalized block per height) id is
	// on our accepted chain. A node CAN gossip-hold a non-finalized sibling at such a height with NO
	// safety break (it received a losing fork via gossip) — but that costs nothing HERE: this oracle
	// only feeds the caught-up decision, which acts on the ⅔-NAMED frontier, and C1 (⅔-by-stake
	// frontier naming) guarantees a forged/losing sibling is NEVER the named frontier; the descent's
	// content-addressing + the per-height accept guard reject any off-chain block during execution.
	// So treating a held sub-boot-seed block as accepted is the safe over-approximation.
	return h, true
}

// finalizedBlockAtHeight returns the block the IN-PROCESS consensus ledger finalized at height h
// (ok=false when the node has not finalized h this session — a height below the boot seed — or has
// no engine). It is the authoritative fork-sibling oracle that replaces the EVM's dead height
// index; degrading to ok=false simply routes acceptedHeightCtx to its height-bound anchor.
func (b *blockHandler) finalizedBlockAtHeight(h uint64) (ids.ID, bool) {
	if b.engine == nil {
		return ids.Empty, false
	}
	return b.engine.FinalizedBlockAtHeight(h)
}

// AcceptBootstrapBlock implements chainbootstrap.Chain. A quorum chain accepts a
// fetched block only through the same signed-certificate predicate as live catch-up;
// a peer/beacon majority is discovery input, not finality authority. The legacy
// frontier-trust path remains available only when no signed votes are required (the
// sole-validator/dev topology, where no external quorum certificate can exist).
func (b *blockHandler) AcceptBootstrapBlock(ctx context.Context, raw []byte) error {
	if !b.signedVotesRequired {
		return b.engine.AcceptBootstrapBlock(ctx, raw)
	}
	if b.engine == nil || b.vm == nil {
		return errBootstrapCertificateRequired
	}
	blk, err := b.vm.ParseBlock(ctx, raw)
	if err != nil {
		return err
	}
	b.bsMu.Lock()
	cert := b.bsCertificates[blk.ID()]
	delete(b.bsCertificates, blk.ID())
	b.bsMu.Unlock()
	if len(cert) == 0 {
		return errBootstrapCertificateRequired
	}
	return b.engine.AcceptCatchupBlock(ctx, raw, cert)
}

// BootstrapComplete reports whether initial sync has reached the frontier. This is the
// REAL bootstrap-ready signal monitorBootstrap gates the chain on — replacing the
// premature engine.IsBootstrapped() poll that returned true at the local last-accepted
// height (the bug: an empty node declared itself bootstrapped at genesis and never
// synced).
func (b *blockHandler) BootstrapComplete() bool { return b.bootstrapDone.Load() }

// bsFailure records the fail-safe reason initial sync returned (ErrBeaconsUnreachable /
// ErrNoBeaconQuorum / ErrGapTooLarge / ...). Stored behind an atomic pointer so the
// monitorBootstrap goroutine reads it race-free — the precise diagnostic for the health check.
type bsFailure struct{ err error }

// BootstrapFailed reports whether initial sync ended in a fail-SAFE error (eclipse / partition
// / deep gap) rather than reaching the frontier (F5). monitorBootstrap polls this so a fail-safe
// return STOPS the chain promptly — it does not sit in the dead window between Run returning and
// the 5-min no-progress watchdog. Distinct from BootstrapComplete: a chain is ready XOR failed
// XOR still-syncing.
func (b *blockHandler) BootstrapFailed() bool { return b.bootstrapFailed.Load() != nil }

// BootstrapFailure returns the fail-safe reason once BootstrapFailed (else nil) — the precise
// cause (eclipsed/partitioned/too-far-behind) the operator's health check surfaces.
func (b *blockHandler) BootstrapFailure() error {
	if f := b.bootstrapFailed.Load(); f != nil {
		return f.err
	}
	return nil
}

// BootstrapWait reports WHY initial sync is in its bounded connectivity-RETRY wait — a transient
// beacon-quorum-unreachable fail-safe it is re-attempting (the SELF-HEAL path) — and empty when it
// is not waiting. The node is correctly failing safe DOWN (VM in Bootstrapping, serving nothing as
// head) and waiting for the quorum to return; the network cannot make progress without it.
// monitorBootstrap's no-progress watchdog polls this so it does NOT force-STOP a node that is
// deliberately waiting — which, given the K8s probes only poll the always-green
// /v1/health/ops/liveness, would be a permanent brick. It is the discriminator between "stuck on a
// served gap" (a real stall → stop) and "waiting for the quorum" (self-heal → keep waiting).
// Distinct from BootstrapFailed (a terminal/structural fail).
//
// Whether the node is waiting and why it is waiting are one question, so they are one value:
// asking twice admits an answer where the two halves disagree, and the reason is what the
// operator came for anyway.
func (b *blockHandler) BootstrapWait() string {
	if w := b.bootstrapWait.Load(); w != nil {
		return *w
	}
	return ""
}

// BootstrapHeight reports the node's current ACCEPTED height — the PROGRESS signal
// monitorBootstrap uses (H2) to tell a slow-but-advancing sync (reset the stall timer) from a
// genuine no-progress stall (fail). It reads the IN-PROCESS finalized ledger (finalizedTip), the
// SAME advancing source as the convergence oracle — NOT the VM cache, which a fire-and-forget ZAP
// Accept leaves FROZEN: reading the frozen cache here made monitorBootstrap's progress probe see
// zero advance even while the descent finalized block after block, so a deep but healthy sync
// looked like a stall. Best-effort: 0 if unknown.
func (b *blockHandler) BootstrapHeight() uint64 {
	_, h, err := b.finalizedTip(context.Background())
	if err != nil {
		return 0
	}
	return h
}

// deliverBootstrapFrontier routes a frontier reply (TAGGED with the responding beacon)
// to the waiting FrontierTip when the bootstrap loop is driving. Returns true iff
// consumed (the caller must then NOT run the live auto-fetch). The nodeID is what lets
// FrontierTip weight the reply by the responder's stake — the heart of the α-quorum.
// Non-blocking: if the buffered channel is momentarily full the reply is dropped (the
// window/loop re-samples).
func (b *blockHandler) deliverBootstrapFrontier(nodeID ids.NodeID, containerID ids.ID) bool {
	if !b.bsActive.Load() {
		return false
	}
	b.bsMu.Lock()
	ch := b.bsFrontierCh
	b.bsMu.Unlock()
	if ch != nil {
		select {
		case ch <- bsFrontierReply{nodeID: nodeID, tip: containerID}:
		default:
		}
	}
	return true
}

// deliverBootstrapAccepted routes an Accepted reply — a beacon's answer to the second naming
// question — to the waiting Acceptance call. Returns true iff this lane TOOK it.
//
// An answer is taken only if this lane asked THAT peer under THAT request id. Both halves matter:
// the id keeps an answer out of a round it was not part of, and the sender keeps a peer nobody
// asked from occupying a slot in the round's buffer — which would deny it to a beacon whose answer
// the tally needs. Non-blocking, so a full buffer drops the answer rather than holding the message
// goroutine.
func (b *blockHandler) deliverBootstrapAccepted(requestID uint32, nodeID ids.NodeID, accepted []ids.ID) bool {
	if !b.bsActive.Load() {
		return false
	}
	b.bsMu.Lock()
	round, open := b.bsAccepted[requestID]
	b.bsMu.Unlock()
	if !open || !round.asked.Contains(nodeID) {
		return false
	}
	select {
	case round.ch <- bsAcceptedReply{nodeID: nodeID, accepted: accepted}:
		return true
	default:
		return false
	}
}

// deliverBootstrapAncestors routes an Ancestors reply (framed block batch) to the
// waiting Ancestors call when the bootstrap loop is driving. Returns true iff this
// lane actually TOOK the reply — the caller hands anything else to the live path.
//
// bsActive means "initial sync is running", NOT "initial sync owns every Ancestors
// reply on this chain". The live cert catch-up path issues its own requests
// concurrently — that is how a node which is merely behind fetches the block a
// vote or cert referenced. Claiming a reply this lane never asked for strands such
// a node permanently: it fetches the right blocks and applies none of them, logging
// that the catch-up response was consumed by initial sync over full replies it had
// just successfully fetched, while it stays thousands of blocks behind its peers.
// A reply belongs to this lane only if this lane asked for it.
func (b *blockHandler) deliverBootstrapAncestors(nodeID ids.NodeID, requestID uint32, data []byte) bool {
	if !b.bsActive.Load() {
		return false
	}
	b.bsMu.Lock()
	ch := b.bsAncestorCh[requestID]
	asked := b.bsAncestorPeers[requestID]
	b.bsMu.Unlock()
	if ch == nil {
		return false
	}
	// A reply belongs to this fetch only if we asked THIS peer for it.
	if !asked.Contains(nodeID) {
		return false
	}
	select {
	case ch <- decodeContextBlocks(data):
		return true
	default:
		// The waiter exists but is not receiving (it timed out and moved on). The
		// send is non-blocking, so claiming the reply would drop the payload AND
		// deny it to the live path — losing the blocks twice. Let the live path try.
		return false
	}
}

// runBootstrapThenPoll is the chain's startup sync driver (the goroutine buildChain
// launches). It runs INITIAL SYNC to the network frontier with the VM's normal-operation
// transition GATED on that completion (runInitialSync), then — ONLY if the chain actually
// went live — hands off to the live frontier poller (runtime cert-carry catch-up). On
// fail-safe runInitialSync returns false and the poller is NOT started: the chain stays
// not-ready for monitorBootstrap to surface, and the VM stays in Bootstrapping (it never
// serves a stale height). Exits when ctx is done (shutdown). The gating logic lives in
// runInitialSync so it is unit-testable without the blocking poller hand-off.
func (b *blockHandler) runBootstrapThenPoll(ctx context.Context) {
	if b.runInitialSync(ctx) {
		b.runFrontierPoller(ctx)
	}
}

// runInitialSync drives the fetch+execute bootstrap loop to the network frontier and, ON
// SUCCESS, transitions the VM to normal operation (transitionVMReady → vm.Ready), ENDS the
// engine's bootstrap phase (FinishBootstrap — only the α-of-K cert-gate finalizes
// thereafter), and flips bootstrapDone — IN THAT ORDER, so "VM serves / builds" and "engine
// cert-gates finality" go live TOGETHER at the named frontier. Returns true iff the chain
// went live.
//
// THE ORDERING IS THE FIX. Previously buildChain put the VM into normal operation
// UNCONDITIONALLY right after Initialize — at the LOCAL last-accepted height — and ran this
// sync afterward as a detached, non-gating goroutine. A restarted STALE validator therefore
// went live (block building, mempool, validator dispatch via the EVM's
// onNormalOperationsStarted) at its stale height and never converged to the finalized
// frontier. Gating the normal-op transition on reaching the frontier makes "VM live at a
// stale height" structurally impossible: the VM is in Bootstrapping until it has converged.
//
// GO-LIVE includes the CAUGHT-UP path. A tip-holding producer on a mixed-height co-restart names
// no frontier (the responders split below ⅔) yet is at the top of every responder — FrontierTip
// returns its OWN held tip NAMED (BootstrapPolicy.CaughtUp), the loop's Accepted()-shortcut concludes
// caught-up, and bs.Run returns nil → it goes Ready at its own tip. Without that path the producer
// would fail safe DOWN at its own tip (the regression this round fixes), the opposite of the
// stale-go-live bug.
//
// FAIL-SAFE + SELF-HEAL (eclipse / isolation / majority outage). Each bs.Run attempt is internally
// BOUNDED: it WAITS for beacon connectivity (re-sampling every RetryInterval up to ConnectDeadline)
// then returns rather than hanging. A TRANSIENT connectivity fail-safe (ErrBeaconsUnreachable /
// ErrNoBeaconQuorum — the quorum was not reachable this attempt) is RE-ATTEMPTED (bootstrapMaxAttempts
// ≤ 0 ⇒ until the quorum returns or shutdown), with the wait reason published so monitorBootstrap's
// no-progress watchdog treats it as a deliberate WAIT, not a stall: the node stays in Bootstrapping
// (serving nothing as head, NEVER live at the stale height) and CONVERGES the instant the quorum
// returns — the in-process self-heal the K8s probes do NOT provide (they poll the always-green
// /v1/health/ops/liveness, so a fail-safe-DOWN node is never restarted). A STRUCTURAL failure (deep gap
// → state-sync) or an exhausted attempt bound returns false WITHOUT going Ready, bootstrapFailed
// recording the reason so monitorBootstrap surfaces it. The node NEVER false-completes at its stale
// height, and a transient outage NEVER becomes a permanent brick.
func (b *blockHandler) runInitialSync(ctx context.Context) bool {
	if b.engine == nil || b.net == nil || b.msgCreator == nil {
		// Degenerate handler (no transport/engine to drive sync): nothing to converge to.
		// Go live immediately so a single-node / transport-less chain is not pinned
		// unbootstrapped — the same fast-path the no-beacon-set case takes.
		if b.engine != nil {
			b.engine.FinishBootstrap()
		}
		if err := b.transitionVMReady(ctx); err != nil {
			b.logger.Error("degenerate chain: VM SetState(Ready) failed — NOT marking bootstrapped", log.Err(err))
			b.bootstrapFailed.Store(&bsFailure{err: err})
			return false
		}
		b.bootstrapDone.Store(true)
		return true
	}

	// Ancestors bridges a cert-carrying wire format through a source-compatible
	// bootstrap interface by caching proofs under the block ID. Executed blocks
	// consume their entries immediately; frontier probes and abandoned retries can
	// leave entries behind. Keep the cache scoped to this initial-sync run.
	defer func() {
		b.bsMu.Lock()
		clear(b.bsCertificates)
		b.bsMu.Unlock()
	}()

	bs := chainbootstrap.New(chainbootstrap.Config{
		Source: b,
		Chain:  b,
		Log:    b.logger,
		// Bounded beacon-connect WAIT + re-sample pause (zero ⇒ library defaults 3m / 1s).
		// ConnectDeadline is the IN-ATTEMPT retry: bs.Run re-samples the beacon set every
		// RetryInterval up to this deadline, converging the instant the beacons return.
		ConnectDeadline: b.bootstrapConnectDeadline,
		RetryInterval:   b.bootstrapRetryInterval,
		// Optional operator-pinned weak-subjectivity anchor: the α-agreed frontier must
		// descend from this id at this height (defense-in-depth for empty-genesis). Zero
		// ⇒ disabled (the beacon + ⅔-stake quorum is the primary anchor).
		WeakSubjectivityID:     b.wsCheckpointID,
		WeakSubjectivityHeight: b.wsCheckpointHeight,
	})

	// OUTER SELF-HEAL RETRY. A bs.Run attempt that fails because the beacon quorum was UNREACHABLE
	// (eclipse / partition / a majority co-restart still in flight) is RE-ATTEMPTED — the node stays
	// in Bootstrapping (engine alive, VM serving nothing as head, never live at the stale height)
	// and CONVERGES the instant the quorum returns. This is the recovery the K8s probes do NOT
	// provide (they all poll the always-green /v1/health/ops/liveness, so a fail-safe-DOWN node is
	// never restarted). bootstrapMaxAttempts ≤ 0 ⇒ retry until the quorum returns or shutdown; a
	// test pins it to 1 to assert the single-attempt terminal fail-safe. A STRUCTURAL failure (deep
	// gap → state-sync) is NOT retried — a retry cannot fix it; it is surfaced for the operator.
	// ADOPTION FIRST. A node whose gap is deeper than the descent can close has no
	// way home through the descent, however many times it retries — asking for a
	// summary is the other door, and it has to be tried before the walk, because
	// adopting one moves the floor the walk starts from. A chain whose VM cannot
	// sync state, or whose network has nothing to offer, falls straight through to
	// the descent it would have run anyway.
	b.adoptSummary(ctx)

	var lastErr error
	for attempt := 1; ; attempt++ {
		b.bsActive.Store(true)
		err := bs.Run(ctx)
		b.bsActive.Store(false)

		if err == nil {
			// Reached the frontier (or no beacon set / already at / above the tip — caught up): go
			// live. Close the bootstrap phase FIRST, then transition the VM to normal operation.
			// Quorum-chain bootstrap acceptance is already certificate-gated by this adapter;
			// ending the phase also closes the legacy cert-less engine entry point. Ordering
			// them this way
			// (matching the degenerate path above) leaves NO window where the live VM is
			// building/serving while the cert-less accept path is still open. FinishBootstrap is
			// idempotent and void; if the VM transition then fails we fail safe (record the reason,
			// return false). A VM that REFUSES normal-op is a real failure: do NOT mark ready.
			b.bootstrapWait.Store(nil)
			b.engine.FinishBootstrap()
			if verr := b.transitionVMReady(ctx); verr != nil {
				b.logger.Error("chain reached the frontier but VM SetState(Ready) failed — NOT marking bootstrapped", log.Err(verr))
				b.bootstrapFailed.Store(&bsFailure{err: verr})
				return false
			}
			b.bootstrapDone.Store(true)
			b.logger.Info("chain initial sync complete — VM live (normal operation) at the network frontier")
			return true
		}

		if ctx.Err() != nil {
			b.bootstrapWait.Store(nil)
			return false // shutdown — not a bootstrap failure
		}
		lastErr = err

		// Decide whether to RE-ATTEMPT. A transient connectivity fail-safe self-heals when the
		// quorum returns (retry); a structural failure or an exhausted attempt bound does not (break
		// → surface). While we wait between attempts, publish the reason so the monitor's
		// no-progress watchdog treats this as a deliberate quorum WAIT, not a stall — a node failing
		// safe DOWN and waiting must not be force-stopped (that is the brick this avoids).
		boundReached := b.bootstrapMaxAttempts > 0 && attempt >= b.bootstrapMaxAttempts
		if boundReached || !isRetryableBootstrapFailure(err) {
			break
		}
		// The wait is declared here because this is where it is decided. bs.Run fails safe on a
		// bare sentinel, so the counts come from the last round that measured them; without them
		// this says only that a quorum is missing, which reads the same whether five beacons
		// answered or none did.
		wait := "waiting for the beacon quorum"
		if s := b.shortfall.Load(); s != nil {
			wait += ": " + *s
		}
		b.bootstrapWait.Store(&wait)
		if perr := b.pauseBootstrapRetry(ctx); perr != nil {
			b.bootstrapWait.Store(nil)
			return false // shutdown during the inter-attempt backoff
		}
	}

	// Exhausted the attempt bound, or a structural failure (deep gap / disjoint peer). DO NOT
	// transition to normal operation — leaving the VM in Bootstrapping is the fail-safe: it does not
	// serve / build at the stale local height (that was the freeze defect). Record the reason so
	// monitorBootstrap surfaces it (F5).
	b.bootstrapWait.Store(nil)
	b.logger.Warn("chain initial sync did not reach the frontier — VM stays bootstrapping (NOT serving normal-op), failing safe",
		log.Err(lastErr))
	b.bootstrapFailed.Store(&bsFailure{err: lastErr})
	return false
}

// isRetryableBootstrapFailure reports whether a bs.Run fail-safe is a TRANSIENT connectivity
// failure — the beacon quorum was not reachable this attempt — which RECOVERS when the quorum
// returns, so re-attempting bootstrap (the node staying in Bootstrapping meanwhile) self-heals it.
// A STRUCTURAL failure (ErrGapTooLarge needs state-sync; ErrCannotConnect / ErrStalled is a serving
// peer naming a disjoint chain or withholding ancestry) is NOT retried here — a retry cannot fix it;
// it is surfaced (bootstrapFailed → monitorBootstrap) for operator action.
func isRetryableBootstrapFailure(err error) bool {
	return errors.Is(err, chainbootstrap.ErrBeaconsUnreachable) ||
		errors.Is(err, chainbootstrap.ErrNoBeaconQuorum) ||
		// ErrStalled says NO PEER SERVED THE ANCESTRY THIS ROUND. That is a statement
		// about one pass over a sampled set of peers, not about the chain: the peers
		// that could serve may be busy, mid-restart, or simply not the ones this round
		// happened to draw. Treating it as structural ends initial sync for the life of
		// the process, and the node then has only the live catch-up path — which cannot
		// close a gap wider than a served window, so it re-asks forever and the chain
		// never rejoins. The only exit was a restart, which draws a new sample and can
		// stall again on the next unlucky round.
		//
		// Retrying costs one backoff interval and re-samples. Not retrying costs the
		// bootstrap. The attempt bound still terminates it, and a genuinely structural
		// failure — a gap too deep for the window — has its own error and is still not
		// retried, because a retry cannot fix that one.
		errors.Is(err, chainbootstrap.ErrStalled)
}

// pauseBootstrapRetry backs off one RetryInterval between bootstrap re-attempts (never a hot loop),
// or returns ctx.Err() on shutdown. Mirrors the bootstrapper's own pause so the OUTER self-heal
// retry has the same bounded, shutdown-terminable backoff as the inner connect wait.
func (b *blockHandler) pauseBootstrapRetry(ctx context.Context) error {
	d := b.bootstrapRetryInterval
	if d <= 0 {
		d = time.Second
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// transitionVMReady moves the VM to NORMAL OPERATION (vm.Ready) through the gated callback
// buildChain wired from the VM's SetState. It is the SINGLE place the VM goes live, called
// by runInitialSync ONLY after initial sync has reached the named frontier. No-op when no
// SetState VM is wired (degenerate / test handlers — nothing to transition). Bounded by a 30s
// timeout DERIVED FROM the sync/shutdown ctx (not context.Background): a wedged VM transition
// cannot hang the goroutine past 30s, AND a shutdown that cancels ctx cancels the transition
// immediately instead of blocking the chain teardown for up to 30s.
func (b *blockHandler) transitionVMReady(ctx context.Context) error {
	if b.vmReady == nil {
		return nil
	}
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return b.vmReady(sctx)
}

// decodeContextBlocks extracts blocks and their finality certificates (oldest-first) from a framed
// Ancestors payload — the inverse of GetContext's framing, shared by the bootstrap
// fetch path. Each outer [entryLen:4][entry] entry is either a v2 cert-carrying frame
// or a legacy raw block (the entry IS the block and Certificate is empty). Strict: a
// malformed length stops the walk (returns what parsed cleanly so far).
func decodeContextBlocks(data []byte) []bootstrapFetchedBlock {
	var out []bootstrapFetchedBlock
	remaining := data
	for len(remaining) >= 4 {
		entryLen := int(binary.BigEndian.Uint32(remaining[:4]))
		remaining = remaining[4:]
		if entryLen <= 0 || entryLen > len(remaining) {
			break
		}
		entry := remaining[:entryLen]
		remaining = remaining[entryLen:]
		if blockBytes, certBytes, isV2 := decodeCatchupEntry(entry); isV2 {
			out = append(out, bootstrapFetchedBlock{Bytes: blockBytes, Certificate: certBytes})
		} else {
			out = append(out, bootstrapFetchedBlock{Bytes: entry})
		}
	}
	return out
}

// ancestryProgress returns the handler's cross-attempt descent state, allocating on
// first use. The map is the recovery state: the per-attempt budget resets each round
// and these cursors do not, which is what turns a gap wider than one attempt into
// progress rather than repetition.
func (b *blockHandler) ancestryProgress() map[ids.ID]*NamingProgress {
	if b.namingProgress == nil {
		b.namingProgress = make(map[ids.ID]*NamingProgress)
	}
	return b.namingProgress
}
