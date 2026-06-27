// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// bootstrap_sync_test.go — node-side proof that the blockHandler's bootstrap wiring
// (BlockSource transport adapter + Chain execute sink) converges a node over the real
// GetAcceptedFrontier/GetAncestors message path, AND — the C1 forged-chain gate — that it
// names the frontier ONLY from a ⅔-by-stake quorum of the configured BEACONS. A
// non-beacon peer (or any sub-⅔-stake set) can never name the frontier, so an
// empty/behind node can only ever sync the canonical (beacon-agreed) chain. The
// fetch+execute and content-addressed-descent ALGORITHMS are proven in consensus
// (engine/chain/bootstrap); here we prove the node TRANSPORT + the frontier QUORUM.
package chains

import (
	"context"
	"encoding/binary"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	consensuschain "github.com/luxfi/consensus/engine/chain"
	cblock "github.com/luxfi/consensus/engine/chain/block"
	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/proto/p2p"
	"github.com/luxfi/validators"
)

// ----- mock block + VM ------------------------------------------------------

type bsTestBlock struct {
	id, parent ids.ID
	height     uint64
	bytes      []byte
	valid      bool
	accepts    int
}

func (b *bsTestBlock) ID() ids.ID           { return b.id }
func (b *bsTestBlock) Parent() ids.ID       { return b.parent }
func (b *bsTestBlock) ParentID() ids.ID     { return b.parent }
func (b *bsTestBlock) Height() uint64       { return b.height }
func (b *bsTestBlock) Timestamp() time.Time { return time.Unix(int64(b.height), 0) }
func (b *bsTestBlock) Status() uint8        { return 0 }
func (b *bsTestBlock) Verify(context.Context) error {
	if !b.valid {
		return errBSInvalid
	}
	return nil
}
func (b *bsTestBlock) Accept(context.Context) error { b.accepts++; return nil }
func (b *bsTestBlock) Reject(context.Context) error { return nil }
func (b *bsTestBlock) Bytes() []byte                { return b.bytes }

type bsErr string

func (e bsErr) Error() string { return string(e) }

const (
	errBSInvalid   = bsErr("invalid block")
	errBSNoBuild   = bsErr("no build")
	errBSNotStored = bsErr("not accepted")
	errBSUnknown   = bsErr("unknown bytes")
)

// bsTestVM models a node VM: it can PARSE any block on the chain (the wire codec is
// deterministic), but only GETS blocks it has ACCEPTED (stored). SetPreference marks a
// block stored — the engine calls it right after Accept, so Has/LastAccepted advance
// exactly as the node syncs.
type bsTestVM struct {
	all      map[ids.ID]*bsTestBlock
	byBytes  map[string]*bsTestBlock
	accepted map[ids.ID]bool
	lastAcc  ids.ID
}

func newBSVM(chain []*bsTestBlock) *bsTestVM {
	vm := &bsTestVM{
		all:      map[ids.ID]*bsTestBlock{},
		byBytes:  map[string]*bsTestBlock{},
		accepted: map[ids.ID]bool{},
	}
	for _, b := range chain {
		vm.all[b.id] = b
		vm.byBytes[string(b.bytes)] = b
	}
	// Empty node: only genesis is accepted/stored.
	vm.accepted[chain[0].id] = true
	vm.lastAcc = chain[0].id
	return vm
}

func (m *bsTestVM) BuildBlock(context.Context) (cblock.Block, error) { return nil, errBSNoBuild }
func (m *bsTestVM) GetBlock(_ context.Context, id ids.ID) (cblock.Block, error) {
	if m.accepted[id] {
		return m.all[id], nil
	}
	return nil, errBSNotStored
}
func (m *bsTestVM) ParseBlock(_ context.Context, b []byte) (cblock.Block, error) {
	if blk, ok := m.byBytes[string(b)]; ok {
		return blk, nil
	}
	return nil, errBSUnknown
}
func (m *bsTestVM) LastAccepted(context.Context) (ids.ID, error) { return m.lastAcc, nil }
func (m *bsTestVM) SetPreference(_ context.Context, id ids.ID) error {
	m.accepted[id] = true
	m.lastAcc = id
	return nil
}

