// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// bootstrap_sync.go — the node-side wiring for INITIAL SYNC. The blockHandler is both
// the fetch transport (chainbootstrap.BlockSource) and the execute sink
// (chainbootstrap.Chain) the engine/chain/bootstrap loop drives, so an EMPTY or BEHIND
// node converges from its local last-accepted to the network frontier by fetch+execute
// (re-execute each fetched block; no vote, no cert). When the loop reaches the
// frontier the driver ENDS the engine's bootstrap phase (FinishBootstrap — only the
// α-of-K cert-gate finalizes thereafter) and flips bootstrapDone, the REAL ready
// signal monitorBootstrap gates the chain on. Decomplected from the live cert/vote
// path: when bsActive is false every method below is inert and the handler behaves
// exactly as it did before initial sync existed.
package chains

import (
	"bytes"
	"context"
	"encoding/binary"
	"sort"
	"time"

	cblock "github.com/luxfi/consensus/engine/chain/block"
	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/proto/p2p"
)

// The blockHandler IS the bootstrap loop's fetch transport AND execute sink. If it
// ever stops satisfying either, the chain cannot initial-sync — catch it at compile
// time, not in production (the same discipline as the ancestorRequester assertion).
var (
	_ chainbootstrap.BlockSource = (*blockHandler)(nil)
	_ chainbootstrap.Chain       = (*blockHandler)(nil)
)

const (
	// bootstrapFrontierWindow bounds how long FrontierTip collects weighted beacon
	// replies before tallying the ⅔-by-stake quorum. A beacon answering later just
	// misses this round (the driver re-samples) — it is not abandoned.
	bootstrapFrontierWindow = 3 * time.Second
	// bootstrapAncestorsTimeout bounds how long Ancestors waits for a peer to serve a
	// batch (longer than the 10s GetAncestors deadline so a slow-but-honest peer is not
	// abandoned prematurely).
	bootstrapAncestorsTimeout = 12 * time.Second
	// bootstrapAncestorSample is how many beacons one Ancestors round asks. The sample
	// ROTATES (bsRotor) so no single beacon monopolizes the descent or can repeatedly
	// stall it (M1). Content addressing in the loop makes the fetched ancestry safe
	// regardless of WHICH beacon serves it.
	bootstrapAncestorSample = 4
	// bootstrapMinAgreeingBeacons floors the DISTINCT beacons that must name the same
	// tip, so a single (even >⅔-stake) validator cannot alone determine the frontier.
	// Capped at the beacon-set size for tiny networks.
	bootstrapMinAgreeingBeacons = 2
	// bootstrapNamingWindow bounds the ancestry the ANCESTOR-TOLERANT frontier tally fetches
	// per candidate anchor to resolve the bleeding-edge skew between honest beacons. On a live
	// chain that skew is tiny (the canary was ONE block: 2 producers at N, 1 at N+1), so a
	// single 256-block window covers it with enormous margin. A ⅔-common height more than this
	// far below the highest tip is not a healthy bleeding-edge split — the tally names nothing
	// (the loop then retries / fails safe), never a wrong block. Matches the consensus descent's
	// per-round fetch size.
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
	bootstrapNamingTimeout = 3 * time.Second
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
// b.chainID. This is the mainnet-canary fix: a peer advertises the NETS it tracks in its
// handshake (network/peer/peer.go adds constants.PrimaryNetworkID plus every tracked L1 net
// ID to peer.Info.TrackedChains) — it NEVER advertises an individual native chain ID like
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
	for _, p := range b.net.PeerInfo(beaconIDs) {
		if _, isBeacon := weights[p.ID]; isBeacon && p.TrackedChains.Contains(b.networkID) {
			connected = append(connected, p.ID)
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
	start := int(b.bsRotor.Add(1)-1) % len(connected)
	sample := set.NewSet[ids.NodeID](bootstrapAncestorSample)
	for i := 0; i < len(connected) && sample.Len() < bootstrapAncestorSample; i++ {
		sample.Add(connected[(start+i)%len(connected)])
	}
	return sample, sample.Len() > 0
}

// FrontierTip implements chainbootstrap.BlockSource. It returns a FrontierStatus that
// decomplects the THREE reasons a tip may not be named — the fix for the mainnet canary
// where a freshly-booted STALE node, asking the frontier BEFORE any beacon had connected,
// concluded "caught up" at its local stale height (a single ok=false meant both "no beacon
// quorum reachable yet" and "nothing ahead — done"):
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
	weights, total, haveBeacons := b.beaconWeights()
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
		return ids.Empty, chainbootstrap.FrontierConnecting
	}

	required := bootstrapMinAgreeingBeacons
	if n := len(weights); n < required {
		required = n
	}
	floor := consensusconfig.TwoThirdsStakeFloor(total)

	// DECOMPLECT the canary boot race from a real eclipse: is enough beacon stake CONNECTED
	// that a ⅔-by-stake quorum is even POSSIBLE right now? The most stake any single tip
	// could gather is the connected stake; if that cannot clear the floor (or too few
	// distinct beacons are up), connectivity is still coming up — report Connecting so the
	// loop WAITS rather than false-completing at the local stale height. This is consistent
	// with the named-quorum check below (agree > floor): if even unanimous connected beacons
	// could not clear it, we have not yet connected enough to ask.
	connected := b.connectedBeacons(weights)
	var connectedStake uint64
	for _, id := range connected {
		connectedStake += weights[id]
	}
	if connectedStake <= floor || len(connected) < required {
		return ids.Empty, chainbootstrap.FrontierConnecting
	}

	// Enough beacon stake is connected to ASK — query + tally the ⅔-by-stake quorum.
	if tip, ok := b.queryFrontierQuorum(ctx, connected, weights, floor, required); ok {
		return tip, chainbootstrap.FrontierNamed
	}
	// Connected enough to ask, but no ⅔-by-stake agreement on a single tip: a genuine
	// eclipse / partition. Fail safe — the loop must NOT conclude caught-up at the stale tip.
	return ids.Empty, chainbootstrap.FrontierNoQuorum
}

