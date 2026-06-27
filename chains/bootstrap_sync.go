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

// connectedBeaconsTrackingChain intersects the beacon set with the peers actually
// CONNECTED and tracking this chain. Only these beacons' frontier replies count toward
// the quorum, but the FULL beacon stake (total) stays the quorum DENOMINATOR — so a node
// must hear a ⅔-stake supermajority. An eclipse that reaches < ⅔ of beacon stake fails
// CLOSED (it cannot name a frontier) instead of syncing an attacker's chain.
func (b *blockHandler) connectedBeaconsTrackingChain() (connected []ids.NodeID, weights map[ids.NodeID]uint64, total uint64, ok bool) {
	weights, total, ok = b.beaconWeights()
	if !ok || b.net == nil {
		return nil, nil, 0, false
	}
	beaconIDs := make([]ids.NodeID, 0, len(weights))
	for id := range weights {
		beaconIDs = append(beaconIDs, id)
	}
	for _, p := range b.net.PeerInfo(beaconIDs) {
		if _, isBeacon := weights[p.ID]; isBeacon && p.TrackedChains.Contains(b.chainID) {
			connected = append(connected, p.ID)
		}
	}
	if len(connected) == 0 {
		return nil, weights, total, false
	}
	return connected, weights, total, true
}

// sampleAncestorBeacons returns a small ROTATED sample of connected beacons to fetch
// ancestry from (M1 — bsRotor advances each call so the sample walks the beacon set and
// no single beacon monopolizes the descent). Safety is independent of WHICH beacon
// serves: the loop's content-addressed descent rejects any block off the agreed
// frontier's parent chain, so a withholding/forging beacon costs only a re-sample.
func (b *blockHandler) sampleAncestorBeacons() (set.Set[ids.NodeID], bool) {
	connected, _, _, ok := b.connectedBeaconsTrackingChain()
	if !ok {
		return nil, false
	}
	start := int(b.bsRotor.Add(1)-1) % len(connected)
	sample := set.NewSet[ids.NodeID](bootstrapAncestorSample)
	for i := 0; i < len(connected) && sample.Len() < bootstrapAncestorSample; i++ {
		sample.Add(connected[(start+i)%len(connected)])
	}
	return sample, sample.Len() > 0
}

// FrontierTip implements chainbootstrap.BlockSource. It names the network frontier ONLY
// when a ⅔-BY-STAKE SUPERMAJORITY of the configured beacons agree on the SAME accepted
// tip — the C1 forged-chain gate. It queries every connected beacon, tallies their
// replies weighted by stake, and returns a tip only once its agreeing stake clears the
// shared live floor (config.TwoThirdsStakeFloor over the FULL beacon stake) with at
// least the minimum distinct beacons. ok=false (fail-closed) when no such quorum forms —
// a non-beacon peer, a single peer, or any sub-⅔-stake set can NEVER name the frontier,
// so a forged chain cannot be the thing an empty/behind node syncs to.
func (b *blockHandler) FrontierTip(ctx context.Context) (ids.ID, bool) {
	if b.net == nil || b.msgCreator == nil {
		return ids.Empty, false
	}
	connected, weights, total, ok := b.connectedBeaconsTrackingChain()
	if !ok {
		// No reachable beacon quorum — fail closed (re-sampled by the loop). Once ⅔ of
		// beacon stake is reachable and agrees, the quorum forms.
		return ids.Empty, false
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
		return ids.Empty, false
	}
	sample := set.NewSet[ids.NodeID](len(connected))
	sample.Add(connected...)
	b.net.Send(msg, sample, b.networkID, 0)

	// Tally weighted replies until a tip clears the ⅔-by-stake floor with ≥ the minimum
	// distinct beacons, or the window closes. ONE reply per beacon counts.
	required := bootstrapMinAgreeingBeacons
	if n := len(weights); n < required {
		required = n
	}
	floor := consensusconfig.TwoThirdsStakeFloor(total)
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
				return rep.tip, true // ⅔-by-stake supermajority of beacons named this tip
			}
		case <-deadline:
			// Window closed: return the best tip that cleared the floor, else no quorum.
			var best ids.ID
			var bestW uint64
			for tip, w := range agree {
				if w > floor && len(voters[tip]) >= required && w > bestW {
					best, bestW = tip, w
				}
			}
			return best, bestW > 0
		case <-ctx.Done():
			return ids.Empty, false
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
		// Initial sync did not complete (stalled / gap-too-large). Do NOT mark the chain
		// ready — monitorBootstrap times out and surfaces the failure. The operator
		// state-syncs (deep gap) or fixes peering, then restarts.
		b.logger.Warn("chain initial sync did not complete — chain NOT marked bootstrapped",
			log.Stringer("chainID", b.chainID),
			log.Err(err))
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