// ----- mock outbound message + builder --------------------------------------

type bsOutMsg struct {
	op        string
	blockID   ids.ID
	requestID uint32
}

func (*bsOutMsg) BypassThrottling() bool     { return true }
func (*bsOutMsg) Op() message.Op             { return 0 }
func (*bsOutMsg) Bytes() []byte              { return nil }
func (*bsOutMsg) BytesSavedCompression() int { return 0 }

type bsMsgBuilder struct{ message.OutboundMsgBuilder }

func (bsMsgBuilder) GetAcceptedFrontier(_ ids.ID, requestID uint32, _ time.Duration) (message.OutboundMessage, error) {
	return &bsOutMsg{op: "frontier", requestID: requestID}, nil
}
func (bsMsgBuilder) GetAncestors(_ ids.ID, requestID uint32, _ time.Duration, blockID ids.ID, _ p2p.EngineType) (message.OutboundMessage, error) {
	return &bsOutMsg{op: "ancestors", blockID: blockID, requestID: requestID}, nil
}

// ----- beacon-aware mock peer net -------------------------------------------

// bsBeaconNet models a network of BEACONS (each with stake) that hold the chain, plus an
// optional MALICIOUS non-beacon peer that reports/serves a forged chain. The bootstrap
// frontier quorum must name the tip ONLY from the connected beacons, ignoring the
// malicious peer entirely. `connected` is the subset of beacons currently reachable (so a
// test can model "beacons offline, only the attacker is up").
type bsBeaconNet struct {
	network.Network
	bh        *blockHandler
	chainID   ids.ID
	connected []ids.NodeID            // beacons that are connected + tracking the chain
	byID      map[ids.ID]*bsTestBlock // honest chain by id (for ancestry serving)
	tip       *bsTestBlock            // the honest tip the beacons report
	serveAncestors bool               // beacons serve ancestry (false models name-only beacons)

	// Optional malicious NON-beacon peer: connected and tracking, reports a forged tip.
	malicious   ids.NodeID
	forgedTip   ids.ID
}

// PeerInfo returns the connected beacons (and the malicious peer, when present) that
// match the requested filter. The bootstrap path queries PeerInfo(beaconIDs), so only
// beacons in `connected` are returned for it; the malicious peer surfaces only on an
// unfiltered (nil) query — i.e. it is never in the beacon-restricted frontier sample.
func (n *bsBeaconNet) PeerInfo(nodeIDs []ids.NodeID) []peer.Info {
	want := map[ids.NodeID]bool{}
	for _, id := range nodeIDs {
		want[id] = true
	}
	var out []peer.Info
	for _, b := range n.connected {
		if len(nodeIDs) == 0 || want[b] {
			out = append(out, peer.Info{ID: b, TrackedChains: set.Of(n.chainID)})
		}
	}
	if n.malicious != ids.EmptyNodeID && (len(nodeIDs) == 0 || want[n.malicious]) {
		out = append(out, peer.Info{ID: n.malicious, TrackedChains: set.Of(n.chainID)})
	}
	return out
}

func (n *bsBeaconNet) Send(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ uint32) set.Set[ids.NodeID] {
	m, ok := msg.(*bsOutMsg)
	if !ok {
		return nil
	}
	switch m.op {
	case "frontier":
		// Each queried beacon answers with the honest tip.
		for id := range nodeIDs {
			n.bh.deliverBootstrapFrontier(id, n.tip.id)
		}
		// The malicious peer ALSO tries to inject its forged tip (it spams the channel),
		// modeling an attacker shouting a frontier. It must be IGNORED (not a beacon).
		if n.forgedTip != ids.Empty {
			n.bh.deliverBootstrapFrontier(n.malicious, n.forgedTip)
		}
	case "ancestors":
		if n.serveAncestors {
			n.bh.deliverBootstrapAncestors(m.requestID, n.frame(m.blockID))
		}
	}
	return nil
}