// queryFrontierQuorum sends GetAcceptedFrontier to the connected beacons and tallies their
// replies WEIGHTED by stake, naming the highest block a ⅔-by-stake supermajority of the
// beacons agree on — where AGREEMENT is "this block is in my accepted chain" (the block is the
// beacon's tip OR an ancestor of it), NOT "this block is my exact tip".
//
// THE EXACT-TIP TALLY (the pre-fix behavior) required ⅔-by-stake to report the IDENTICAL tip
// id. On a LIVE chain that can never be guaranteed: validators are legitimately ±1 block apart
// at the bleeding edge, so the freshest tip splits and no single id draws ⅔. The mainnet canary
// (luxd-2) hit exactly this — 2 producers had accepted block N, the third's un-finalized
// pending block was N+1, neither tip alone cleared ⅔ — and a healthy stale node failed safe
// instead of converging. The chain was idle, so the split was STABLE and never resolved.
//
// ANCESTOR TOLERANCE fixes it without weakening C1: a beacon reporting tip N+1 also vouches for
// N (its ancestor), so N draws ALL THREE producers' stake → clears ⅔ → is named; N+1 draws only
// the lone producer → is NOT named. The node syncs to N (the ⅔-agreed committed height) and
// live consensus catch-up handles N+1. ONLY the agreement RELATION changed; the named frontier
// is still backed by > ⅔ of the TOTAL real staked-beacon weight with ≥ required distinct voters,
// and the synced chain still descends to it via the consensus content-addressed descent. A
// forged or sub-⅔ tip still names nothing (see nameAncestorTolerant for the C1 argument).
//
// The EXACT-⅔ fast path is preserved: when one tip does draw the supermajority outright (the
// whole network already agrees) it is named immediately with NO ancestry fetch. ok=false when
// not even the ancestor-tolerant ⅔ quorum forms within the window. ONE reply per beacon counts.
func (b *blockHandler) queryFrontierQuorum(ctx context.Context, connected []ids.NodeID, weights map[ids.NodeID]uint64, floor uint64, required int) (ids.ID, bool) {
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
		return ids.Empty, false
	}
	sample := set.NewSet[ids.NodeID](len(connected))
	sample.Add(connected...)
	b.net.Send(msg, sample, b.networkID, 0)

	agree := make(map[ids.ID]uint64)
	voters := make(map[ids.ID]map[ids.NodeID]struct{})
	seen := make(map[ids.NodeID]struct{})
	deadline := time.After(bootstrapFrontierWindow)
	for {
		select {
		case rep := <-ch:
			w, isBeacon := weights[rep.nodeID]
			if !isBeacon || rep.tip == ids.Empty {
				continue // only a beacon's non-empty reply counts
			}
			if _, dup := seen[rep.nodeID]; dup {
				continue // one vote per beacon
			}
			seen[rep.nodeID] = struct{}{}
			agree[rep.tip] += w
			if voters[rep.tip] == nil {
				voters[rep.tip] = make(map[ids.NodeID]struct{})
			}
			voters[rep.tip][rep.nodeID] = struct{}{}
			if agree[rep.tip] > floor && len(voters[rep.tip]) >= required {
				// EXACT-⅔ fast path: one tip drew the supermajority outright (the whole network
				// agrees on the same tip). Name it with NO ancestry fetch — the common case.
				return rep.tip, true
			}
			if len(seen) >= len(connected) {
				// Heard from EVERY connected beacon and no single tip drew an exact ⅔. There is no
				// reason to wait out the rest of the window — resolve the ancestor-tolerant ⅔ now.
				// (The window remains the bound for when some beacons are slow/silent.) This keeps a
				// live-frontier split converging in one round instead of stalling a full window.
				return b.nameAncestorTolerant(ctx, agree, voters, floor, required)
			}
		case <-deadline:
			// No single tip drew an EXACT ⅔ supermajority within the window. On a live chain this
			// is the normal bleeding-edge case — honest beacons legitimately ±1 block apart (the
			// canary: 2 producers at N, 1 at N+1). A beacon reporting tip T also has every ANCESTOR
			// of T in its accepted chain, so resolve the highest COMMON committed block the ⅔-stake
			// supermajority shares. A genuine eclipse/partition (no ⅔-backed common ancestor) still
			// returns ok=false here → FrontierNoQuorum → the loop fails safe.
			return b.nameAncestorTolerant(ctx, agree, voters, floor, required)
		case <-ctx.Done():
			return ids.Empty, false
		}
	}
}

