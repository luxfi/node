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
	// bootstrapFrontierTimeout bounds how long FrontierTip waits for a peer to name its
	// accepted tip before giving up this round (the driver re-samples).
	bootstrapFrontierTimeout = 6 * time.Second
	// bootstrapAncestorsTimeout bounds how long Ancestors waits for a peer to serve a
	// batch (longer than the 10s GetAncestors deadline so a slow-but-honest peer is not
	// abandoned prematurely).
	bootstrapAncestorsTimeout = 12 * time.Second
)

// samplePeerTrackingChain returns a small sample of connected peers that track this
// chain. ok=false when none exist (a single-node / dev chain, or a momentarily
// peerless start) — the caller then treats the node as already at the frontier
// (nothing to sync to).
func (b *blockHandler) samplePeerTrackingChain() (set.Set[ids.NodeID], bool) {
	if b.net == nil {
		return nil, false
	}
	peers := b.net.PeerInfo(nil)
	sample := set.NewSet[ids.NodeID](frontierPollSample)
	for _, p := range peers {
		if p.TrackedChains.Contains(b.chainID) {
			sample.Add(p.ID)
			if sample.Len() >= frontierPollSample {
				break
			}
		}
	}
	return sample, sample.Len() > 0
}

// FrontierTip implements chainbootstrap.BlockSource: ask a sample of peers for their
// accepted tip and return the first reply (the network frontier). ok=false when no
// peer is reachable — the loop treats that as "nothing to sync to" and finishes.
func (b *blockHandler) FrontierTip(ctx context.Context) (ids.ID, bool) {
	if b.net == nil || b.msgCreator == nil {
		return ids.Empty, false
	}
	sample, ok := b.samplePeerTrackingChain()
	if !ok {
		return ids.Empty, false
	}

	ch := make(chan ids.ID, frontierPollSample)
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
	b.net.Send(msg, sample, b.networkID, 0)

	select {
	case tip := <-ch:
		return tip, tip != ids.Empty
	case <-time.After(bootstrapFrontierTimeout):
		return ids.Empty, false
	case <-ctx.Done():
		return ids.Empty, false
	}
}

// Ancestors implements chainbootstrap.BlockSource: fetch up to maxBlocks blocks ending
// at blockID, OLDEST-FIRST, from a peer (wire: GetAncestors -> Ancestors). An empty
// result (no error) means the sampled peer did not serve — the loop re-samples.
func (b *blockHandler) Ancestors(ctx context.Context, blockID ids.ID, maxBlocks int) ([][]byte, error) {
	if b.net == nil || b.msgCreator == nil {
		return nil, nil
	}
	sample, ok := b.samplePeerTrackingChain()
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

// deliverBootstrapFrontier routes a frontier reply to the waiting FrontierTip when the
// bootstrap loop is driving. Returns true iff consumed (the caller must then NOT run
// the live auto-fetch). Non-blocking: an extra reply (several peers under one
// requestID) is dropped.
func (b *blockHandler) deliverBootstrapFrontier(containerID ids.ID) bool {
	if !b.bsActive.Load() {
		return false
	}
	b.bsMu.Lock()
	ch := b.bsFrontierCh
	b.bsMu.Unlock()
	if ch != nil {
		select {
		case ch <- containerID:
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