// frame serves up to 256 blocks ending at blockID, OLDEST-FIRST, in the same outer
// [entryLen:4][entry] framing GetContext produces (entry = encodeCatchupEntry).
func (n *bsBeaconNet) frame(blockID ids.ID) []byte {
	tip, ok := n.byID[blockID]
	if !ok {
		return nil
	}
	var rev []*bsTestBlock
	cur := tip
	for i := 0; i < 256; i++ {
		rev = append(rev, cur)
		if cur.parent == ids.Empty {
			break
		}
		cur = n.byID[cur.parent]
		if cur == nil {
			break
		}
	}
	var data []byte
	for i := len(rev) - 1; i >= 0; i-- { // oldest-first
		entry := encodeCatchupEntry(rev[i].bytes, nil)
		var lp [4]byte
		binary.BigEndian.PutUint32(lp[:], uint32(len(entry)))
		data = append(data, lp[:]...)
		data = append(data, entry...)
	}
	return data
}

// buildBSChain builds genesis..N. invalidAt (≥0) marks that height's block invalid.
func buildBSChain(n int, invalidAt int) ([]*bsTestBlock, map[ids.ID]*bsTestBlock) {
	chain := make([]*bsTestBlock, 0, n+1)
	byID := map[ids.ID]*bsTestBlock{}
	var parent ids.ID
	for h := 0; h <= n; h++ {
		b := &bsTestBlock{
			id:     ids.GenerateTestID(),
			parent: parent,
			height: uint64(h),
			bytes:  []byte("n-blk@" + strconv.Itoa(h) + ":" + ids.GenerateTestID().String()),
			valid:  h != invalidAt,
		}
		chain = append(chain, b)
		byID[b.id] = b
		parent = b.id
	}
	return chain, byID
}

// newBeaconManager builds a real validators.Manager registering `ids` as equal-weight
// beacons under networkID — the beacon set the frontier quorum is anchored to.
func newBeaconManager(networkID ids.ID, beaconIDs []ids.NodeID, weight uint64) validators.Manager {
	mgr := validators.NewManager()
	for _, id := range beaconIDs {
		_ = mgr.AddStaker(networkID, id, nil, ids.GenerateTestID(), weight)
	}
	return mgr
}

// newBSHandlerAndEngine wires a blockHandler over a fresh engine (K=1, no verifier) and
// the mock VM, seeded at genesis — the node-side of an EMPTY node — with `numBeacons`
// equal-weight beacons registered under networkID. Returns the handler, the chainID, and
// the beacon node IDs.
func newBSHandlerAndEngine(t *testing.T, vm *bsTestVM, numBeacons int) (*blockHandler, ids.ID, []ids.NodeID) {
	t.Helper()
	chainID := ids.GenerateTestID()
	networkID := ids.GenerateTestID()
	eng := consensuschain.NewRuntime(consensuschain.NetworkConfig{
		ChainID:   chainID,
		NetworkID: networkID,
		NodeID:    ids.GenerateTestNodeID(),
		Logger:    log.NewNoOpLogger(),
		Params:    &consensusconfig.Parameters{K: 1, AlphaPreference: 1, AlphaConfidence: 1, Beta: 1},
		VM:        vm,
	})
	// Seed consensus at the VM's genesis (height 0), as buildChain does via
	// SyncStateFromVM — this establishes the finalized tip the first gap block extends.
	if _, _, err := consensuschain.SyncStateFromVM(context.Background(), vm, eng.Transitive); err != nil {
		t.Fatalf("seed: %v", err)
	}
	beaconIDs := make([]ids.NodeID, numBeacons)
	for i := range beaconIDs {
		beaconIDs[i] = ids.GenerateTestNodeID()
	}
	bh := &blockHandler{
		logger:         log.NewNoOpLogger(),
		engine:         eng,
		vm:             vm,
		chainID:        chainID,
		networkID:      networkID,
		beacons:        newBeaconManager(networkID, beaconIDs, 100),
		pendingContext: make(map[ids.ID]contextRequest),
		bsAncestorCh:   make(map[uint32]chan [][]byte),
	}
	return bh, chainID, beaconIDs
}