// nameAncestorTolerant resolves the COMMON COMMITTED FRONTIER when no single tip drew an EXACT
// ⅔-by-stake supermajority — the live-chain bleeding-edge case the mainnet canary hit (luxd-2:
// 2 producers had accepted block N, the third producer's un-finalized pending block was N+1, so
// neither tip alone cleared ⅔ and the exact-match tally returned NoQuorum forever, even though
// all three producers AGREE on N as the canonical committed height).
//
// A beacon reporting tip T vouches not only for T but for every ANCESTOR of T (its accepted
// chain contains them). So the named frontier is the HIGHEST block B such that beacons holding
// > floor (⅔ of the TOTAL beacon stake) have B in their accepted chain — B == their tip OR B is
// an ancestor of their tip — with ≥ required distinct voters. The node then syncs to B and live
// consensus catch-up handles anything above it.
//
// Ancestry is learned by CONTENT, reusing the same parent-link descent the sync loop trusts:
// for each candidate tip (most stake first, so the honest, well-supported tips are covered),
// fetch its ancestry, parse it into a parent-linked chain, and cumulatively tally — walking the
// chain from the tip DOWNWARD, summing the stake of beacons whose own tip sits at or above the
// current block ON THIS chain. The FIRST (highest) block clearing the floor is that anchor's
// candidate; the highest across all anchors is the named frontier.
//
// C1 (a forged chain finalizes ZERO) is preserved EXACTLY. A credit is only ever made along a
// real parent-linked chain: a block's parent id is fixed by its content, so a peer cannot fake
// the linkage BELOW an honestly-reported tip — crediting block B with a beacon's stake is
// truthful precisely because that beacon's tip lies on B's real descendant chain. A block is
// named only with > ⅔ of the total staked-beacon weight behind it. A forged or minority tip on
// a side-fork shares only the honest prefix, so it can never raise the named block above the
// genuine ⅔-common height; a sub-⅔ set still names nothing. Under ⅔-honest finality two
// honestly-reported tips are always on one chain (honest validators never finalize conflicting
// blocks), so the only ids drawing honest credit are real canonical blocks. The agreement
// RELATION moved from "exact tip" to "in the accepted chain"; the ⅔-by-stake-of-the-real-set
// requirement did not.
func (b *blockHandler) nameAncestorTolerant(ctx context.Context, stakeOnTip map[ids.ID]uint64, votersOf map[ids.ID]map[ids.NodeID]struct{}, floor uint64, required int) (ids.ID, bool) {
	// TOTAL-bound all the anchor ancestry fetches so a partition that answers the frontier but
	// withholds ancestry cannot hang FrontierTip — the loop's bounded retry handles it instead.
	ctx, cancel := context.WithTimeout(ctx, bootstrapNamingTimeout)
	defer cancel()

	// Candidate anchors = the distinct reported tips, MOST STAKE FIRST. Honest beacons hold ⅔ of
	// the stake, so even when split their tips sort ahead of any low-stake forged outliers (which
	// the anchor cap then excludes). Stake order only minimizes fetches — safety comes from the
	// per-anchor content verification and the ⅔ re-tally, not from which anchor is tried first.
	anchors := make([]ids.ID, 0, len(stakeOnTip))
	for tip := range stakeOnTip {
		anchors = append(anchors, tip)
	}
	sort.Slice(anchors, func(i, j int) bool {
		if stakeOnTip[anchors[i]] != stakeOnTip[anchors[j]] {
			return stakeOnTip[anchors[i]] > stakeOnTip[anchors[j]]
		}
		return bytes.Compare(anchors[i][:], anchors[j][:]) < 0 // stable tiebreak
	})

	covered := make(map[ids.ID]struct{}) // ids already walked on a fetched chain — skip re-anchoring
	var bestID ids.ID
	var bestHeight, bestStake uint64
	found := false
	for i, anchor := range anchors {
		if i >= maxNamingAnchors {
			break // bound the fetches — honest tips cluster; a forged swarm cannot exhaust this
		}
		if _, done := covered[anchor]; done {
			continue // this tip already lies on a previously-fetched (higher) chain
		}
		id, h, st, ok := b.tallyAnchorChain(ctx, anchor, stakeOnTip, votersOf, floor, required, covered)
		if !ok {
			continue
		}
		if !found || h > bestHeight || (h == bestHeight && st > bestStake) {
			bestID, bestHeight, bestStake, found = id, h, st, true
		}
	}
	return bestID, found
}

