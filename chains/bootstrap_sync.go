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

	frontier, err := policy.AcceptsFrontier(ctx, replies)
	switch {
	case err == nil:
		// A configured-beacon quorum named a safe sync anchor (NOT a finality cert — the loop
		// re-executes the descent and re-enters consensus, where ConsensusQuorum alone governs).
		return frontier.ID, chainbootstrap.FrontierNamed
	case errors.Is(err, ErrInsufficientBootstrapResponses):
		// Fewer than MinResponses configured beacons have RESPONDED — not a partition-capture-safe
		// quorum yet. More may connect: WAIT (bounded by the loop's ConnectDeadline, then fail
		// safe). Never false-complete at the stale height; never trust the captured few.
		return ids.Empty, chainbootstrap.FrontierConnecting
	default:
		// ErrNoBootstrapQuorum: enough beacons responded but no ⅔-of-responders agreement on a
		// common committed block this round — a transient bleeding-edge split (the loop's bounded
		// F2 retry converges it) or a genuine partition (fails safe at the bound). NOT caught up.
		return ids.Empty, chainbootstrap.FrontierNoQuorum
	}
}

// bootstrapPolicy builds the BootstrapTrust DECISION object for this round from the configured
// beacon set (the trust anchor — INVARIANT 1: configured/checkpoint/genesis, never peer
// self-report). MinResponses defaults to a MAJORITY of the configured beacons (the largest floor
// that still lets the node recover when a minority of validators is down); the agreement is ⅔ of
// the RESPONDERS; MinFrontierHeight is the node's last-accepted height (the ancestor-tolerant path
// never names a block beneath where the node already is — fail safe, not false-complete). All are
// operator-overridable via the blockHandler fields; zero ⇒ the documented default.
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
		NamingTimeout:      bootstrapNamingTimeout,
		Source:             b,
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
			if len(seen) >= len(connected) {
				return replies // every connected beacon answered — resolve now, do not wait the window
			}
		case <-deadline:
			return replies
		case <-ctx.Done():
			return replies
		}
	}
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
// FAIL-SAFE (eclipse / isolated node). bs.Run is internally BOUNDED: it WAITS for beacon
// connectivity (re-sampling the beacon set every RetryInterval up to ConnectDeadline) — the
// bounded retry that self-heals the instant the beacons return — then returns rather than
// hanging. If the deadline elapses with no ⅔-by-stake beacon quorum reachable (genuine
// eclipse / partition / deep gap), runInitialSync returns false WITHOUT going Ready: the VM
// stays in Bootstrapping (it serves nothing as head), and bootstrapFailed records the reason
// so monitorBootstrap stops the chain and surfaces it. The orchestrator restarts the node,
// which re-syncs and converges (nothing pins it live at the stale height anymore). The node
// NEVER false-completes at its local stale height.
func (b *blockHandler) runInitialSync(ctx context.Context) bool {
	if b.engine == nil || b.net == nil || b.msgCreator == nil {
		// Degenerate handler (no transport/engine to drive sync): nothing to converge to.
		// Go live immediately so a single-node / transport-less chain is not pinned
		// unbootstrapped — the same fast-path the no-beacon-set case takes.
		if b.engine != nil {
			b.engine.FinishBootstrap()
		}
		if err := b.transitionVMReady(); err != nil {
			b.logger.Error("degenerate chain: VM SetState(Ready) failed — NOT marking bootstrapped",
				log.Stringer("chainID", b.chainID), log.Err(err))
			b.bootstrapFailed.Store(&bsFailure{err: err})
			return false
		}
		b.bootstrapDone.Store(true)
		return true
	}

	b.bsActive.Store(true)
	bs := chainbootstrap.New(chainbootstrap.Config{
		Source: b,
		Chain:  b,
		Log:    b.logger,
		// Bounded beacon-connect WAIT + re-sample pause (zero ⇒ library defaults 3m / 1s).
		// ConnectDeadline is the bounded retry: bs.Run re-samples the beacon set every
		// RetryInterval up to this deadline, converging the instant the beacons return.
		ConnectDeadline: b.bootstrapConnectDeadline,
		RetryInterval:   b.bootstrapRetryInterval,
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
			return false // shutdown — not a bootstrap failure
		}
		// Initial sync did not reach the frontier within the bounded deadline (eclipse /
		// partition / deep gap). DO NOT transition to normal operation — leaving the VM in
		// Bootstrapping is the fail-safe: it does not serve / build at the stale local height
		// (that was the freeze defect). Record the reason so monitorBootstrap stops the chain
		// PROMPTLY (F5) and surfaces it; the orchestrator restarts and the node re-syncs. bs.Run
		// already retried beacon connectivity for its full ConnectDeadline, so this is the
		// bounded fail-safe, not a hang.
		b.logger.Warn("chain initial sync did not reach the frontier — VM stays bootstrapping (NOT serving normal-op), failing safe",
			log.Stringer("chainID", b.chainID),
			log.Err(err))
		b.bootstrapFailed.Store(&bsFailure{err: err})
		return false
	}

	// Reached the frontier (or no beacon set / already at the tip): transition the VM to
	// normal operation, THEN end the engine's bootstrap phase and mark ready — so the two
	// go-live transitions happen TOGETHER at the named frontier, never at a stale height. A VM
	// that REFUSES normal-op is a real failure: do NOT mark ready (fail safe).
	if err := b.transitionVMReady(); err != nil {
		b.logger.Error("chain reached the frontier but VM SetState(Ready) failed — NOT marking bootstrapped",
			log.Stringer("chainID", b.chainID), log.Err(err))
		b.bootstrapFailed.Store(&bsFailure{err: err})
		return false
	}
	b.engine.FinishBootstrap()
	b.bootstrapDone.Store(true)
	b.logger.Info("chain initial sync complete — VM live (normal operation) at the network frontier",
		log.Stringer("chainID", b.chainID))
	return true
}

// transitionVMReady moves the VM to NORMAL OPERATION (vm.Ready) through the gated callback
// buildChain wired from the VM's SetState. It is the SINGLE place the VM goes live, called
// by runInitialSync ONLY after initial sync has reached the named frontier. No-op when no
// SetState VM is wired (degenerate / test handlers — nothing to transition). Bounded by a
// 30s timeout (decoupled from the sync ctx) so a wedged VM transition cannot hang the
// goroutine, matching the original buildChain transition budget.
func (b *blockHandler) transitionVMReady() error {
	if b.vmReady == nil {
		return nil
	}
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return b.vmReady(sctx)
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