func runBS(t *testing.T, bh *blockHandler) error {
	t.Helper()
	bh.bsActive.Store(true)
	defer bh.bsActive.Store(false)
	bs := chainbootstrap.New(chainbootstrap.Config{Source: bh, Chain: bh, Log: log.NewNoOpLogger()})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return bs.Run(ctx)
}

// TestNodeBootstrap_EmptyNodeConvergesViaTransport: a fresh/empty node (only genesis)
// FETCHES blocks 1..N from a quorum of beacons over the real
// GetAcceptedFrontier/GetAncestors path, re-EXECUTES them, and ACCEPTS up to N.
func TestNodeBootstrap_EmptyNodeConvergesViaTransport(t *testing.T) {
	const N = 50
	chain, byID := buildBSChain(N, -1)
	vm := newBSVM(chain)
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 4)

	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[N], serveAncestors: true}
	bh.msgCreator = bsMsgBuilder{}

	ctx := context.Background()
	require.NoError(t, runBS(t, bh), "bootstrap loop must converge over the transport")

	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last, "empty node must sync to the beacon-agreed tip (height %d)", N)
	require.Equal(t, 1, chain[N].accepts, "tip block must be VM-accepted exactly once")
	require.True(t, bh.Has(ctx, chain[N].id), "node must hold the tip after sync")
}

// TestRED_ForgedFrontierFromNonBeaconRejected is the HEADLINE C1 proof at the node layer.
//
// THE ATTACK (red): an eclipse/malicious peer serves a forged-but-Verify-passing chain
// from genesis and names it as the accepted frontier; the victim, sampling the frontier
// from ANY connected peer and accepting the first reply, finalized the forged chain
// cert-lessly (red's PoC: 40 forged blocks finalized) and bricked against the real chain.
//
// THE FIX: the frontier is named ONLY by a ⅔-by-stake quorum of BEACONS. A non-beacon
// peer is never even queried (PeerInfo is beacon-restricted) and its weight is zero in the
// tally, so the forged tip can never be named. Here the honest beacons are OFFLINE and the
// only reachable peer is the attacker: FrontierTip fails closed (no beacon quorum), the
// loop has "nothing to sync to", and ZERO forged blocks are finalized.
func TestRED_ForgedFrontierFromNonBeaconRejected(t *testing.T) {
	const N = 40
	honest, honestByID := buildBSChain(N, -1)
	forged, _ := buildBSChain(N, -1) // a different valid chain from a fresh genesis
	vm := newBSVM(honest)
	bh, chainID, _ := newBSHandlerAndEngine(t, vm, 4)

	// The 4 beacons exist in the validator set but are OFFLINE (connected = none). The
	// only reachable peer is a malicious NON-beacon naming + serving the forged chain.
	mal := ids.GenerateTestNodeID()
	net := &bsBeaconNet{
		bh: bh, chainID: chainID, connected: nil, byID: honestByID, tip: honest[N],
		serveAncestors: true, malicious: mal, forgedTip: forged[N].id,
	}
	bh.net = net
	bh.msgCreator = bsMsgBuilder{}

	ctx := context.Background()
	// FrontierTip must refuse to name a frontier (no beacon connected → no quorum). Drive
	// it with bsActive so a reply WOULD be routed — proving it is the QUORUM, not an inert
	// channel, that rejects the forged frontier.
	bh.bsActive.Store(true)
	tip, ok := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.False(t, ok, "C1: a non-beacon peer must NOT be able to name the frontier")
	require.Equal(t, ids.Empty, tip)

	// Drive the full loop: it converges (nothing to sync to) WITHOUT finalizing anything.
	require.NoError(t, runBS(t, bh))
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, honest[0].id, last, "C1: node must stay at genesis — the forged chain was NOT synced")
	require.False(t, bh.Has(ctx, forged[N].id), "C1: node must NOT hold the forged tip")
	for _, f := range forged[1:] {
		require.False(t, bh.Has(ctx, f.id), "C1: no forged block may be finalized")
	}
}