// tallyAnchorChain fetches `anchor`'s ancestry (ONE bounded window — the bleeding-edge skew
// between honest beacons is small), rebuilds the parent-linked chain by CONTENT, and walks it
// from the anchor DOWNWARD accumulating beacon stake + distinct voters. It returns the HIGHEST
// block whose cumulative stake exceeds `floor` with ≥ `required` distinct voters (the highest
// ⅔-common block ON THIS chain), and records every block id it walks into `covered` so the
// caller can skip re-anchoring a tip already on this chain. ok=false if the anchor's ancestry
// cannot be fetched/parsed, or no block on it clears the floor within the window.
//
// The cumulative is exact: walking from the anchor down, at each block we add the stake of
// beacons whose TIP is that block; a beacon higher on the chain was already counted when the
// walk passed its tip. So the running total at block B equals the stake of every beacon whose
// tip lies on this chain at height ≥ B — i.e. the stake that has B in its accepted chain.
func (b *blockHandler) tallyAnchorChain(ctx context.Context, anchor ids.ID, stakeOnTip map[ids.ID]uint64, votersOf map[ids.ID]map[ids.NodeID]struct{}, floor uint64, required int, covered map[ids.ID]struct{}) (ids.ID, uint64, uint64, bool) {
	batch, err := b.Ancestors(ctx, anchor, bootstrapNamingWindow)
	if err != nil || len(batch) == 0 {
		return ids.Empty, 0, 0, false // peer did not serve this anchor's ancestry — skip it
	}
	type linked struct {
		height uint64
		parent ids.ID
	}
	index := make(map[ids.ID]linked, len(batch))
	for _, raw := range batch {
		blk, perr := b.vm.ParseBlock(ctx, raw)
		if perr != nil {
			return ids.Empty, 0, 0, false // a malformed served block — do not trust this anchor's chain
		}
		index[blk.ID()] = linked{height: blk.Height(), parent: blk.ParentID()}
	}

	var cumStake uint64
	cumVoters := make(map[ids.NodeID]struct{})
	cur := anchor
	for {
		n, ok := index[cur]
		if !ok {
			break // the served batch does not extend the content-addressed chain further down
		}
		covered[cur] = struct{}{}
		cumStake += stakeOnTip[cur]
		for v := range votersOf[cur] {
			cumVoters[v] = struct{}{}
		}
		if cumStake > floor && len(cumVoters) >= required {
			return cur, n.height, cumStake, true // highest crossing (we walk top-down)
		}
		if n.parent == ids.Empty {
			break
		}
		cur = n.parent
	}
	return ids.Empty, 0, 0, false
}