// TestRED_HonestBeaconQuorumIgnoresMaliciousFrontier: with the honest beacons ONLINE, the
// node syncs the canonical chain AND ignores a connected malicious non-beacon that is
// simultaneously shouting a forged frontier — proving the node syncs ONLY the
// beacon-agreed chain even when an attacker is present.
func TestRED_HonestBeaconQuorumIgnoresMaliciousFrontier(t *testing.T) {
	const N = 50
	honest, honestByID := buildBSChain(N, -1)
	forged, _ := buildBSChain(N, -1)
	vm := newBSVM(honest)
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 4)

	mal := ids.GenerateTestNodeID()
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: beacons, byID: honestByID, tip: honest[N],
		serveAncestors: true, malicious: mal, forgedTip: forged[N].id,
	}
	bh.msgCreator = bsMsgBuilder{}

	ctx := context.Background()
	require.NoError(t, runBS(t, bh))
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, honest[N].id, last, "node must sync the beacon-agreed canonical tip")
	require.False(t, bh.Has(ctx, forged[N].id), "node must ignore the malicious forged frontier")
}

// TestRED_SubAlphaStakeCannotNameFrontier: a minority of beacons (below the ⅔ stake floor)
// reporting a tip cannot name the frontier. Only once a ⅔-by-stake supermajority agrees
// does FrontierTip return the tip.
func TestRED_SubAlphaStakeCannotNameFrontier(t *testing.T) {
	const N = 30
	chain, byID := buildBSChain(N, -1)
	vm := newBSVM(chain)
	// 6 beacons, equal weight → ⅔ floor = floor(2*600/3) = 400, need > 400 ⇒ ≥ 5 beacons.
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 6)
	bh.msgCreator = bsMsgBuilder{}
	bh.bsActive.Store(true) // so frontier replies route to the quorum tally
	defer bh.bsActive.Store(false)
	ctx := context.Background()

	// Only 4 of 6 beacons connected (4*100 = 400, NOT > 400) → sub-quorum → no frontier.
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons[:4], byID: byID, tip: chain[N]}
	tip, ok := bh.FrontierTip(ctx)
	require.False(t, ok, "4/6 beacons (≤ ⅔ stake) must NOT name the frontier")
	require.Equal(t, ids.Empty, tip)

	// 5 of 6 connected (5*100 = 500 > 400) → quorum → the tip is named.
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons[:5], byID: byID, tip: chain[N]}
	tip, ok = bh.FrontierTip(ctx)
	require.True(t, ok, "5/6 beacons (> ⅔ stake) must name the frontier")
	require.Equal(t, chain[N].id, tip)
}

// TestNodeBootstrap_InvalidBlockHaltsTransport: beacons serve a corrupt block at height
// bad; the node accepts 1..bad-1 then STOPS — it never advances past the unverifiable
// block (the safety property, over the real transport, under a valid beacon frontier).
func TestNodeBootstrap_InvalidBlockHaltsTransport(t *testing.T) {
	const N = 40
	const bad = 25
	chain, byID := buildBSChain(N, bad)
	vm := newBSVM(chain)
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 4)

	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[N], serveAncestors: true}
	bh.msgCreator = bsMsgBuilder{}

	bh.bsActive.Store(true)
	bs := chainbootstrap.New(chainbootstrap.Config{Source: bh, Chain: bh, Log: log.NewNoOpLogger(), RetryInterval: time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = bs.Run(ctx) // stalls (cannot pass the invalid block)
	bh.bsActive.Store(false)

	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[bad-1].id, last, "sync must halt at the block below the invalid one (height %d)", bad-1)
}

// TestDecodeContextBlocks_RoundTrip: the bootstrap framing decode recovers exactly the
// block bytes GetContext framed, oldest-first, for both v2 (cert-carrying) and legacy
// entries.
func TestDecodeContextBlocks_RoundTrip(t *testing.T) {
	want := [][]byte{[]byte("oldest"), []byte("mid"), []byte("newest")}
	var data []byte
	for i, bz := range want {
		var entry []byte
		if i == 1 {
			entry = bz // legacy raw entry (no magic) — decode must treat the entry AS the block
		} else {
			entry = encodeCatchupEntry(bz, nil) // v2 frame, empty cert
		}
		var lp [4]byte
		binary.BigEndian.PutUint32(lp[:], uint32(len(entry)))
		data = append(data, lp[:]...)
		data = append(data, entry...)
	}

	got := decodeContextBlocks(data)
	require.Equal(t, want, got, "decodeContextBlocks must recover the framed blocks oldest-first")
}