// Ancestors implements chainbootstrap.BlockSource: fetch up to maxBlocks blocks ending
// at blockID, OLDEST-FIRST, from a ROTATED sample of beacons (wire: GetAncestors ->
// Ancestors). An empty result (no error) means the sampled beacon did not serve — the
// loop re-samples. The fetched ancestry is made safe by the loop's content-addressed
// descent (off-path blocks ignored), not by trusting the serving peer.
func (b *blockHandler) Ancestors(ctx context.Context, blockID ids.ID, maxBlocks int) ([][]byte, error) {
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

	ch := make(chan [][]byte, 1)
	b.bsMu.Lock()
	b.bsAncestorCh[requestID] = ch
	b.bsMu.Unlock()
	defer func() {
		b.bsMu.Lock()
		delete(b.bsAncestorCh, requestID)
		b.bsMu.Unlock()
	}()

	msg, err := b.msgCreator.GetAncestors(b.chainID, requestID, 10*time.Second, blockID, p2p.EngineType_ENGINE_TYPE_CHAIN)
	if err != nil {
		return nil, err
	}
	b.net.Send(msg, sample, b.networkID, 0)

	select {
	case blocks := <-ch:
		return blocks, nil
	case <-time.After(bootstrapAncestorsTimeout):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ParseBlock implements chainbootstrap.Chain: decode block bytes through the SAME
// builder the engine parses through (identity-preserving), so the loop reads each
// block's height + parent for ordering and the descent.
func (b *blockHandler) ParseBlock(ctx context.Context, raw []byte) (cblock.Block, error) {
	return b.vm.ParseBlock(ctx, raw)
}

// LastAccepted implements chainbootstrap.Chain: the local last-accepted id + height.
func (b *blockHandler) LastAccepted(ctx context.Context) (ids.ID, uint64, error) {
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

// Has implements chainbootstrap.Chain: whether the node already holds block id (so the
// loop can detect it has reached the frontier).
func (b *blockHandler) Has(ctx context.Context, id ids.ID) bool {
	if id == ids.Empty || b.vm == nil {
		return false
	}
	_, err := b.vm.GetBlock(ctx, id)
	return err == nil
}

// AcceptBootstrapBlock implements chainbootstrap.Chain: re-execute + finalize a fetched
// block on frontier-trust via the engine's bootstrap accept authority.
func (b *blockHandler) AcceptBootstrapBlock(ctx context.Context, raw []byte) error {
	return b.engine.AcceptBootstrapBlock(ctx, raw)
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

// BootstrapHeight reports the node's current locally-accepted height — the PROGRESS
// signal monitorBootstrap uses (H2) to tell a slow-but-advancing sync (reset the stall
// timer) from a genuine no-progress stall (fail). Best-effort: 0 if unknown.
func (b *blockHandler) BootstrapHeight() uint64 {
	if b.vm == nil {
		return 0
	}
	id, err := b.vm.LastAccepted(context.Background())
	if err != nil || id == ids.Empty {
		return 0
	}
	blk, err := b.vm.GetBlock(context.Background(), id)
	if err != nil {
		return 0
	}
	return blk.Height()
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

// deliverBootstrapAncestors routes an Ancestors reply (framed block batch) to the
// waiting Ancestors call when the bootstrap loop is driving. Returns true iff consumed.
func (b *blockHandler) deliverBootstrapAncestors(requestID uint32, data []byte) bool {
	if !b.bsActive.Load() {
		return false
	}
	raw := decodeContextBlocks(data)
	b.bsMu.Lock()
	ch := b.bsAncestorCh[requestID]
	b.bsMu.Unlock()
	if ch != nil {
		select {
		case ch <- raw:
		default:
		}
	}
	return true
}

// runBootstrapThenPoll is the chain's startup sync driver. It (1) runs the
// fetch+execute bootstrap loop to converge to the network frontier, (2) on success
// ENDS the engine's bootstrap phase (FinishBootstrap — only the α-of-K cert-gate
// finalizes thereafter) and flips bootstrapDone (so monitorBootstrap marks the chain
// ready), then (3) hands off to the live frontier poller (runtime catch-up via the
// cert path). On bootstrap failure it leaves bootstrapDone false so monitorBootstrap
// surfaces a real failure (timeout) rather than masking it. Node-lifetime goroutine
// (started in buildChain); exits when ctx is done (shutdown).
func (b *blockHandler) runBootstrapThenPoll(ctx context.Context) {
	if b.engine == nil || b.net == nil || b.msgCreator == nil {
		// No transport/engine to drive sync — nothing to bootstrap; treat as ready so a
		// degenerate handler does not pin the chain unbootstrapped.
		if b.engine != nil {
			b.engine.FinishBootstrap()
		}
		b.bootstrapDone.Store(true)
		return
	}

	b.bsActive.Store(true)
	bs := chainbootstrap.New(chainbootstrap.Config{
		Source: b,
		Chain:  b,
		Log:    b.logger,
		// Optional operator-pinned weak-subjectivity anchor: the α-agreed frontier must
		// descend from this id at this height (defense-in-depth for empty-genesis). Zero
		// ⇒ disabled (the beacon + ⅔-stake quorum is the primary anchor).
		WeakSubjectivityID:     b.wsCheckpointID,
		WeakSubjectivityHeight: b.wsCheckpointHeight,
	})
	err := bs.Run(ctx)
	b.bsActive.Store(false)

	if err != nil {
		if ctx.Err() != nil {
			return // shutdown — not a bootstrap failure
		}
		// Initial sync did not complete (eclipse / partition / gap-too-large). Do NOT mark the
		// chain ready. Record the fail-safe reason so monitorBootstrap surfaces it PROMPTLY (F5)
		// — stopping the chain the moment Run returns instead of leaving it in the dead window
		// until the 5-min no-progress watchdog. The operator state-syncs (deep gap) or fixes
		// peering, then restarts.
		b.logger.Warn("chain initial sync did not complete — chain NOT marked bootstrapped",
			log.Stringer("chainID", b.chainID),
			log.Err(err))
		b.bootstrapFailed.Store(&bsFailure{err: err})
		return
	}

	// Reached the frontier: end the bootstrap phase (fail-close the bootstrap accept
	// authority) and mark the chain ready, THEN run the live frontier poller.
	b.engine.FinishBootstrap()
	b.bootstrapDone.Store(true)
	b.logger.Info("chain initial sync complete — entering live consensus",
		log.Stringer("chainID", b.chainID))
	b.runFrontierPoller(ctx)
}

// decodeContextBlocks extracts the raw block bytes (oldest-first) from a framed
// Ancestors payload — the inverse of GetContext's framing, shared by the bootstrap
// fetch path. Each outer [entryLen:4][entry] entry is either a v2 cert-carrying frame
// (we take just the block) or a legacy raw block (the entry IS the block). Strict: a
// malformed length stops the walk (returns what parsed cleanly so far).
func decodeContextBlocks(data []byte) [][]byte {
	var out [][]byte
	remaining := data
	for len(remaining) >= 4 {
		entryLen := int(binary.BigEndian.Uint32(remaining[:4]))
		remaining = remaining[4:]
		if entryLen <= 0 || entryLen > len(remaining) {
			break
		}
		entry := remaining[:entryLen]
		remaining = remaining[entryLen:]
		if blockBytes, _, isV2 := decodeCatchupEntry(entry); isV2 {
			out = append(out, blockBytes)
		} else {
			out = append(out, entry)
		}
	}
	return out
}