// TestDeliverBootstrap_GatedByActive: the reply hooks deliver ONLY while the bootstrap
// loop is driving (bsActive). When inactive they return false so the live cert/vote
// path runs unchanged. The frontier reply now carries the responding beacon's nodeID.
func TestDeliverBootstrap_GatedByActive(t *testing.T) {
	bh := &blockHandler{bsAncestorCh: make(map[uint32]chan [][]byte)}

	// Inactive: both hooks are inert (return false → caller uses the live path).
	require.False(t, bh.deliverBootstrapFrontier(ids.GenerateTestNodeID(), ids.GenerateTestID()))
	require.False(t, bh.deliverBootstrapAncestors(1, nil))

	// Active: a registered FrontierTip channel receives the tagged reply.
	bh.bsActive.Store(true)
	fch := make(chan bsFrontierReply, 1)
	bh.bsFrontierCh = fch
	node := ids.GenerateTestNodeID()
	tip := ids.GenerateTestID()
	require.True(t, bh.deliverBootstrapFrontier(node, tip))
	got := <-fch
	require.Equal(t, node, got.nodeID)
	require.Equal(t, tip, got.tip)

	// Active: a registered Ancestors channel receives the decoded blocks.
	ach := make(chan [][]byte, 1)
	bh.bsAncestorCh[7] = ach
	entry := encodeCatchupEntry([]byte("blk"), nil)
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(entry)))
	require.True(t, bh.deliverBootstrapAncestors(7, append(lp[:], entry...)))
	require.Equal(t, [][]byte{[]byte("blk")}, <-ach)
}

// ----- H2: progress-based bootstrap watchdog --------------------------------

// TestWatchBootstrapProgress_SlowButAdvancingNotKilled is the H2 proof: a sync that takes
// LONGER than the stall window but keeps ADVANCING its height is NEVER killed (the old
// once-set 5-minute hard timer would have killed it). ready() only reports done after far
// more polls than the stall window spans, yet because the height climbs every poll the
// stall clock keeps resetting.
func TestWatchBootstrapProgress_SlowButAdvancingNotKilled(t *testing.T) {
	var calls int
	heightOf := func() uint64 { calls++; return uint64(calls) } // advances every poll
	ready := func() bool { return calls >= 120 }                 // done only after 120 polls (≫ the 60ms window)
	outcome, _ := watchBootstrapProgress(ready, heightOf, time.Millisecond, 60*time.Millisecond, nil)
	require.Equal(t, bootstrapReady, outcome, "a slow-but-ADVANCING sync must complete, not be timed out")
}

// TestWatchBootstrapProgress_GenuineStallFails: a sync whose height does NOT advance for
// the whole window is failed (a real stall is still surfaced, not masked).
func TestWatchBootstrapProgress_GenuineStallFails(t *testing.T) {
	heightOf := func() uint64 { return 5 } // pinned — no progress
	ready := func() bool { return false }
	start := time.Now()
	outcome, last := watchBootstrapProgress(ready, heightOf, time.Millisecond, 60*time.Millisecond, nil)
	require.Equal(t, bootstrapStalled, outcome, "a no-progress sync must stall out")
	require.Equal(t, uint64(5), last, "the stall diagnostic must report the height it stalled at")
	require.GreaterOrEqual(t, time.Since(start), 60*time.Millisecond, "must wait the full stall window before failing")
}

// TestWatchBootstrapProgress_Shutdown: a shutdown signal returns promptly without
// declaring success or failure.
func TestWatchBootstrapProgress_Shutdown(t *testing.T) {
	shutdown := make(chan struct{})
	go func() { time.Sleep(20 * time.Millisecond); close(shutdown) }()
	outcome, _ := watchBootstrapProgress(
		func() bool { return false }, func() uint64 { return 0 },
		time.Millisecond, time.Hour, shutdown,
	)
	require.Equal(t, bootstrapShutdown, outcome)
}
