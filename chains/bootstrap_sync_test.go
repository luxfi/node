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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	consensusconfig "github.com/luxfi/consensus/config"
	consensuschain "github.com/luxfi/consensus/engine/chain"
	cblock "github.com/luxfi/consensus/engine/chain/block"
	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	"github.com/luxfi/constants"
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

// bsTestVM models a node VM with the REAL store-vs-acceptance distinction (the prior mock
// conflated them — GetBlock returned only accepted blocks). It can PARSE any block on the chain
// (the wire codec is deterministic). It GETS a block that is either ACCEPTED or merely STORED (a
// block GOSSIPED into the store ahead of acceptance — the luxd-2 freeze precondition). `accepted`
// is the FINALIZED chain; `acceptedByHeight` is the canonical height→id index the real coreth VM
// maintains (GetBlockIDAtHeight); `stored` holds present-but-unaccepted blocks. SetPreference
// marks a block accepted — the engine calls it right after Accept, so LastAccepted/the index
// advance exactly as the node syncs.
type bsTestVM struct {
	all              map[ids.ID]*bsTestBlock
	byBytes          map[string]*bsTestBlock
	accepted         map[ids.ID]bool   // FINALIZED chain
	acceptedByHeight map[uint64]ids.ID // canonical height→id index (coreth GetBlockIDAtHeight)
	stored           map[ids.ID]bool   // present in the store but NOT (yet) accepted (gossiped-ahead)
	lastAcc          ids.ID
}

func newBSVM(chain []*bsTestBlock) *bsTestVM {
	vm := &bsTestVM{
		all:              map[ids.ID]*bsTestBlock{},
		byBytes:          map[string]*bsTestBlock{},
		accepted:         map[ids.ID]bool{},
		acceptedByHeight: map[uint64]ids.ID{},
		stored:           map[ids.ID]bool{},
	}
	for _, b := range chain {
		vm.all[b.id] = b
		vm.byBytes[string(b.bytes)] = b
	}
	// Empty node: only genesis is accepted.
	vm.accepted[chain[0].id] = true
	vm.acceptedByHeight[chain[0].height] = chain[0].id
	vm.lastAcc = chain[0].id
	return vm
}

// newBSVMAt seeds the VM with blocks 0..m ACCEPTED (a node already at height m) so a test
// can model a STALE node rather than an empty one. SyncStateFromVM then seeds consensus at
// height m, and the first fetched gap block extends chain[m].
func newBSVMAt(chain []*bsTestBlock, m int) *bsTestVM {
	vm := newBSVM(chain)
	for i := 1; i <= m; i++ {
		vm.accepted[chain[i].id] = true
		vm.acceptedByHeight[chain[i].height] = chain[i].id
	}
	vm.lastAcc = chain[m].id
	return vm
}

// store marks blocks PRESENT in the block store WITHOUT accepting them — models blocks GOSSIPED
// into the store ahead of acceptance (the luxd-2 freeze precondition: GetBlock returns them, but
// they are not on the accepted chain and not in the canonical height index).
func (m *bsTestVM) store(blocks ...*bsTestBlock) {
	for _, b := range blocks {
		m.stored[b.id] = true
	}
}

func (m *bsTestVM) BuildBlock(context.Context) (cblock.Block, error) { return nil, errBSNoBuild }
func (m *bsTestVM) GetBlock(_ context.Context, id ids.ID) (cblock.Block, error) {
	if m.accepted[id] || m.stored[id] {
		return m.all[id], nil
	}
	return nil, errBSNotStored
}

// GetBlockIDAtHeight is the canonical ACCEPTED height→id index (coreth's authoritative oracle): it
// returns the FINALIZED block at a height, never a stored-but-unaccepted one. ids.Empty when no
// block is accepted at h — exactly how the real VM reports "nothing finalized there".
func (m *bsTestVM) GetBlockIDAtHeight(_ context.Context, h uint64) (ids.ID, error) {
	if id, ok := m.acceptedByHeight[h]; ok {
		return id, nil
	}
	return ids.Empty, nil
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
	if blk, ok := m.all[id]; ok {
		m.acceptedByHeight[blk.height] = id
	}
	delete(m.stored, id) // accepting a stored-ahead block promotes it onto the finalized chain
	// The real ZAP client refreshes its cached LastAccepted on every successful Accept
	// (vms/rpcchainvm/zap/client.go) — a live cache is the client contract the position
	// rule stands on, so the mock honors it.
	m.lastAcc = id
	return nil
}

// ----- mock outbound message + builder --------------------------------------

type bsOutMsg struct {
	op           string
	blockID      ids.ID
	requestID    uint32
	containerIDs []ids.ID
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
func (bsMsgBuilder) GetAccepted(_ ids.ID, requestID uint32, _ time.Duration, containerIDs []ids.ID) (message.OutboundMessage, error) {
	return &bsOutMsg{op: "getaccepted", requestID: requestID, containerIDs: containerIDs}, nil
}
func (bsMsgBuilder) Accepted(_ ids.ID, requestID uint32, containerIDs []ids.ID) (message.OutboundMessage, error) {
	return &bsOutMsg{op: "accepted", requestID: requestID, containerIDs: containerIDs}, nil
}

// ----- beacon-aware mock peer net -------------------------------------------

// bsBeaconNet models a network of BEACONS (each with stake) that hold the chain, plus an
// optional MALICIOUS non-beacon peer that reports/serves a forged chain. The bootstrap
// frontier quorum must name the tip ONLY from the connected beacons, ignoring the
// malicious peer entirely. `connected` is the subset of beacons currently reachable (so a
// test can model "beacons offline, only the attacker is up").
type bsBeaconNet struct {
	network.Network
	bh             *blockHandler
	chainID        ids.ID
	connected      []ids.NodeID            // beacons that are connected + tracking the chain
	byID           map[ids.ID]*bsTestBlock // honest chain by id (for ancestry serving)
	tip            *bsTestBlock            // the honest tip the beacons report
	serveAncestors bool                    // beacons serve ancestry (false models name-only beacons)
	ancestorsEmpty bool                    // beacons REPLY to GetAncestors but serve an EMPTY batch (cross-version / withholding)

	// emptyResponders models a MIXED fleet on a WIPE-path recovery: the named beacons REPLY to
	// GetAncestors with an EMPTY batch (they lack the block — e.g. still at genesis after a
	// simultaneous wipe), while the rest serve the real ancestry. When set, the mock delivers the
	// empties FIRST, proving the fetcher (blockHandler.Ancestors) skips them and still returns the
	// non-empty batch — never starved by a peer that can't serve. Requires serveAncestors=true.
	emptyResponders set.Set[ids.NodeID]

	// tipFor optionally overrides the tip a specific beacon reports (models DISAGREEMENT —
	// beacons connected but split across tips, so no ⅔ quorum forms → FrontierNoQuorum).
	tipFor map[ids.NodeID]ids.ID

	// withhold names blocks no sampled beacon serves ancestry for WHILE THE FLEET IS SPREAD. The
	// bleeding edge of a live chain is exactly this: the highest blocks are held by one or two
	// nodes, so a walk that must fetch them to compare heads gets empties, while the settled chain
	// below is served by everyone. Once the fleet settles (tipFor2) those blocks are held
	// everywhere and serve normally. Acceptance answers are unaffected either way — a beacon
	// answers about its own chain without serving anything.
	withhold set.Set[ids.ID]

	// connectAfterCalls models the CANARY boot race over the REAL transport: while
	// peerInfoCalls ≤ connectAfterCalls the beacons are reported as NOT yet connected (so
	// FrontierTip must return FrontierConnecting and the loop WAITS); afterward they connect.
	connectAfterCalls int
	peerInfoCalls     int

	// gate models a DYNAMIC eclipse a concurrent test goroutine flips (atomic: PeerInfo runs on the
	// bootstrap goroutine). gate==true ⇒ NO beacon is reported connected (the quorum is unreachable),
	// the self-heal driver re-attempts; gate==false ⇒ beacons connect and the node converges. Zero
	// value (false) is the default (connected), so existing tests are unaffected.
	gate atomic.Bool

	// Optional malicious NON-beacon peer: connected and tracking, reports a forged tip.
	malicious ids.NodeID
	forgedTip ids.ID

	// tipFor2 + propagateAtHeight model finalization PROPAGATION across the fleet, the faithful
	// analog of the connectivity evolution above. A beacon reports tipFor[id] UNTIL the syncing
	// node has accepted through propagateAtHeight, then reports tipFor2[id]. This is what
	// GetAcceptedFrontier returns in production (chains/manager.go: vm.LastAccepted — the
	// last-ACCEPTED block, NEVER an un-finalized preferred tip): on a live chain the laggards'
	// last-accepted frontier RATCHETS UP as finalization completes, so by the time a behind node
	// has synced to the ⅔-common height the bleeding-edge block is finalized fleet-wide. Modeling
	// it this way lets a full-loop test exercise the ancestor-tolerant ⅔-common naming for a BEHIND
	// node AND still reach caught-up at the TRUE finalized tip — instead of a STATIC minority-above
	// that (post the M1 own-height-exclusion fix) correctly refuses to go-live stale. Zero value
	// (nil / 0) disables it, so existing tests are unaffected.
	tipFor2           map[ids.NodeID]ids.ID
	propagateAtHeight uint64
}

// nodeAcceptedHeight reports the syncing node's current last-ACCEPTED height (0 if unknown) — the
// signal the propagation model keys on, exactly the value chains/manager.go answers GetAcceptedFrontier with.
func (n *bsBeaconNet) nodeAcceptedHeight() uint64 {
	if n.bh == nil || n.bh.vm == nil {
		return 0
	}
	id, err := n.bh.vm.LastAccepted(context.Background())
	if err != nil || id == ids.Empty {
		return 0
	}
	blk, err := n.bh.vm.GetBlock(context.Background(), id)
	if err != nil {
		return 0
	}
	return blk.Height()
}

// PeerInfo returns the connected beacons (and the malicious peer, when present) that
// match the requested filter. The bootstrap path queries PeerInfo(beaconIDs), so only
// beacons in `connected` are returned for it; the malicious peer surfaces only on an
// unfiltered (nil) query — i.e. it is never in the beacon-restricted frontier sample.
// When connectAfterCalls > 0 the beacons are withheld for the first that many calls,
// modeling a beacon set that has not finished its handshake at boot.
//
// TrackedChains models REALITY (network/peer/peer.go): a peer advertises the NETS it tracks
// — always constants.PrimaryNetworkID plus any tracked L1 net IDs — NEVER an individual
// native chain ID. So beacons report the handler's networkID (the NET the beacon set is
// anchored to), the value connectedBeacons now filters on. The pre-fix code filtered on the
// chain ID, which no real peer advertises; the old mock reported set.Of(n.chainID), masking
// that bug. Reporting set.Of(networkID) is what makes the canary-convergence tests meaningful.
func (n *bsBeaconNet) PeerInfo(nodeIDs []ids.NodeID) []peer.Info {
	n.peerInfoCalls++
	// gate (atomic) is the dynamic eclipse: when set, NO beacon is reported connected this round.
	beaconsUp := (n.connectAfterCalls == 0 || n.peerInfoCalls > n.connectAfterCalls) && !n.gate.Load()
	trackedNet := n.bh.networkID // the NET real peers advertise (incl. PrimaryNetworkID)
	want := map[ids.NodeID]bool{}
	for _, id := range nodeIDs {
		want[id] = true
	}
	var out []peer.Info
	if beaconsUp {
		for _, b := range n.connected {
			if len(nodeIDs) == 0 || want[b] {
				out = append(out, peer.Info{ID: b, TrackedChains: set.Of(trackedNet)})
			}
		}
	}
	if n.malicious != ids.EmptyNodeID && (len(nodeIDs) == 0 || want[n.malicious]) {
		out = append(out, peer.Info{ID: n.malicious, TrackedChains: set.Of(trackedNet)})
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
		// Each queried beacon answers with the honest tip (or its tipFor override, modeling
		// disagreement) — and, once the node has accepted through propagateAtHeight, its tipFor2
		// (modeling finalization propagating across the fleet so the bleeding edge resolves).
		for id := range nodeIDs {
			n.bh.deliverBootstrapFrontier(id, n.tipOf(id))
		}
	case "getaccepted":
		// A beacon answers the SECOND question about its OWN chain: it has accepted every asked
		// candidate at or below its own tip's height. This needs nothing SERVED — that is the
		// point of the question — so a beacon whose ancestry is withheld still answers it.
		for id := range nodeIDs {
			h := n.heightOf(n.tipOf(id))
			var accepted []ids.ID
			for _, c := range m.containerIDs {
				if blk, ok := n.byID[c]; ok && blk.height <= h {
					accepted = append(accepted, c)
				}
			}
			n.bh.deliverBootstrapAccepted(m.requestID, id, accepted)
		}
		// The malicious peer ALSO tries to inject its forged tip (it spams the channel),
		// modeling an attacker shouting a frontier. It must be IGNORED (not a beacon).
		if n.forgedTip != ids.Empty {
			n.bh.deliverBootstrapFrontier(n.malicious, n.forgedTip)
		}
	case "ancestors":
		if n.emptyResponders != nil {
			// Mixed fleet: emptyResponders reply EMPTY (they lack the block), the rest serve.
			// Deliver the empties FIRST so the test proves Ancestors skips them and returns the
			// slower non-empty batch (the WIPE-path ≥2-at-genesis case).
			var servers []ids.NodeID
			for id := range nodeIDs {
				if n.emptyResponders.Contains(id) {
					n.bh.deliverBootstrapAncestors(id, m.requestID, nil)
				} else {
					servers = append(servers, id)
				}
			}
			for _, id := range servers {
				n.bh.deliverBootstrapAncestors(id, m.requestID, n.frame(m.blockID))
			}
			return nil
		}
		for id := range nodeIDs {
			if n.serveAncestors {
				n.bh.deliverBootstrapAncestors(id, m.requestID, n.frame(m.blockID))
			} else if n.ancestorsEmpty {
				// The peer REPLIES but serves NOTHING — the cross-version / withholding case the
				// canary risked. The descent gets an empty batch and must fail safe (ErrStalled),
				// never silently conclude caught-up at the stale height.
				n.bh.deliverBootstrapAncestors(id, m.requestID, nil)
			}
		}
	}
	return nil
}

// tipOf is the tip a beacon reports: the honest tip, its tipFor override (modeling a fleet spread
// over several heads), or — once the node has accepted through propagateAtHeight — its tipFor2
// (modeling finalization propagating across the fleet).
func (n *bsBeaconNet) tipOf(nodeID ids.NodeID) ids.ID {
	tip := n.tip.id
	if t, ok := n.tipFor[nodeID]; ok {
		tip = t
	}
	if n.settled() {
		if t, ok := n.tipFor2[nodeID]; ok {
			tip = t
		}
	}
	return tip
}

// settled reports that finalization has propagated across the fleet — the laggards have ratcheted
// up to the tip that was the bleeding edge, so the fleet no longer disagrees and what was held by
// one or two nodes is now held by all of them.
func (n *bsBeaconNet) settled() bool {
	return n.tipFor2 != nil && n.nodeAcceptedHeight() >= n.propagateAtHeight
}

// heightOf resolves a block's height on the honest chain (0 when unknown).
func (n *bsBeaconNet) heightOf(blockID ids.ID) uint64 {
	if blk, ok := n.byID[blockID]; ok {
		return blk.height
	}
	return 0
}

// frame serves up to 256 blocks ending at blockID, OLDEST-FIRST, in the same outer
// [entryLen:4][entry] framing GetContext produces (entry = encodeCatchupEntry).
func (n *bsBeaconNet) frame(blockID ids.ID) []byte {
	if n.withhold.Contains(blockID) && !n.settled() {
		return nil // still the bleeding edge: too few nodes hold it for a sample to fetch it
	}
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

// newBSEngine builds a fresh K=1 (no-verifier) consensus engine over the mock VM, seeded at
// the VM's genesis (height 0) via SyncStateFromVM — the shared core of the bootstrap test
// handlers. Returns the engine plus fresh chain + network IDs.
func newBSEngine(t *testing.T, vm *bsTestVM) (*consensuschain.Runtime, ids.ID, ids.ID) {
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
	if _, _, err := consensuschain.SyncStateFromVM(context.Background(), vm, eng.Transitive); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return eng, chainID, networkID
}

// newBSHandlerAndEngine wires a blockHandler over a fresh engine and the mock VM, seeded at
// genesis — the node-side of an EMPTY node — with `numBeacons` equal-weight beacons registered
// under networkID. Returns the handler, the chainID, and the beacon node IDs.
func newBSHandlerAndEngine(t *testing.T, vm *bsTestVM, numBeacons int) (*blockHandler, ids.ID, []ids.NodeID) {
	t.Helper()
	eng, chainID, networkID := newBSEngine(t, vm)
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

// newBSHandlerWeighted wires a blockHandler (as newBSHandlerAndEngine) but with a caller-
// supplied STAKE-WEIGHTED beacon set, modeling a real primary-network validator set where
// producers hold the majority of stake. expectsStakedBeacons is true (these are native-chain
// staked beacons). Returns the handler and its chainID.
func newBSHandlerWeighted(t *testing.T, vm *bsTestVM, weights map[ids.NodeID]uint64) (*blockHandler, ids.ID) {
	t.Helper()
	eng, chainID, networkID := newBSEngine(t, vm)
	mgr := validators.NewManager()
	for id, w := range weights {
		require.NoError(t, mgr.AddStaker(networkID, id, nil, ids.GenerateTestID(), w))
	}
	bh := &blockHandler{
		logger:               log.NewNoOpLogger(),
		engine:               eng,
		vm:                   vm,
		chainID:              chainID,
		networkID:            networkID,
		beacons:              mgr,
		expectsStakedBeacons: true,
		pendingContext:       make(map[ids.ID]contextRequest),
		bsAncestorCh:         make(map[uint32]chan [][]byte),
	}
	return bh, chainID
}

func runBS(t *testing.T, bh *blockHandler) error {
	t.Helper()
	bh.bsActive.Store(true)
	defer bh.bsActive.Store(false)
	bs := chainbootstrap.New(chainbootstrap.Config{
		Source:          bh,
		Chain:           bh,
		Log:             log.NewNoOpLogger(),
		RetryInterval:   2 * time.Millisecond,   // fast re-sample/connect polling in tests
		ConnectDeadline: 500 * time.Millisecond, // bound the beacon-connectivity wait in tests
	})
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
	require.True(t, bh.Accepted(ctx, chain[N].id), "node must hold the tip after sync")
}

// TestNodeBootstrap_WipePath_MixedGenesisPeers_ObtainsAncestry is the regression guard for the
// WIPE-path ≥2-at-genesis stall (deliverable 3). A re-bootstrapping node samples a fleet where
// SOME beacons reply to GetAncestors with an EMPTY batch (they lack the block — still at genesis
// after a simultaneous wipe) while the rest serve the real ancestry, and the empties arrive FIRST.
// With the pre-fix size-1 reply channel + first-reply-wins, the empty reply won the race and the
// good peer's non-empty batch was dropped, so the descent got nothing every round and stalled. The
// fix buffers the whole sample and SKIPS empty batches, returning the first non-empty one — so the
// node obtains ancestry from the peers that CAN serve, even when ≥2 peers are at genesis.
func TestNodeBootstrap_WipePath_MixedGenesisPeers_ObtainsAncestry(t *testing.T) {
	const N = 30
	chain, byID := buildBSChain(N, -1)
	vm := newBSVM(chain)
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 5)

	// 2 of 5 beacons are "still at genesis" — they reply EMPTY to GetAncestors. The mock
	// delivers those empties FIRST so a first-reply-wins fetcher would be starved.
	empties := set.NewSet[ids.NodeID](2)
	empties.Add(beacons[0], beacons[1])
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[N],
		serveAncestors: true, emptyResponders: empties,
	}
	bh.msgCreator = bsMsgBuilder{}

	ctx := context.Background()
	require.NoError(t, runBS(t, bh), "node must converge despite ≥2 peers serving empty ancestry")

	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last, "node must sync to the tip via the peers that CAN serve")
	require.True(t, bh.Accepted(ctx, chain[N].id))
}

// TestSampleAncestorBeacons_PrefersAheadBeacons proves change 2: the ancestry sample PREFERS
// beacons the frontier round found genuinely AHEAD (they hold the ancestry), so a re-bootstrapping
// node asks peers that can serve rather than wasting the bounded sample on genesis peers. With more
// connected beacons than the sample size, blind rotation would periodically EXCLUDE any given
// beacon; the preference guarantees the recorded ahead-beacon is ALWAYS sampled.
func TestSampleAncestorBeacons_PrefersAheadBeacons(t *testing.T) {
	const numBeacons = 8 // > bootstrapAncestorSample (4), so rotation alone would sometimes miss one
	chain, byID := buildBSChain(1, -1)
	vm := newBSVM(chain)
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, numBeacons)
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[1]}
	bh.msgCreator = bsMsgBuilder{}

	// Record ONE beacon as ahead (the frontier round's output). It must appear in EVERY sample.
	ahead := beacons[numBeacons-1]
	bh.bsMu.Lock()
	bh.bsAheadBeacons = set.Of(ahead)
	bh.bsMu.Unlock()

	for i := 0; i < 2*numBeacons; i++ {
		sample, ok := bh.sampleAncestorBeacons()
		require.True(t, ok)
		require.LessOrEqual(t, sample.Len(), bootstrapAncestorSample)
		require.True(t, sample.Contains(ahead),
			"the recorded ahead-beacon must be preferred into every ancestry sample (call %d)", i)
	}
}

// TestRED_LedgerAheadOfApplied_ConvergesOffAppliedHead guards the wedge measured on five
// live fleets: the consensus finalized ledger sits AHEAD of the VM's applied head, and a
// loop positioned at the LEDGER believes it stands past blocks it has not executed. It
// skips exactly the band it must fetch, reports full batches landed, and re-asks forever
// — isBootstrapped stays false while every counter reads success.
//
// The seam is produced the way production produces it: the node finalizes through F,
// then crash-restarts, and the VM reopens at an older commit M — the EVM persists on a
// commit interval, so reopening below the finalized tip is its documented recovery
// behavior, not an anomaly. The ledger says F; the VM says M; blocks M+1..F are decided
// and unexecuted.
//
// The position rule under test: the loop stands at the LOWER of the two heads, and the
// bootstrap accept path EXECUTES the band the ledger decided. The VM cache is live — the
// real ZAP client refreshes LastAccepted on every successful Accept — so positioning on
// it is honest.
func TestRED_LedgerAheadOfApplied_ConvergesOffAppliedHead(t *testing.T) {
	const N = 40
	const M = 24 // where the VM reopens after the crash
	const F = 31 // where the ledger stands (finalized before the crash)
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M)
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 4)

	// Finalize M+1..F through the real bootstrap accept path (ledger AND VM advance)…
	for i := M + 1; i <= F; i++ {
		require.NoError(t, bh.engine.AcceptBootstrapBlock(context.Background(), chain[i].bytes),
			"finalize height %d before the crash", i)
	}
	// …then the crash: the VM reopens at its last durable commit, M. The ledger keeps F.
	vm.lastAcc = chain[M].id
	if _, h, set := bh.engine.FinalizedLedger(); !set || h != uint64(F) {
		t.Fatalf("precondition: ledger at (%d,%v), want %d", h, set, F)
	}

	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[N], serveAncestors: true}
	bh.msgCreator = bsMsgBuilder{}

	ctx := context.Background()

	// Position must be the applied head, not the ledger — this is what makes the loop
	// fetch the band instead of believing it already stands past it.
	posID, posH, err := bh.LastAccepted(ctx)
	require.NoError(t, err)
	require.Equal(t, chain[M].id, posID, "position must be the VM applied head, not the ledger")
	require.Equal(t, uint64(M), posH)

	require.NoError(t, runBS(t, bh),
		"must converge by EXECUTING the band the ledger decided and the VM never ran")

	// The one honest number: the VM applied everything through the tip.
	last, err := vm.LastAccepted(ctx)
	require.NoError(t, err)
	require.Equal(t, chain[N].id, last, "the applied head must reach the frontier — counters that say otherwise lie")

	tip, h, set := bh.engine.FinalizedLedger()
	require.True(t, set)
	require.Equal(t, chain[N].id, tip)
	require.Equal(t, uint64(N), h)
	require.True(t, bh.Accepted(ctx, chain[N].id), "caught-up oracle must recognize the frontier")
	require.False(t, bh.Accepted(ctx, ids.GenerateTestID()), "an unknown/ahead id must never read as accepted")
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
// only reachable peer is the attacker: FrontierTip reports FrontierConnecting (no beacon
// quorum reachable), and — folding in red's LOW — the loop FAILS SAFE
// (ErrBeaconsUnreachable) rather than false-completing at the stale height. ZERO forged
// blocks are finalized either way.
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
	// FrontierTip must refuse to name a frontier. With NO beacon connected the status is
	// FrontierConnecting (not enough stake up to even ask) — NEVER FrontierNamed. Drive it
	// with bsActive so a reply WOULD be routed — proving it is the QUORUM, not an inert
	// channel, that rejects the forged frontier.
	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.NotEqual(t, chainbootstrap.FrontierNamed, status, "C1: a non-beacon peer must NOT be able to name the frontier")
	require.Equal(t, chainbootstrap.FrontierConnecting, status, "no beacon connected → still connecting, not a named frontier")
	require.Equal(t, ids.Empty, tip)

	// Drive the full loop: with the beacons offline it FAILS SAFE (red's LOW — an eclipsed
	// node does NOT false-complete at its stale height) and finalizes NOTHING.
	err := runBS(t, bh)
	require.ErrorIs(t, err, chainbootstrap.ErrBeaconsUnreachable, "eclipsed node must fail safe, not false-complete")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, honest[0].id, last, "C1: node must stay at genesis — the forged chain was NOT synced")
	require.False(t, bh.Accepted(ctx, forged[N].id), "C1: node must NOT hold the forged tip")
	for _, f := range forged[1:] {
		require.False(t, bh.Accepted(ctx, f.id), "C1: no forged block may be finalized")
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
	require.False(t, bh.Accepted(ctx, forged[N].id), "node must ignore the malicious forged frontier")
}

// TestRED_SubQuorumCannotNameFrontier: a minority of configured beacons cannot name the
// frontier — the partition-capture floor (INVARIANT 2). This REPLACES the prior
// SubAlphaStakeCannotNameFrontier, which asserted the ⅔-of-CURRENT-TOTAL-STAKE connect gate that
// was THE MASS-RECOVERY BUG: it required ⅔ of the WHOLE validator set to be connected, which is
// unsatisfiable when the down validators ARE the recovery targets. Under the BootstrapTrust
// policy the floor is MinResponses (a count of authenticated CONFIGURED beacons that must
// respond), defaulting to a MAJORITY of the set — so an attacker who partitions the node down to
// a minority of beacons still cannot capture the frontier, while a recovering node only needs a
// majority (not ⅔ of total stake) of beacons reachable. The C1 "minority cannot name" property is
// preserved; only the floor changed from ⅔-of-total-stake to a response COUNT over RESPONDERS.
func TestRED_SubQuorumCannotNameFrontier(t *testing.T) {
	const N = 30
	chain, byID := buildBSChain(N, -1)
	vm := newBSVM(chain)
	// 6 beacons, equal weight. Default MinResponses = majority(6) = 4. Below 4 responders → no
	// trusted quorum (capture-prevented); at/above 4 with ⅔-of-responders agreement → named.
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 6)
	bh.msgCreator = bsMsgBuilder{}
	bh.bsActive.Store(true) // so frontier replies route to the policy decision
	defer bh.bsActive.Store(false)
	ctx := context.Background()

	// Only 3 of 6 connected (3 < the majority floor of 4) → fewer than MinResponses configured
	// beacons responded → CONNECTING (wait for more; never name from the captured minority, never
	// conclude caught-up at the stale height).
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons[:3], byID: byID, tip: chain[N]}
	tip, status := bh.FrontierTip(ctx)
	require.Equal(t, chainbootstrap.FrontierConnecting, status, "3/6 beacons (below the majority response floor) cannot name the frontier — still connecting")
	require.Equal(t, ids.Empty, tip)

	// 4 of 6 connected (= the majority floor) and all agreeing → the policy names the tip. Note
	// the OLD ⅔-of-total gate would have REJECTED this (4*100 = 400 is NOT > the 400 floor) — that
	// rejection was the deadlock; the response-floor model correctly accepts it.
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons[:4], byID: byID, tip: chain[N]}
	tip, status = bh.FrontierTip(ctx)
	require.Equal(t, chainbootstrap.FrontierNamed, status, "4/6 beacons (≥ the majority floor) all agreeing must name the frontier")
	require.Equal(t, chain[N].id, tip)
}

// TestNodeBootstrap_StaleNodeWaitsForBeaconConnect REPRODUCES THE MAINNET CANARY over the
// REAL GetAcceptedFrontier/GetAncestors transport. A STALE node at height M boots while its
// beacons are still handshaking: the first PeerInfo polls report NO beacon connected, so
// FrontierTip returns FrontierConnecting and the loop WAITS — it must NOT conclude caught-up
// at M (the canary bug: luxd-2 declared "bootstrap complete" at its stale height 1082780
// BEFORE the beacons connected, then could never pull the gap). Once the beacons connect, a
// ⅔ quorum names tip N and the node descends, executes the gap, and converges to N.
func TestNodeBootstrap_StaleNodeWaitsForBeaconConnect(t *testing.T) {
	const N = 40 // beacon-named frontier height (the producers)
	const M = 23 // our STALE local height (gap N-M = 17 — the canary's gap-17, within window)
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M) // STALE: already accepted 0..M
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 4)

	net := &bsBeaconNet{
		bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[N],
		serveAncestors:    true,
		connectAfterCalls: 4, // beacons finish handshaking only after the 4th PeerInfo poll
	}
	bh.net = net
	bh.msgCreator = bsMsgBuilder{}

	ctx := context.Background()
	require.NoError(t, runBS(t, bh), "stale node must converge once beacons connect")

	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last, "CANARY: stale node must converge to the beacon frontier N=%d (NOT false-complete at the stale M=%d)", N, M)
	require.True(t, bh.Accepted(ctx, chain[N].id), "node must hold the beacon tip after sync")
	require.Greater(t, net.peerInfoCalls, net.connectAfterCalls, "the loop must have WAITED through the connecting passes before naming the frontier")
}

// TestNodeBootstrap_BeaconsSplitNoQuorum: the beacons ARE connected (enough stake to ASK) but
// are split 3/3 across two INCOMPATIBLE chains (separate geneses — a genuine partition, no
// shared ancestor), so neither half holds ⅔ and there is NO ⅔-backed COMMON committed block.
// FrontierTip reports FrontierNoQuorum and the loop fails safe (proven instantly at the
// consensus layer by TestLoop_BeaconsConnectedNoQuorum_FailsSafe).
//
// This is the genuine-partition guard for the ancestor-tolerant tally: ancestor tolerance names
// the highest block a ⅔-by-stake supermajority SHARES, so it must NOT manufacture a quorum here.
// The beacons serve their ancestry (serveAncestors), so the tally actually fetches and walks
// both halves — and STILL finds no block with ⅔ behind it, because the two halves share no
// ancestor. A ±1-block bleeding-edge split (one shared chain) converges; a real fork does not.
func TestNodeBootstrap_BeaconsSplitNoQuorum(t *testing.T) {
	const N = 30
	chain, byID := buildBSChain(N, -1)
	other, _ := buildBSChain(N, -1) // a DISJOINT chain (fresh genesis) — no common ancestor
	vm := newBSVMAt(chain, 10)
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 6)

	// 6 connected (600 > 400 floor → enough to ASK), split 3/3 so neither chain holds ⅔ and the
	// two halves share no ancestor: no ⅔-backed common block exists.
	tipFor := map[ids.NodeID]ids.ID{}
	for _, id := range beacons[3:] {
		tipFor[id] = other[N].id
	}
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[N], tipFor: tipFor, serveAncestors: true}
	bh.msgCreator = bsMsgBuilder{}

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(context.Background())
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNoQuorum, status, "disjoint split (no ⅔-backed common ancestor) → FrontierNoQuorum, never caught-up")
	require.Equal(t, ids.Empty, tip)
}

// TestNodeBootstrap_NoBeaconSet_ReportsNoBeacons: a node with NO beacon set configured
// (single-node / dev / --skip-bootstrap) reports FrontierNoBeacons, so the loop completes
// rather than hanging on a quorum that can never form.
func TestNodeBootstrap_NoBeaconSet_ReportsNoBeacons(t *testing.T) {
	const N = 10
	chain, byID := buildBSChain(N, -1)
	vm := newBSVM(chain)
	bh, chainID, _ := newBSHandlerAndEngine(t, vm, 0) // zero beacons registered
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, byID: byID, tip: chain[N]}
	bh.msgCreator = bsMsgBuilder{}

	tip, status := bh.FrontierTip(context.Background())
	require.Equal(t, chainbootstrap.FrontierNoBeacons, status, "no beacon set → single-node/dev → NoBeacons (the loop completes, no hang)")
	require.Equal(t, ids.Empty, tip)
}

// TestNodeBootstrap_FreshNet_SelfVoteUnderFullConnectivity_CaughtUp is THE FRESH-NET FIX. The node
// is itself a beacon (selfNodeID) on a 2-validator set; on a fresh net both hold only genesis. The
// PEER-ONLY responder weight (100) is BELOW the stake-majority naming floor (200/2+1 = 101), so the
// node could NEVER name a frontier and hung in FrontierConnecting forever — "bootstrap waiting for
// beacon connectivity" — DESPITE the whole set being connected. With the self-vote, under FULL
// connectivity (the one other beacon reached) the full set PLUS the node itself unanimously hold
// genesis with nobody ahead → FrontierCaughtUp, and the loop completes at the node's own genesis tip.
func TestNodeBootstrap_FreshNet_SelfVoteUnderFullConnectivity_CaughtUp(t *testing.T) {
	const N = 5
	chain, byID := buildBSChain(N, -1)
	vm := newBSVM(chain)                                    // the node is at genesis (lastAccepted = genesis)
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 2) // 2 equal-weight (100) beacons
	bh.selfNodeID = beacons[0]                              // THIS node is beacon 0 — it is itself a validator
	bh.msgCreator = bsMsgBuilder{}

	// FULL connectivity: the ONE other beacon is connected and reports GENESIS (a fresh net — every
	// validator holds only genesis). connected EXCLUDES self (a node is never one of its own peers).
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons[1:], byID: byID, tip: chain[0]}

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(context.Background())
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierCaughtUp, status,
		"fresh net, FULL set + self all at genesis, peer-only weight below the naming floor → CAUGHT UP (the fix), never a FrontierConnecting hang")
	require.Equal(t, chain[0].id, tip, "caught up at the node's OWN genesis tip")
}

// TestNodeBootstrap_SelfVote_PeerAheadDefeatsCaughtUp proves the self-vote NEVER false-completes a
// genuinely BEHIND node. Same 2-beacon set with the node a beacon and FULL connectivity, but the
// peer is AHEAD at N (an EXISTING net the empty node must SYNC to). A responder is above the node's
// height, so CaughtUp(self+peers) is false → NOT FrontierCaughtUp: the node keeps waiting/syncing,
// never goes live at its stale genesis. (The luxfi/consensus loop then descends once a frontier is
// named, or fails safe.)
func TestNodeBootstrap_SelfVote_PeerAheadDefeatsCaughtUp(t *testing.T) {
	const N = 20
	chain, byID := buildBSChain(N, -1)
	vm := newBSVM(chain) // node at genesis
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 2)
	bh.selfNodeID = beacons[0]
	bh.msgCreator = bsMsgBuilder{}
	// FULL connectivity, but the peer reports the AHEAD tip N — the node is genuinely behind.
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons[1:], byID: byID, tip: chain[N], serveAncestors: true}

	bh.bsActive.Store(true)
	_, status := bh.FrontierTip(context.Background())
	bh.bsActive.Store(false)
	require.NotEqual(t, chainbootstrap.FrontierCaughtUp, status,
		"a peer AHEAD must defeat the self-vote — the node is behind and must sync, never false-complete caught-up at genesis")
}

// TestNodeBootstrap_SelfVote_PartialConnectivityFailsSafe proves the self-vote is SAFE against an
// eclipse. The node is a beacon on a 3-validator set, but only ONE of the two OTHER beacons is
// connected (NOT full connectivity). Even though that one + self report genesis, a SUPPRESSED beacon
// could be hiding an ahead-tip — so the self-vote must NOT apply. fullyConnectedBeacons is false →
// the node falls back to the partition-capture floor and reports FrontierConnecting (fail safe),
// exactly as before the fix. self can never tip a PARTIAL view into a false caught-up.
func TestNodeBootstrap_SelfVote_PartialConnectivityFailsSafe(t *testing.T) {
	const N = 5
	chain, byID := buildBSChain(N, -1)
	vm := newBSVM(chain)                                    // node at genesis
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 3) // self + 2 others
	bh.selfNodeID = beacons[0]
	bh.msgCreator = bsMsgBuilder{}
	// ECLIPSE: only ONE of the two OTHER beacons is connected (beacons[1]); beacons[2] is suppressed.
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons[1:2], byID: byID, tip: chain[0]}

	bh.bsActive.Store(true)
	_, status := bh.FrontierTip(context.Background())
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierConnecting, status,
		"partial connectivity (an eclipse could hide an ahead-tip) → the self-vote must NOT apply → fail safe, NOT caught up")
}

// TestRED_PeersTrackNetNotChain_StaleNodeConverges is THE MAINNET-CANARY (luxd-2) intended
// SUCCESS and the regression guard for the beacon-connectivity bug.
//
// THE BUG (canary): the beacon set IS the stake-weighted primary-network validator set, but
// connectedBeacons filtered connected peers by p.TrackedChains.Contains(chainID). Real peers
// advertise the NETS they track (always constants.PrimaryNetworkID + tracked L1 net IDs),
// NEVER an individual native chain ID — so the filter matched ZERO peers, connectedStake
// stayed 0, FrontierTip reported FrontierConnecting forever, and the healthy stale node failed
// safe at the connect deadline despite the ⅔-stake producers being connected and right there.
//
// THE FIX: filter on the NET (b.networkID). Here 3 producers hold > ⅔ of stake and — like
// every real peer — advertise the NET (the handler's networkID), not the chain ID. The stale
// node counts their stake, names the frontier, and converges. C1 is preserved: a peer is
// still counted only if it is in the staked set (weights), and the ⅔-by-stake threshold and
// content-addressed descent are unchanged.
func TestRED_PeersTrackNetNotChain_StaleNodeConverges(t *testing.T) {
	const N = 40 // producer (beacon-named) frontier height
	const M = 23 // our stale local height (gap-17, within the window) — the canary's gap
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M)

	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	o1, o2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	// Producers 100 each (300), two lighter validators 25 each (50): total 350, ⅔ floor = 233.
	// The 3 producers (300) clear it; any 2 of them (200) do not — the quorum genuinely needs
	// the producers' ⅔-stake supermajority, exactly the mainnet shape.
	weights := map[ids.NodeID]uint64{p1: 100, p2: 100, p3: 100, o1: 25, o2: 25}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)

	// Only the 3 producers are connected (the 2 lighter validators are offline). They track the
	// NET (the handler's networkID), not the chain — modeling real peer.Info.TrackedChains.
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3},
		byID: byID, tip: chain[N], serveAncestors: true,
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	// Pin the fix at the unit level: the producers ARE recognized as connected beacons because
	// they track the NET. Pre-fix (filtering on chainID) this returned EMPTY → connectedStake 0.
	w, _, ok := bh.beaconWeights()
	require.True(t, ok, "staked beacon set must be present")
	require.ElementsMatch(t, []ids.NodeID{p1, p2, p3}, bh.connectedBeacons(w),
		"producers tracking the NET must be recognized as connected beacons (the canary fix)")

	// FrontierTip names the frontier from the connected ⅔-stake producers.
	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status,
		"3 producers holding > ⅔ stake, connected + tracking the NET, MUST name the frontier")
	require.Equal(t, chain[N].id, tip)

	// And the full loop converges to N (NOT false-complete at the stale M).
	require.NoError(t, runBS(t, bh), "stake-weighted stale node must converge")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last,
		"CANARY SUCCESS: stale node converges to the producer frontier N=%d, not stuck at M=%d", N, M)
	require.True(t, bh.Accepted(ctx, chain[N].id), "node must hold the producer tip after sync")
}

// TestRED_EmptyStakedSetFailsSafe covers EDGE (4): a native chain whose STAKED validator set
// is empty/unqueryable (not yet loaded by the P-chain, or misconfigured) must WAIT then FAIL
// SAFE — it must NOT conclude caught-up at its stale height, and must NOT fall back to trusting
// a connected non-staked endpoint peer (which would reopen C1). The expectsStakedBeacons flag
// is the discriminator: with it set, an empty set ⇒ FrontierConnecting (wait → fail safe);
// the SAME empty set WITHOUT the flag (single-node / P-chain endpoint-only) ⇒ FrontierNoBeacons
// (complete) — see TestNodeBootstrap_NoBeaconSet_ReportsNoBeacons.
func TestRED_EmptyStakedSetFailsSafe(t *testing.T) {
	const N = 20
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, 5) // stale at height 5

	// EMPTY staked set + expectsStakedBeacons (native non-platform chain on a real network).
	bh, chainID := newBSHandlerWeighted(t, vm, map[ids.NodeID]uint64{})
	require.True(t, bh.expectsStakedBeacons)

	// A connected NON-beacon endpoint peer that names + serves a tip (forged, in general). It is
	// NOT in the staked set, so it must NEVER be trusted to name the frontier.
	endpoint := ids.GenerateTestNodeID()
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: nil, byID: byID, tip: chain[N],
		serveAncestors: true, malicious: endpoint, forgedTip: chain[N].id,
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierConnecting, status,
		"empty staked set (beacons expected) → Connecting (wait→fail safe), NOT NoBeacons (false-complete) or Named (endpoint trust)")
	require.Equal(t, ids.Empty, tip)

	// Discriminator check: the SAME empty set on a node that does NOT expect staked beacons
	// (single-node / P-chain endpoint-only) reports NoBeacons (legitimately "nothing to sync to").
	bh.expectsStakedBeacons = false
	_, status = bh.FrontierTip(ctx)
	require.Equal(t, chainbootstrap.FrontierNoBeacons, status,
		"empty set WITHOUT a staked-beacon expectation → NoBeacons (single-node), proving the flag is the discriminator")
	bh.expectsStakedBeacons = true

	// The full loop fails safe — no endpoint trust, nothing finalized past the stale height.
	err := runBS(t, bh)
	require.ErrorIs(t, err, chainbootstrap.ErrBeaconsUnreachable,
		"empty staked set must fail safe at the connect deadline, never trust endpoints")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[5].id, last, "node must stay at its stale height — no endpoint-named frontier adopted")
	require.False(t, bh.Accepted(ctx, chain[N].id), "no frontier tip may be adopted from a non-staked endpoint")
}

// TestRED_TipSplitConvergesToCommonHeight is THE MAINNET CANARY (luxd-2 on v1.30.78) the
// ancestor-tolerant frontier quorum fixes — restated FAITHFULLY to the responder semantics
// (chains/manager.go answers GetAcceptedFrontier with vm.LastAccepted — the last-ACCEPTED block,
// NEVER an un-finalized preferred tip). The 3 producers hold > ⅔ stake but are SPLIT at the
// bleeding edge of ACTIVE finalization: producer luxd-3 finalized block TOP first, while luxd-1/2
// are momentarily one ACCEPTED block behind at N (TOP's parent). A behind node at M must (1) name
// the ⅔-COMMON committed height N via ancestor tolerance even though no single tip holds ⅔, and
// (2) RATCHET on to the true finalized tip TOP as finalization propagates across the fleet —
// ending caught-up at TOP, never stuck at M, never falsely caught-up below the network frontier.
//
// THE NAMING BUG this guards: the EXACT-tip tally required ⅔-by-stake to report the IDENTICAL id.
// N drew only luxd-1/2 (200 of 300; the floor is `> 200`, so 200 does NOT clear it) and TOP drew
// only luxd-3 (100) — neither cleared ⅔, so FrontierTip returned NoQuorum and the healthy stale
// node failed safe at its stale height. Ancestor tolerance fixes it: a beacon reporting TOP also
// has N in its accepted chain, so N draws ALL THREE producers (300 > 200) and is NAMED.
//
// WHY PROPAGATION, not a STATIC minority-above: a beacon never reports an UN-finalized tip
// (manager.go: vm.LastAccepted), so a PERMANENT "2 at N, 1 at N+1" split on an idle chain cannot
// occur — luxd-3 reporting TOP means TOP is FINALIZED for it, and on a live chain luxd-1/2's
// last-accepted ratchets to TOP too. Modeling it static would assert a node going Ready while a
// genuinely-finalized higher tip is reported — a gap-1 instance of the M1 stale-go-live the
// own-height-exclusion fix closes. The faithful model converges to the TRUE frontier TOP.
func TestRED_TipSplitConvergesToCommonHeight(t *testing.T) {
	const M = 23   // our STALE local height (gap-17 — the canary's gap, within the window)
	const N = 40   // the ⅔-agreed COMMON committed height (all 3 producers have it accepted)
	const top = 41 // luxd-3 finalized this first; luxd-1/2 ratchet to it as finalization propagates
	chain, byID := buildBSChain(top, -1)
	vm := newBSVMAt(chain, M)

	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	weights := map[ids.NodeID]uint64{p1: 100, p2: 100, p3: 100} // total 300, ⅔ floor = 200 (need > 200)
	bh, chainID := newBSHandlerWeighted(t, vm, weights)

	// luxd-3 reports the true finalized tip TOP throughout; luxd-1/2 report their last-accepted N
	// UNTIL the node has accepted through N, then ratchet to TOP (finalization propagated). So while
	// the node is BEHIND it sees {N, N, TOP} (ancestor tolerance names the ⅔-common N), and once it
	// reaches N it sees {TOP, TOP, TOP} (names TOP, the true frontier).
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3},
		byID: byID, tip: chain[top], serveAncestors: true,
		tipFor:            map[ids.NodeID]ids.ID{p1: chain[N].id, p2: chain[N].id},
		tipFor2:           map[ids.NodeID]ids.ID{p1: chain[top].id, p2: chain[top].id},
		propagateAtHeight: N,
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	// Sanity: 2-of-3 producers (200) is exactly the floor, NOT above it — the exact-tip tally
	// genuinely cannot name N. The fix must come from ancestor tolerance, not a looser floor.
	require.Equal(t, uint64(200), consensusconfig.TwoThirdsStakeFloor(300))

	// BEHIND-NODE NAMING (node at M, pre-propagation sees {N, N, TOP}): ancestor tolerance names the
	// ⅔-COMMON N — NOT NoQuorum, NOT the lone-producer TOP (only 100, sub-⅔). This is the canary fix.
	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status,
		"CANARY: a ±1-block tip split must name the ⅔-COMMON height, not return NoQuorum")
	require.Equal(t, chain[N].id, tip, "must name N (the height all 3 producers share), NOT the minority TOP")
	require.NotEqual(t, chain[top].id, tip, "the lone producer's bleeding-edge TOP must NOT be named while sub-⅔")

	// FULL LOOP: the node ratchets M → N (ancestor-tolerant ⅔-common) → TOP (the true finalized tip,
	// once finalization propagated to the fleet). It converges to TOP — never stuck at M, never
	// falsely caught-up at the intermediate N while a genuinely-finalized TOP is the frontier.
	require.NoError(t, runBS(t, bh), "tip-split stale node must converge to the true finalized frontier")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[top].id, last,
		"CANARY SUCCESS: ratchet through the ⅔-common N to the true finalized tip TOP=%d (NOT stuck at M=%d)", top, M)
	require.True(t, bh.Accepted(ctx, chain[N].id), "node must hold the ⅔-common N it passed through")
	require.True(t, bh.Accepted(ctx, chain[top].id), "node must hold the true finalized tip TOP after ratcheting to it")
}

// TestRED_ForgedHighTipFromMinorityNotNamed is the C1 proof for the ancestor-tolerant path: a
// Byzantine BEACON (in the staked set, but < ⅓ stake) reports a FORGED block built directly on
// the real ⅔-backed block N — a forged sibling of the real bleeding-edge TOP. Ancestor tolerance
// must NOT name the forged block (it holds only the Byzantine minority's stake) through the ENTIRE
// ratchet: it names the real ⅔-common N while the node is behind, then the true finalized TOP as
// finalization propagates, and the node must never sync or hold the forged block at any point.
//
// This is the adversarial heart of C1 under the agreement relation: even a forged block that
// descends from a genuinely ⅔-backed block only RATIFIES that real ancestor (which the honest ⅔
// already back) — it can never name ITSELF, because naming requires > ⅔ of the TOTAL staked
// weight behind the block, and the forger holds < ⅓. (Faithful responder model — manager.go:
// vm.LastAccepted: the honest laggards' last-accepted ratchets to the real TOP as finalization
// propagates, while the Byzantine beacon keeps lying with its forged sibling throughout.)
func TestRED_ForgedHighTipFromMinorityNotNamed(t *testing.T) {
	const M = 23   // stale local height
	const N = 40   // the ⅔-common committed height the honest laggards share
	const top = 41 // the real bleeding-edge tip luxd-3 finalized first; the laggards ratchet to it
	chain, byID := buildBSChain(top, -1)
	vm := newBSVMAt(chain, M)

	// A forged block at height TOP whose parent is the REAL N (a forged sibling of the real TOP).
	// The node can PARSE it (any structurally-valid block parses) but has NOT accepted it — model
	// that by adding it to the VM's parse maps but not to `accepted`.
	forged := &bsTestBlock{
		id: ids.GenerateTestID(), parent: chain[N].id, height: top,
		bytes: []byte("forged-sibling@" + strconv.Itoa(top) + ":" + ids.GenerateTestID().String()),
		valid: true,
	}
	byID[forged.id] = forged
	vm.all[forged.id] = forged
	vm.byBytes[string(forged.bytes)] = forged

	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	byz := ids.GenerateTestNodeID()
	// 4 beacons @100 → total 400, ⅔ floor = 266. Honest laggards p1,p2 at N (200) ratcheting to
	// TOP; honest p3 at the real TOP (100); Byzantine byz at the FORGED sibling (100), forever.
	// While behind: no tip clears 266 → ancestor path names the ⅔-common N (forged only ratifies it).
	weights := map[ids.NodeID]uint64{p1: 100, p2: 100, p3: 100, byz: 100}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3, byz},
		byID: byID, tip: chain[top], serveAncestors: true,
		// p3 reports the real TOP (default tip); p1,p2 report N then ratchet to TOP; byz lies (forged) throughout.
		tipFor:            map[ids.NodeID]ids.ID{p1: chain[N].id, p2: chain[N].id, byz: forged.id},
		tipFor2:           map[ids.NodeID]ids.ID{p1: chain[top].id, p2: chain[top].id},
		propagateAtHeight: N,
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	// BEHIND-NODE NAMING (node at M, pre-propagation sees {N, N, realTOP, forgedTOP}): ancestor
	// tolerance names the ⅔-common N — the forged sibling (100) and the real TOP (100) are both
	// sub-⅔ and NEITHER is named. C1 in the ancestor-tolerant tally.
	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status, "the honest ⅔-common N must still be named")
	require.Equal(t, chain[N].id, tip, "C1: the named frontier is the real ⅔-common N")
	require.NotEqual(t, forged.id, tip, "C1: the Byzantine-minority FORGED block must NEVER be named")
	require.NotEqual(t, chain[top].id, tip, "the real bleeding-edge TOP is not named either while sub-⅔")

	// FULL LOOP: the node ratchets M → N → real TOP; the forged sibling is NEVER named or held at any step.
	require.NoError(t, runBS(t, bh), "node converges to the real finalized frontier, never the forged tip")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[top].id, last, "C1: node syncs to the real TOP (the true finalized frontier)")
	require.False(t, bh.Accepted(ctx, forged.id), "C1: the forged block must NOT be finalized — at any point in the ratchet")
	require.True(t, bh.Accepted(ctx, chain[N].id), "node holds the real ⅔-common N it passed through")
}

// TestNodeBootstrap_ExactQuorumUsesFastPath is the no-regression proof: when a ⅔-by-stake
// supermajority DOES report the IDENTICAL tip, FrontierTip names it via the EXACT fast path —
// with NO ancestry fetch. The beacons do NOT serve ancestry (serveAncestors=false); the tip is
// still named, which is only possible if the exact fast path returned before any fetch (a
// fetch would have found nothing and yielded NoQuorum). It also returns promptly.
func TestNodeBootstrap_ExactQuorumUsesFastPath(t *testing.T) {
	const N = 30
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, 10)
	bh, chainID, beacons := newBSHandlerAndEngine(t, vm, 4) // 4 @100 = 400, floor 266; all agree → exact

	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: beacons, byID: byID, tip: chain[N], serveAncestors: false}
	bh.msgCreator = bsMsgBuilder{}

	bh.bsActive.Store(true)
	start := time.Now()
	tip, status := bh.FrontierTip(context.Background())
	elapsed := time.Since(start)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status, "unanimous ⅔ must name the exact tip with no fetch")
	require.Equal(t, chain[N].id, tip)
	require.Less(t, elapsed, bootstrapNamingTimeout, "the exact fast path must not enter the ancestry-fetch resolution")
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
	bh := &blockHandler{
		bsAncestorCh:    make(map[uint32]chan [][]byte),
		bsAncestorPeers: make(map[uint32]set.Set[ids.NodeID]),
	}
	asked := ids.GenerateTestNodeID()

	// Inactive: both hooks are inert (return false → caller uses the live path).
	require.False(t, bh.deliverBootstrapFrontier(ids.GenerateTestNodeID(), ids.GenerateTestID()))
	require.False(t, bh.deliverBootstrapAncestors(asked, 1, nil))

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
	bh.bsAncestorPeers[7] = set.Of(asked)
	entry := encodeCatchupEntry([]byte("blk"), nil)
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(entry)))
	require.True(t, bh.deliverBootstrapAncestors(asked, 7, append(lp[:], entry...)))
	require.Equal(t, [][]byte{[]byte("blk")}, <-ach)

	// A peer we never asked cannot feed this fetch, however well-formed its reply.
	require.False(t, bh.deliverBootstrapAncestors(ids.GenerateTestNodeID(), 7, append(lp[:], entry...)))
}

// TestDeliverBootstrap_UnclaimedReplyFallsThroughToLivePath is the behind-follower
// convergence proof.
//
// bsActive means "initial sync is running", NOT "initial sync owns every Ancestors
// reply on this chain". The live cert catch-up path issues its OWN requests
// concurrently — that is how a node that is merely behind fetches the blocks a
// vote or cert referenced. While this hook claimed every reply unconditionally,
// those live replies were swallowed by the bootstrap lane and the live path never
// saw them, so a behind node fetched the right blocks forever and never applied
// one. Devnet luxd-0 sat at 12639 while its peers ran 16933, logging
// "catch-up response consumed by initial sync — the live cert path did not see it"
// against 1000-block, 430KB replies it had successfully fetched.
//
// A reply is the bootstrap lane's only if the bootstrap lane ASKED for it.
func TestDeliverBootstrap_UnclaimedReplyFallsThroughToLivePath(t *testing.T) {
	bh := &blockHandler{
		bsAncestorCh:    make(map[uint32]chan [][]byte),
		bsAncestorPeers: make(map[uint32]set.Set[ids.NodeID]),
	}
	bh.bsActive.Store(true)

	entry := encodeCatchupEntry([]byte("blk"), nil)
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(entry)))
	data := append(lp[:], entry...)

	// No waiter registered for this requestID: nothing in the bootstrap lane wants
	// this reply, so it must NOT be claimed — the live path is its only consumer.
	peer := ids.GenerateTestNodeID()
	require.False(t, bh.deliverBootstrapAncestors(peer, 99, data),
		"an Ancestors reply the bootstrap lane never requested must fall through to the live cert path")

	// A registered-but-unread waiter must not swallow it either. The send is
	// non-blocking, so claiming the reply here drops the payload AND denies it to
	// the live path — the blocks are lost twice.
	full := make(chan [][]byte) // unbuffered, nobody receiving
	bh.bsAncestorCh[42] = full
	bh.bsAncestorPeers[42] = set.Of(peer)
	require.False(t, bh.deliverBootstrapAncestors(peer, 42, data),
		"a reply that could not be handed to its waiter must fall through, not be dropped")

	// Control: a waiter that CAN take it still consumes it, so this fix does not
	// hand the live path replies that initial sync is legitimately driving.
	ready := make(chan [][]byte, 1)
	bh.bsAncestorCh[43] = ready
	bh.bsAncestorPeers[43] = set.Of(peer)
	require.True(t, bh.deliverBootstrapAncestors(peer, 43, data))
	require.Equal(t, [][]byte{[]byte("blk")}, <-ready)
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
	ready := func() bool { return calls >= 120 }                // done only after 120 polls (≫ the 60ms window)
	outcome, _ := watchBootstrapProgress(ready, nil, nil, heightOf, time.Millisecond, 60*time.Millisecond, nil)
	require.Equal(t, bootstrapReady, outcome, "a slow-but-ADVANCING sync must complete, not be timed out")
}

// TestWatchBootstrapProgress_GenuineStallFails: a sync whose height does NOT advance for
// the whole window is failed (a real stall is still surfaced, not masked).
func TestWatchBootstrapProgress_GenuineStallFails(t *testing.T) {
	heightOf := func() uint64 { return 5 } // pinned — no progress
	ready := func() bool { return false }
	start := time.Now()
	outcome, last := watchBootstrapProgress(ready, nil, nil, heightOf, time.Millisecond, 60*time.Millisecond, nil)
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
		func() bool { return false }, nil, nil, func() uint64 { return 0 },
		time.Millisecond, time.Hour, shutdown,
	)
	require.Equal(t, bootstrapShutdown, outcome)
}

// TestWatchBootstrapProgress_FailSafeSurfacesPromptly is the F5 proof: when the sync loop
// RETURNS a fail-safe error, the failed() predicate flips and the watchdog returns
// bootstrapFailed on the NEXT tick — NOT after the (here, 1-hour) no-progress window. This is
// what removes the dead zone where the chain was neither syncing nor stopped: monitorBootstrap
// gets the signal to stop the chain the instant Run returns.
func TestWatchBootstrapProgress_FailSafeSurfacesPromptly(t *testing.T) {
	failed := func() bool { return true } // the loop has already returned a fail-safe error
	start := time.Now()
	outcome, _ := watchBootstrapProgress(
		func() bool { return false }, // not ready
		failed,
		nil,                        // not connecting
		func() uint64 { return 7 }, // height pinned — would otherwise wait the full stall window
		time.Millisecond, time.Hour, nil,
	)
	require.Equal(t, bootstrapFailed, outcome, "a fail-safe RETURN must surface as bootstrapFailed, not stall")
	require.Less(t, time.Since(start), 500*time.Millisecond, "must surface PROMPTLY, not after the no-progress watchdog")
}

// TestWatchBootstrapProgress_ReadyBeatsFailed: if the sync completed in the SAME tick the
// predicates are evaluated, ready wins (success is never masked by a stale failure flag).
func TestWatchBootstrapProgress_ReadyBeatsFailed(t *testing.T) {
	outcome, _ := watchBootstrapProgress(
		func() bool { return true }, // ready
		func() bool { return true }, // and failed — ready must take precedence
		nil,                         // connecting (irrelevant — ready wins)
		func() uint64 { return 0 },
		time.Millisecond, time.Hour, nil,
	)
	require.Equal(t, bootstrapReady, outcome, "ready must take precedence over a failed flag")
}

// TestBootstrapFailure_AccessorSurfacesReason proves the F5 plumbing the driver↔monitor share:
// runBootstrapThenPoll stores the fail-safe reason, and the BootstrapFailed/BootstrapFailure
// accessors (which monitorBootstrap polls) surface it race-free. Before a failure both report
// "no failure"; after, they report the exact error (so the health check carries the real cause).
func TestBootstrapFailure_AccessorSurfacesReason(t *testing.T) {
	bh := &blockHandler{}
	require.False(t, bh.BootstrapFailed(), "a fresh handler has not failed")
	require.NoError(t, bh.BootstrapFailure(), "no reason before a failure")

	bh.bootstrapFailed.Store(&bsFailure{err: chainbootstrap.ErrNoBeaconQuorum})
	require.True(t, bh.BootstrapFailed(), "after a fail-safe return BootstrapFailed must be true")
	require.ErrorIs(t, bh.BootstrapFailure(), chainbootstrap.ErrNoBeaconQuorum, "the precise fail-safe reason must surface for the health check")
}

// ----- VM normal-operation GATING (the restart-freeze fix) -------------------
//
// These prove the ORDERING fix in runInitialSync: the VM transitions to NORMAL OPERATION
// (vm.Ready → the EVM's onNormalOperationsStarted: block building / mempool / validator
// dispatch) ONLY AFTER initial sync has fetch+executed up to the network frontier — never at
// the LOCAL (possibly stale) height. Before the fix buildChain put the VM into normal
// operation UNCONDITIONALLY right after Initialize, so a restarted STALE validator served /
// built at its stale height and never converged (the luxd-2 mainnet freeze: stuck at 1082780
// while producers finalized 1082796). vmHeightAtReady records the height the VM was at the
// instant it was told to go live; the gate makes that the frontier, not the stale tip.

// recordVMReady wires bh.vmReady to capture (a) how many times the VM was transitioned to
// normal operation and (b) the VM's accepted height at the FIRST such transition — the two
// facts the gating invariant turns on.
func recordVMReady(bh *blockHandler, vm *bsTestVM) (calls *int, heightAtReady *uint64) {
	calls = new(int)
	heightAtReady = new(uint64)
	bh.vmReady = func(context.Context) error {
		if *calls == 0 {
			*heightAtReady = vm.all[vm.lastAcc].height
		}
		*calls++
		return nil
	}
	return calls, heightAtReady
}

// TestRED_StaleValidatorGoesReadyOnlyAfterReachingFrontier reproduces the EXACT production
// scenario: a node with stale local state (height N) restarts against a live network whose
// finalized frontier is N+k, validator set loaded. It MUST fetch+execute to N+k BEFORE the VM
// is transitioned to normal operation — never go live at the stale N.
//
// FAILS before the fix (the VM was transitioned to normal operation at N, in buildChain,
// before this sync ran) and PASSES after (the transition is gated on reaching N+k).
func TestRED_StaleValidatorGoesReadyOnlyAfterReachingFrontier(t *testing.T) {
	const N, K = 30, 16 // stale at 30; live frontier at 46 — the luxd-2 16-block gap shape
	chain, byID := buildBSChain(N+K, -1)
	vm := newBSVMAt(chain, N) // node ALREADY at height N (restarted spare), gap N+1..N+K ahead

	// 5-validator equal-stake beacon set (the mainnet shape), all connected and serving — the
	// "validator set IS loaded, 9 peers incl. producers" live condition.
	bIDs := make([]ids.NodeID, 5)
	weights := map[ids.NodeID]uint64{}
	for i := range bIDs {
		bIDs[i] = ids.GenerateTestNodeID()
		weights[bIDs[i]] = 100
	}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.bootstrapRetryInterval = 2 * time.Millisecond
	bh.bootstrapConnectDeadline = 2 * time.Second
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: bIDs, byID: byID, tip: chain[N+K], serveAncestors: true}
	bh.msgCreator = bsMsgBuilder{}

	calls, heightAtReady := recordVMReady(bh, vm)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	live := bh.runInitialSync(ctx)

	require.True(t, live, "node must reach the frontier and go live")
	require.True(t, bh.bootstrapDone.Load(), "chain must be marked bootstrapped after reaching the frontier")
	require.False(t, bh.BootstrapFailed(), "a converged sync must not record a fail-safe reason")

	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N+K].id, last, "VM must fetch+execute up to the network frontier (height %d)", N+K)

	require.Equal(t, 1, *calls, "VM must be transitioned to normal operation exactly once")
	require.Equal(t, uint64(N+K), *heightAtReady,
		"THE FIX: VM must go to normal operation ONLY at the frontier (height %d), never at the stale local height %d", N+K, N)
}

// TestNodeBootstrap_FreshGenesisNoBeaconsReadyImmediately is the NO-REGRESSION guard for a
// fresh network / single node: NO beacon set (FrontierNoBeacons) means there is nothing ahead
// to sync to, so the VM must go to normal operation PROMPTLY at genesis. The gate must not pin
// a legitimate fresh-genesis / dev node unbootstrapped.
func TestNodeBootstrap_FreshGenesisNoBeaconsReadyImmediately(t *testing.T) {
	chain, byID := buildBSChain(0, -1) // genesis only
	vm := newBSVM(chain)
	bh, chainID, _ := newBSHandlerAndEngine(t, vm, 0) // 0 beacons, expectsStakedBeacons=false
	bh.bootstrapRetryInterval = 2 * time.Millisecond
	bh.bootstrapConnectDeadline = 2 * time.Second
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: nil, byID: byID, tip: chain[0]}
	bh.msgCreator = bsMsgBuilder{}

	calls, heightAtReady := recordVMReady(bh, vm)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	live := bh.runInitialSync(ctx)

	require.True(t, live, "a no-beacon (fresh genesis / single node) chain must go live")
	require.True(t, bh.bootstrapDone.Load(), "fresh-genesis chain must be marked bootstrapped")
	require.Less(t, time.Since(start), 2*time.Second, "no-beacon fast path must not wait out the connect deadline")
	require.Equal(t, 1, *calls, "VM must be transitioned to normal operation once")
	require.Equal(t, uint64(0), *heightAtReady, "fresh-genesis node goes live at genesis (height 0)")
}

// TestRED_EclipsedValidatorNeverGoesReadyAtStaleHeight is the FAIL-SAFE proof: a stale node
// whose beacons are UNREACHABLE (eclipse / isolation) must NOT go to normal operation at its
// stale height (that was the freeze defect). Within the bounded connect deadline it fails safe
// — VM stays in Bootstrapping (vmReady never called), bootstrapFailed records the reason — and
// it does NOT hang (the deadline bounds it). monitorBootstrap then stops the chain; the
// orchestrator restarts and the node re-syncs.
func TestRED_EclipsedValidatorNeverGoesReadyAtStaleHeight(t *testing.T) {
	const N, K = 30, 16
	chain, byID := buildBSChain(N+K, -1)
	vm := newBSVMAt(chain, N)

	bIDs := make([]ids.NodeID, 5)
	weights := map[ids.NodeID]uint64{}
	for i := range bIDs {
		bIDs[i] = ids.GenerateTestNodeID()
		weights[bIDs[i]] = 100
	}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.bootstrapRetryInterval = 2 * time.Millisecond
	bh.bootstrapConnectDeadline = 300 * time.Millisecond // bounded fail-safe window for the test
	// Pin a SINGLE attempt so this asserts the TERMINAL fail-safe in isolation (the connectivity
	// self-heal RETRY — bootstrapMaxAttempts ≤ 0 ⇒ retry until the quorum returns — is proven
	// separately in TestRED_MajorityOutageSelfHealsWhenQuorumReturns). The SAFETY property here (an
	// eclipsed node NEVER goes live at its stale height) holds identically under one attempt or many.
	bh.bootstrapMaxAttempts = 1
	// connected: nil — the beacon set is configured (weights known) but NONE are reachable.
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: nil, byID: byID, tip: chain[N+K], serveAncestors: true}
	bh.msgCreator = bsMsgBuilder{}

	calls, _ := recordVMReady(bh, vm)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	live := bh.runInitialSync(ctx)
	elapsed := time.Since(start)

	require.False(t, live, "an eclipsed node must NOT go live")
	require.Equal(t, 0, *calls, "THE FIX: VM must NEVER be transitioned to normal operation at the stale height when beacons are unreachable")
	require.False(t, bh.bootstrapDone.Load(), "an eclipsed node must not be marked bootstrapped")
	require.True(t, bh.BootstrapFailed(), "the fail-safe reason must be recorded for monitorBootstrap")
	require.ErrorIs(t, bh.BootstrapFailure(), chainbootstrap.ErrBeaconsUnreachable, "eclipse must surface as ErrBeaconsUnreachable")

	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last, "the VM must stay at its stale height (it served nothing, never advanced)")
	require.Less(t, elapsed, 3*time.Second, "fail-safe must be BOUNDED — no unbounded hang")
}

// TestNodeBootstrap_ProducerAtTipReadyFast is the PRODUCER hard-constraint guard: a producer
// at (or at most one block from) the frontier restarts. It must re-bootstrap QUICKLY and go
// live — not hang waiting to sync blocks it already has. Here the node is at N and the beacon
// quorum names N (it already holds the tip): the loop is caught-up on the first round and the
// VM goes live at N promptly.
func TestNodeBootstrap_ProducerAtTipReadyFast(t *testing.T) {
	const N = 40
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, N) // already at the tip

	bIDs := make([]ids.NodeID, 5)
	weights := map[ids.NodeID]uint64{}
	for i := range bIDs {
		bIDs[i] = ids.GenerateTestNodeID()
		weights[bIDs[i]] = 100
	}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.bootstrapRetryInterval = 2 * time.Millisecond
	bh.bootstrapConnectDeadline = 2 * time.Second
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: bIDs, byID: byID, tip: chain[N], serveAncestors: true}
	bh.msgCreator = bsMsgBuilder{}

	calls, heightAtReady := recordVMReady(bh, vm)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	live := bh.runInitialSync(ctx)

	require.True(t, live, "a producer at the tip must go live")
	require.Less(t, time.Since(start), 2*time.Second, "an at-the-tip restart must be FAST — not hang")
	require.Equal(t, 1, *calls, "VM transitioned to normal operation once")
	require.Equal(t, uint64(N), *heightAtReady, "producer goes live at the frontier (== its tip, height %d)", N)
	require.True(t, bh.bootstrapDone.Load(), "producer chain marked bootstrapped")
}

// TestRED_TipHolderCoRestartGoesReadyAtOwnTip is THE CRITICAL regression RED found and the hinge of
// this round. A PRODUCER at the tip N, on a simultaneous co-restart, sees the production fleet SPLIT
// across heights: 2 other producers at N, one node at N-16, one at genesis. The tip-holders are only
// ½ of the 4 responders (< ⅔), so NO frontier is NAMED, and the ⅔-backed common ancestor N-16 is
// below the node's own last-accepted (MinFrontierHeight filters it). WITHOUT the caught-up
// determination this producer has no go-live path and fails safe DOWN at its own tip — the OPPOSITE
// of the stale-go-live bug, and just as wrong.
//
// FAILS on 5d3aaeb5a0 (FrontierTip → FrontierNoQuorum → ErrNoBeaconQuorum → live==false); PASSES
// after: CaughtUp proves the node is at the top of every responder and holds every reported tip, so
// the network frontier IS the node's own accepted tip → the loop's Has()-shortcut → Ready at N, once.
func TestRED_TipHolderCoRestartGoesReadyAtOwnTip(t *testing.T) {
	const N = 80   // the producer's tip (models the mainnet 1082796 — the RELATIVE shape is the point)
	const gap = 16 // luxd-2's lag (the canary's 16-block gap → N-16)
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, N) // the PRODUCER holds its full accepted chain 0..N (a co-restart spare)

	// 5 equal-weight beacons (the mainnet shape). The node is the 5th; its 4 PEERS report the SPLIT:
	// two producers at the tip N, luxd-2 at N-16, luxd-0 at genesis.
	bIDs := make([]ids.NodeID, 5)
	weights := map[ids.NodeID]uint64{}
	for i := range bIDs {
		bIDs[i] = ids.GenerateTestNodeID()
		weights[bIDs[i]] = 100
	}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.bootstrapRetryInterval = 2 * time.Millisecond
	bh.bootstrapConnectDeadline = 2 * time.Second
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: bIDs[:4], byID: byID, tip: chain[N], serveAncestors: true,
		tipFor: map[ids.NodeID]ids.ID{
			bIDs[2]: chain[N-gap].id, // luxd-2 at N-16
			bIDs[3]: chain[0].id,     // luxd-0 at genesis
		},
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	// UNIT: the tip-holders (b0,b1 = 200) do NOT clear ⅔ (266 of 400) and the ⅔-common N-16 is below
	// MinFrontierHeight=N → no frontier is NAMED; the caught-up path names the node's OWN held tip.
	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status,
		"CAUGHT-UP: a tip-holder that holds every reported tip and is at the top of all of them must go Ready, not NoQuorum")
	require.Equal(t, chain[N].id, tip, "the caught-up frontier is the node's OWN accepted tip N")

	// FULL LOOP: the producer goes live at its OWN tip N — exactly once, never below it, never hung.
	calls, heightAtReady := recordVMReady(bh, vm)
	rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	live := bh.runInitialSync(rctx)

	require.True(t, live, "THE FIX: a tip-holding producer on a mixed-height co-restart must go Ready at its own tip")
	require.Less(t, time.Since(start), 2*time.Second, "a caught-up producer must go live PROMPTLY, not hang on the no-quorum retry")
	require.Equal(t, 1, *calls, "VM transitioned to normal operation exactly once")
	require.Equal(t, uint64(N), *heightAtReady, "went live at its OWN tip N=%d, never below it", N)
	require.True(t, bh.bootstrapDone.Load(), "tip-holder chain marked bootstrapped")
	require.False(t, bh.BootstrapFailed(), "a caught-up producer must NOT record a fail-safe reason")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last, "the producer stays at its tip N — it does NOT sync backward to the ⅔-common N-16")
}

// TestRED_MajorityOutageSelfHealsWhenQuorumReturns is the RED MEDIUM #2 self-heal proof. A node boots
// into a MAJORITY OUTAGE (its beacon quorum unreachable — a co-restart where < MinResponses are up).
// It must NOT go live at its stale height (safety) and must NOT permanently give up (liveness): it
// stays in Bootstrapping, RE-ATTEMPTS (bootstrapMaxAttempts ≤ 0 ⇒ until the quorum returns), and
// CONVERGES the instant the quorum comes back — all IN-PROCESS, with no pod restart (the K8s probes
// poll the always-green /v1/health/liveness and would never restart it). This is the bounded
// re-bootstrap retry that closes the "permanent brick" RED flagged.
func TestRED_MajorityOutageSelfHealsWhenQuorumReturns(t *testing.T) {
	const N, K = 30, 16 // stale at N; the live frontier (once the quorum returns) is N+K
	chain, byID := buildBSChain(N+K, -1)
	vm := newBSVMAt(chain, N)

	bIDs := make([]ids.NodeID, 5)
	weights := map[ids.NodeID]uint64{}
	for i := range bIDs {
		bIDs[i] = ids.GenerateTestNodeID()
		weights[bIDs[i]] = 100
	}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.bootstrapRetryInterval = 2 * time.Millisecond
	bh.bootstrapConnectDeadline = 80 * time.Millisecond // short per-attempt connect wait (test bound)
	// bootstrapMaxAttempts left 0 ⇒ UNLIMITED retry — the production self-heal.

	net := &bsBeaconNet{bh: bh, chainID: chainID, connected: bIDs, byID: byID, tip: chain[N+K], serveAncestors: true}
	net.gate.Store(true) // start in the OUTAGE: the quorum is unreachable (a majority co-restart)
	bh.net = net
	bh.msgCreator = bsMsgBuilder{}

	calls, heightAtReady := recordVMReady(bh, vm)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- bh.runInitialSync(ctx) }()

	// While the quorum is down the node must fail safe DOWN and ENTER its connectivity-retry wait —
	// NOT go live at the stale height, NOT give up. (Asserted via the ATOMIC status — no non-atomic
	// read of the bootstrap goroutine's state while it runs.)
	require.Eventually(t, bh.BootstrapConnecting, 3*time.Second, 2*time.Millisecond,
		"an outage must put the node into the connectivity-retry wait (failing safe DOWN), not live or given-up")
	require.False(t, bh.bootstrapDone.Load(), "must NOT be bootstrapped while the quorum is unreachable")
	require.False(t, bh.BootstrapFailed(), "a retryable connectivity outage is NOT a terminal fail-safe — it keeps trying")

	// The quorum RETURNS. The node must SELF-HEAL in-process: converge to the frontier and go live.
	net.gate.Store(false)

	require.True(t, <-done, "node must self-heal and go live once the quorum returns") // happens-before for the reads below
	require.True(t, bh.bootstrapDone.Load(), "chain marked bootstrapped after self-heal")
	// calls==1 (exactly one go-live, TOTAL) proves the VM never went live during the outage — had it,
	// the recovery go-live would make this 2. heightAtReady==N+K proves that single go-live was at the
	// recovered frontier, never the stale N.
	require.Equal(t, 1, *calls, "VM transitioned to normal operation exactly once — at the frontier, after recovery")
	require.Equal(t, uint64(N+K), *heightAtReady, "self-healed node converges to the frontier N+K=%d, never the stale N=%d", N+K, N)
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N+K].id, last, "self-healed node holds the frontier tip after recovery")
}

// TestRED_EclipseOwnHeightNotNamedRoutesToSync is the M1 fast-follow at the INTEGRATION level (the
// real FrontierTip routing + full-loop go-live decision). A node at accepted height N restarts while
// block N+5 is GENUINELY FINALIZED ahead, but a sustained eclipse THROTTLES the ahead responders below
// the ⅔ naming threshold (R_a=300 of a responder weight 500; ⅔ floor 333) while letting the at-height
// responders (R_b=200 at N) through. Block N then accrues R_a+R_b=500 > ⅔ purely as the shared
// ANCESTOR of the ahead N+5 tips.
//
//   - BEFORE the own-height filter fix (`ref.Height < MinFrontierHeight`): nameFrontier NAMES N (own
//     height) → FrontierNamed at chain[N] → the node goes Ready STALE, 5 blocks behind finality. M1.
//   - AFTER (`ref.Height <= MinFrontierHeight`): own height is excluded → AcceptsFrontier names nothing
//     → routes to CaughtUp → CaughtUp sees the un-held, above N+5 tips → REFUSES → FrontierNoQuorum.
//     The full loop fails safe: the VM is NEVER transitioned to normal operation at the stale height,
//     and the node stays at N (serving nothing) rather than going live 5 blocks behind finality.
//
// Then the ECLIPSE LIFTS (the ahead set returns to ≥⅔): N+5 becomes nameable and the node SYNCS UP to
// it — proving the refusal was a safe HOLD, not a permanent stall, and the node converges to the true
// finalized frontier, never having served the stale N.
func TestRED_EclipseOwnHeightNotNamedRoutesToSync(t *testing.T) {
	const N, ahead = 40, 45 // node at N; block N+5 genuinely finalized ahead
	chain, byID := buildBSChain(ahead, -1)

	// 6 equal-weight beacons (total 600 → bootstrapPolicy wires MinResponseWeight ⌈600/2⌉=301,
	// MinResponses majority=4, MinFrontierHeight=lastAccepted=N).
	bIDs := make([]ids.NodeID, 6)
	weights := map[ids.NodeID]uint64{}
	for i := range bIDs {
		bIDs[i] = ids.GenerateTestNodeID()
		weights[bIDs[i]] = 100
	}

	// ---- PART A: the eclipse — ahead set throttled below ⅔ → must NOT go Ready at stale N ----
	vmA := newBSVMAt(chain, N) // node already accepted 0..N, NOT N+1..N+5
	bhA, chainIDA := newBSHandlerWeighted(t, vmA, weights)
	bhA.bootstrapRetryInterval = 2 * time.Millisecond
	bhA.bootstrapConnectDeadline = 300 * time.Millisecond
	bhA.bootstrapMaxAttempts = 1 // assert the terminal fail-safe in isolation
	// Connected responders: b0,b1 at N (R_b=200), b2,b3,b4 at N+5 (R_a=300, < ⅔·500=333); b5 eclipsed.
	bhA.net = &bsBeaconNet{
		bh: bhA, chainID: chainIDA, connected: bIDs[:5], byID: byID, tip: chain[N], serveAncestors: true,
		tipFor: map[ids.NodeID]ids.ID{
			bIDs[2]: chain[ahead].id, bIDs[3]: chain[ahead].id, bIDs[4]: chain[ahead].id,
		},
	}
	bhA.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	// UNIT: FrontierTip must NOT name the stale own height N. Own height is excluded from nameFrontier,
	// the ahead N+5 is sub-⅔, and CaughtUp refuses (N+5 un-held, above) → FrontierNoQuorum.
	bhA.bsActive.Store(true)
	tip, status := bhA.FrontierTip(ctx)
	bhA.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNoQuorum, status,
		"M1: an eclipse crediting the node's OWN height as a ⅔-ancestor of throttled ahead tips must NOT name it")
	require.NotEqual(t, chain[N].id, tip, "M1: the stale own height N must NOT be named")
	require.Equal(t, ids.Empty, tip)

	// FULL LOOP: the node fails safe DOWN — VM never goes to normal operation at the stale height.
	callsA, _ := recordVMReady(bhA, vmA)
	rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	live := bhA.runInitialSync(rctx)
	require.False(t, live, "M1: an eclipse-stale node must NOT go live at its own height")
	require.Equal(t, 0, *callsA, "M1: VM must NEVER be transitioned to normal operation at the stale height")
	require.False(t, vmA.accepted[chain[ahead].id], "node did not (and could not) sync the throttled N+5 under eclipse")
	lastA, _ := vmA.LastAccepted(ctx)
	require.Equal(t, chain[N].id, lastA, "node stays at its stale height N (served nothing) — never live, never advanced")

	// ---- PART B: the eclipse lifts — ahead set returns to ≥⅔ → node SYNCS UP to the true frontier ----
	vmB := newBSVMAt(chain, N)
	bhB, chainIDB := newBSHandlerWeighted(t, vmB, weights)
	bhB.bootstrapRetryInterval = 2 * time.Millisecond
	bhB.bootstrapConnectDeadline = 2 * time.Second
	// Now 4 of 5 responders report N+5 (400 > ⅔·500=333 → nameable); one lags at N. The ahead set is
	// no longer throttled, so N+5 is named and the node descends+executes N+1..N+5.
	bhB.net = &bsBeaconNet{
		bh: bhB, chainID: chainIDB, connected: bIDs[:5], byID: byID, tip: chain[ahead], serveAncestors: true,
		tipFor: map[ids.NodeID]ids.ID{bIDs[0]: chain[N].id}, // one laggard at N; b1..b4 report N+5 (default tip)
	}
	bhB.msgCreator = bsMsgBuilder{}

	callsB, heightAtReadyB := recordVMReady(bhB, vmB)
	bctx, bcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bcancel()
	require.True(t, bhB.runInitialSync(bctx), "eclipse lifted: the node must converge to the true finalized frontier")
	require.Equal(t, 1, *callsB, "VM goes live exactly once — at the recovered frontier, never the stale N")
	require.Equal(t, uint64(ahead), *heightAtReadyB, "went live at the true finalized frontier N+5=%d, never the stale N=%d", ahead, N)
	lastB, _ := vmB.LastAccepted(ctx)
	require.Equal(t, chain[ahead].id, lastB, "node synced UP to N+5 once the eclipse lifted (a safe HOLD, not a permanent stall)")
}

// TestWatchBootstrapProgress_ConnectingNotStalled is the RED MEDIUM #2 watchdog guard: a node in its
// connectivity-RETRY wait reports connecting()==true, so the no-progress watchdog must NOT declare it
// stalled — force-STOPPING a node that is correctly waiting for its quorum would be a permanent brick
// (the K8s probes never restart it). The clock is reset while connecting; only a NON-connecting
// no-progress node stalls (the converse is TestWatchBootstrapProgress_GenuineStallFails).
func TestWatchBootstrapProgress_ConnectingNotStalled(t *testing.T) {
	shutdown := make(chan struct{})
	go func() { time.Sleep(120 * time.Millisecond); close(shutdown) }() // ~6 stall windows later
	outcome, _ := watchBootstrapProgress(
		func() bool { return false }, // never ready
		func() bool { return false }, // never a terminal fail
		func() bool { return true },  // ALWAYS connecting (deliberate quorum wait)
		func() uint64 { return 5 },   // height pinned — no progress
		time.Millisecond, 20*time.Millisecond, shutdown,
	)
	require.Equal(t, bootstrapShutdown, outcome,
		"a CONNECTING node must NEVER be stalled out (it is waiting for its quorum) — only shutdown ends it")
}

// ============================================================================
// PRODUCTION-MODELING REPRO (mainnet luxd-2 freeze, v1.30.84 → fix/bootstrap-vm-ready-ordering)
//
// The v1.30.84 fix passed 50 unit tests + a BFT proof but FAILED the real mainnet canary: a
// STALE spare (luxd-2) at C-Chain accepted height N went Ready at N instead of fetching the 16
// blocks to the producers' frontier Top. The mocks injected the FULL, STABLE validator set from
// t=0, masking the real trigger: a native chain's bootstrap goroutine starts right after its
// VM.Initialize — BEFORE the P-chain has finished replaying its blocks — so the staked beacon set
// (m.Validators) is a PARTIAL set at boot. The MinResponseWeight stake-majority floor (total/2+1)
// is fail-secure ONLY when `total` is the TRUE full-set stake; under a partial denominator a
// degenerate at-or-below responder subset (the genuinely-ahead producers not yet loaded) clears
// the floor and the frontier decision concludes "caught up" at the stale local height.
//
// These tests model the REAL conditions (not the ideal full set): not-held producer tips, an
// empty ancestry fetch, a peer-connect delay, and — the headline — a PARTIAL staked set at boot.
// ============================================================================

// TestRED_PartialStakedSet_BehindNodeMustNotGoLiveStale is THE production reproduction. It models
// the staked validator set being PARTIAL while the P-chain is still syncing (the producers not yet
// loaded), and proves:
//   - WITHOUT the P-ready gate (the f882142c81 behavior): the partial at-or-below responder subset
//     clears the floor and FrontierTip NAMES the node's OWN stale tip → go-live-at-stale (the freeze).
//   - WITH the gate: a native chain WAITS (FrontierConnecting) for the staked set to load, never
//     concluding caught-up on a non-representative partial set.
//   - Once the P-chain finishes (full set, producers now loaded at Top), the node names Top and
//     converges — never stopping at its stale N.
func TestRED_PartialStakedSet_BehindNodeMustNotGoLiveStale(t *testing.T) {
	const M = 40   // our STALE local height (luxd-2's 1082780, scaled)
	const Top = 56 // producers' real accepted frontier (gap 16 — the canary's 16 blocks)
	chain, byID := buildBSChain(Top, -1)
	vm := newBSVMAt(chain, M) // behind by 16

	// The PARTIAL staked set visible while the P-chain is still replaying: only the two stale spares
	// (at M) are loaded; the three genuinely-ahead producers (at Top) are NOT in the set yet. Both
	// spares report the node's OWN tip chain[M] (they are at M too) — a stake-MAJORITY of the PARTIAL
	// set, so the floor is met and the exact-fast-path would name chain[M], concluding caught-up STALE.
	spareA, spareB := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	bh, chainID := newBSHandlerWeighted(t, vm, map[ids.NodeID]uint64{spareA: 100, spareB: 100})
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{spareA, spareB},
		byID: byID, tip: chain[M], serveAncestors: true, // both partial-set beacons report our OWN tip
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	// (1) NO GATE == f882142c81: the partial set false-names the stale own tip — the production freeze.
	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status,
		"REPRO: without the P-ready gate a PARTIAL staked set false-names the stale own tip (go-live-at-stale)")
	require.Equal(t, chain[M].id, tip, "REPRO: the named 'frontier' is the node's OWN stale tip — the luxd-2 freeze")

	// (2) WITH THE GATE: staked set not yet fully loaded (P-chain still syncing) → WAIT, never caught-up.
	var pReady atomic.Bool // false until the P-chain finishes its own initial sync
	bh.primaryNetworkReady = pReady.Load
	bh.bsActive.Store(true)
	tip, status = bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierConnecting, status,
		"FIX: a behind node must WAIT for the FULL staked set, not conclude caught-up on a partial one")
	require.Equal(t, ids.Empty, tip)

	// (3) P-chain finished → the FULL set is now visible (the three ahead producers loaded, only THEY
	// connected). They report Top (NOT held by the node at M). The node must NAME Top and sync to it,
	// never stop at its stale M.
	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	full := validators.NewManager()
	for id, w := range map[ids.NodeID]uint64{spareA: 100, spareB: 100, p1: 100, p2: 100, p3: 100} {
		require.NoError(t, full.AddStaker(bh.networkID, id, nil, ids.GenerateTestID(), w))
	}
	bh.beacons = full
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3}, // the ⅔-stake producers, at Top
		byID: byID, tip: chain[Top], serveAncestors: true,
	}
	pReady.Store(true)
	require.NoError(t, runBS(t, bh), "with the full staked set the behind node must converge to the producers' frontier")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[Top].id, last,
		"behind node must sync to the producers' real frontier Top=%d, NEVER stop at its stale M=%d", Top, M)
}

// TestNodeBootstrap_BehindNodeNotHeldTips_SyncsToFrontier (variant A). A behind node at M, the FULL
// staked set loaded (P-ready), the ⅔-stake producers reporting Top which the node does NOT hold,
// ancestry served. CaughtUp's held-check fails (the producer tip is not held), so the node names
// the real higher frontier and fetch+executes to it — Ready at Top, never at its stale M.
func TestNodeBootstrap_BehindNodeNotHeldTips_SyncsToFrontier(t *testing.T) {
	const M = 40
	const Top = 56 // gap 16, all NOT held by the node at M
	chain, byID := buildBSChain(Top, -1)
	vm := newBSVMAt(chain, M)

	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	bh, chainID := newBSHandlerWeighted(t, vm, map[ids.NodeID]uint64{p1: 100, p2: 100, p3: 100})
	var pReady atomic.Bool
	pReady.Store(true) // P-chain synced: full staked set
	bh.primaryNetworkReady = pReady.Load
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3},
		byID: byID, tip: chain[Top], serveAncestors: true,
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	require.False(t, bh.Accepted(ctx, chain[Top].id), "precondition: the producers' tip Top is NOT held by the behind node")
	require.NoError(t, runBS(t, bh), "behind node must descend the not-held frontier and converge")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[Top].id, last, "behind node must reach Top=%d, never stop at stale M=%d", Top, M)
	require.True(t, bh.Accepted(ctx, chain[Top].id), "node must hold the producer tip after sync")
}

// TestNodeBootstrap_StoredButUnacceptedFrontier_DrivesAcceptance is the NODE-LAYER reproduction of
// THE mainnet luxd-2 freeze with production ground truth — the case the prior mocks could not even
// express (their VM conflated store and acceptance). A behind node at M holds blocks M+1..Top
// ALREADY IN ITS STORE (gossiped / a prior incomplete sync) but UNACCEPTED, and the ⅔-stake
// producers name the frontier at Top — a tip the node HOLDS but has NOT accepted. THE BUG: the
// node-side Has()/heldHeight reported "present" for those stored-but-unaccepted blocks, the loop's
// Has()-shortcut concluded caught-up at the stale M, and the node went Ready at M forever (self-
// reinforcing across restarts — every reboot re-concluded caught-up). THE FIX: Accepted(Top) is
// false (present ≠ accepted), so the node descends and ACCEPTS M+1..Top — through the per-height-
// guarded cert/Verify path, accepting the blocks already in the store — reaching Top, never Ready
// at the stale M.
func TestNodeBootstrap_StoredButUnacceptedFrontier_DrivesAcceptance(t *testing.T) {
	const M = 40
	const Top = 56 // gap 16, the exact ground-truth skew (1082780 → 1082796)
	chain, byID := buildBSChain(Top, -1)
	vm := newBSVMAt(chain, M)
	// GROUND TRUTH: M+1..Top are PRESENT IN THE STORE (gossiped ahead) but UNACCEPTED.
	vm.store(chain[M+1 : Top+1]...)

	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	bh, chainID := newBSHandlerWeighted(t, vm, map[ids.NodeID]uint64{p1: 100, p2: 100, p3: 100})
	var pReady atomic.Bool
	pReady.Store(true) // P-chain synced: full staked set
	bh.primaryNetworkReady = pReady.Load
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3},
		byID: byID, tip: chain[Top], serveAncestors: true,
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	// Precondition — the EXACT freeze state: the node HOLDS the named frontier in its store yet has
	// NOT accepted it (last-accepted is M). The store-vs-acceptance distinction the conflation missed.
	require.True(t, vm.stored[chain[Top].id], "precondition: the frontier must be present in the store (gossiped ahead)")
	require.False(t, bh.Accepted(ctx, chain[Top].id), "precondition: the held-in-store frontier must NOT be accepted (last-accepted is M)")

	require.NoError(t, runBS(t, bh), "node must descend+accept the stored-but-unaccepted frontier, not freeze at M")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[Top].id, last,
		"FREEZE: node must reach Top=%d, never go Ready at the stale M=%d while holding M+1..Top unaccepted", Top, M)
	require.True(t, bh.Accepted(ctx, chain[Top].id), "the named frontier must be ACCEPTED after sync")
}

// TestBootstrap_AcceptedHeight_StoreVsAcceptance pins the ACCEPTANCE oracle the whole fix turns on:
// acceptedHeight / Accepted report a block ACCEPTED only when it is on the FINALIZED chain — never
// merely because it is PRESENT in the store. This is the store-vs-acceptance distinction the luxd-2
// freeze conflated, isolated from the network so the predicate itself is the unit under test.
func TestBootstrap_AcceptedHeight_StoreVsAcceptance(t *testing.T) {
	const M = 30
	const Top = 40
	chain, _ := buildBSChain(Top, -1)
	vm := newBSVMAt(chain, M)       // accepted 0..M
	vm.store(chain[M+1 : Top+1]...) // M+1..Top GOSSIPED into the store but UNACCEPTED
	bh := &blockHandler{logger: log.NewNoOpLogger(), vm: vm}
	ctx := context.Background()

	// (a) the accepted head and a finalized ancestor resolve as ACCEPTED at their canonical heights.
	if h, ok := bh.acceptedHeight(chain[M].id); !ok || h != uint64(M) {
		t.Fatalf("accepted head must resolve accepted at height %d, got (%d,%v)", M, h, ok)
	}
	if h, ok := bh.acceptedHeight(chain[M-5].id); !ok || h != uint64(M-5) {
		t.Fatalf("a finalized ancestor must resolve accepted at height %d, got (%d,%v)", M-5, h, ok)
	}
	// (b) stored-but-unaccepted blocks (ABOVE the accepted head) are present in the store yet NOT
	// accepted — the freeze conflation. acceptedHeight AND Accepted must both report not-accepted.
	for _, h := range []int{M + 1, Top} {
		if _, err := vm.GetBlock(ctx, chain[h].id); err != nil {
			t.Fatalf("precondition: chain[%d] must be present in the store", h)
		}
		if _, ok := bh.acceptedHeight(chain[h].id); ok {
			t.Fatalf("FREEZE: stored-but-unaccepted chain[%d] must NOT resolve as accepted", h)
		}
		if bh.Accepted(ctx, chain[h].id) {
			t.Fatalf("FREEZE: Accepted(stored-but-unaccepted chain[%d]) must be false", h)
		}
	}
	// (c) an absent block is not accepted.
	if bh.Accepted(ctx, ids.GenerateTestID()) {
		t.Fatal("an absent block must not be accepted")
	}
}

// TestNodeBootstrap_BehindNode_EmptyAncestry_StaysBootstrapping (variant B). A behind node names the
// real higher frontier Top (the producers report it), but the ancestry fetch returns EMPTY (the
// cross-version / unreachable case the canary risked). The node MUST stay un-synced (fail safe),
// NEVER concluding caught-up at its stale M: bs.Run returns a real error and the local height is
// unchanged — runInitialSync would leave the VM in Bootstrapping, not Ready.
func TestNodeBootstrap_BehindNode_EmptyAncestry_StaysBootstrapping(t *testing.T) {
	const M = 40
	const Top = 56
	chain, byID := buildBSChain(Top, -1)
	vm := newBSVMAt(chain, M)

	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	bh, chainID := newBSHandlerWeighted(t, vm, map[ids.NodeID]uint64{p1: 100, p2: 100, p3: 100})
	var pReady atomic.Bool
	pReady.Store(true)
	bh.primaryNetworkReady = pReady.Load
	// ancestorsEmpty — the producers REPLY to GetAncestors but serve an EMPTY batch (the
	// cross-version / withholding case). The frontier is named (producers report Top) but the gap
	// can never be fetched, so the descent must fail safe (ErrStalled), never conclude caught-up.
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3},
		byID: byID, tip: chain[Top], serveAncestors: false, ancestorsEmpty: true,
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	err := runBS(t, bh)
	require.Error(t, err, "an empty ancestry fetch must FAIL SAFE (real error), never a nil 'caught up'")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[M].id, last,
		"behind node must NOT advance and must NOT go Ready when the gap cannot be fetched — stays at M=%d (Bootstrapping)", M)
	require.False(t, bh.Accepted(ctx, chain[Top].id), "node must NOT hold the unfetched frontier")
}

// TestRED_BehindNode_PeerConnectDelay_NeverFalseCompletesAtStale (variant C). A behind node boots
// with the staked set loaded (P-ready) but its producer beacons still handshaking: the first polls
// see ZERO connected beacons. The node must WAIT (never conclude caught-up at its stale M while it
// cannot even ask the network), then converge to Top once the producers connect.
func TestRED_BehindNode_PeerConnectDelay_NeverFalseCompletesAtStale(t *testing.T) {
	const M = 40
	const Top = 56
	chain, byID := buildBSChain(Top, -1)
	vm := newBSVMAt(chain, M)

	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	bh, chainID := newBSHandlerWeighted(t, vm, map[ids.NodeID]uint64{p1: 100, p2: 100, p3: 100})
	var pReady atomic.Bool
	pReady.Store(true)
	bh.primaryNetworkReady = pReady.Load
	net := &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3},
		byID: byID, tip: chain[Top], serveAncestors: true,
		connectAfterCalls: 4, // beacons finish handshaking only after the 4th PeerInfo poll
	}
	bh.net = net
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	require.NoError(t, runBS(t, bh), "behind node must converge once the producer beacons connect")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[Top].id, last,
		"behind node must converge to Top=%d after the connect delay, NEVER false-complete at stale M=%d", Top, M)
	require.Greater(t, net.peerInfoCalls, net.connectAfterCalls,
		"the loop must have WAITED through the connecting passes before naming the frontier")
}

// TestChainExpectsStakedBeacons_SybilDrivesNotSkipBootstrap pins the ROOT-CAUSE decision of the
// rejoin wedge (tasks #66/#74): whether a chain syncs its bootstrap frontier against the STAKED
// primary-network set is driven by SYBIL PROTECTION, NOT --skip-bootstrap. Production validators
// set --skip-bootstrap (to skip the initial bootstrap WAIT); the prior `!m.SkipBootstrap && ...`
// made that flag also EMPTY the beacon set, so a behind native chain (C/X/Q) reported
// FrontierNoBeacons, named its STALE local tip the network frontier, went live there, and never
// caught up across restarts until a manual chaindata wipe. skip-bootstrap is not even an input to
// the fixed decision — that is the point.
func TestChainExpectsStakedBeacons_SybilDrivesNotSkipBootstrap(t *testing.T) {
	require.True(t, chainExpectsStakedBeacons(true, true, false),
		"sybil-protected native non-platform chain (C/X/Q) MUST expect staked beacons → peer-sync a behind validator (regardless of skip-bootstrap)")
	require.False(t, chainExpectsStakedBeacons(false, true, false),
		"non-sybil (dev / single-node) native chain → NO staked beacons: the empty-beacon immediate-start path")
	require.False(t, chainExpectsStakedBeacons(true, false, false),
		"a non-native chain does not sync against the primary staked set here")
	require.False(t, chainExpectsStakedBeacons(true, true, true),
		"the platform chain anchors to its OWN CustomBeacons, never the staked-set frontier quorum")
}

// TestChainValidatesOnPrimaryNetwork_RealHashChainID is the regression the hardcoded-bool test
// above could NOT catch: it exercises the ACTUAL discriminator buildChain feeds into
// chainExpectsStakedBeacons. The durable rejoin fix (#66/#74) shipped INERT in production because
// the discriminator keyed off the blockchain ID via ids.IsNativeChain — false for every deployed
// C/X/Q (they carry HASH blockchain IDs; ids.IsNativeChain only matches the symbolic 111...C alias
// form, which no deployed chain uses). So a real behind C-Chain still got empty beacons under
// --skip-bootstrap and wedged. The fix keys off the validating Net (chainParams.ChainID) instead.
func TestChainValidatesOnPrimaryNetwork_RealHashChainID(t *testing.T) {
	// Real deployed C-Chain blockchain IDs (devnet + mainnet) are HASHES, and ids.IsNativeChain
	// is false for them — the exact trap that disabled the fix.
	for _, s := range []string{
		"21HieZngQW8unBnSTbdQ9PcPAz6hhPLGrPzZhTvjC8KgjE95Bg", // devnet C-Chain
		"2wRdZGeca1qkxzNCq88NWDF5nJ5A9o623vRJKd3FsjRYvuVvvt", // mainnet C-Chain
	} {
		id, err := ids.FromString(s)
		require.NoError(t, err)
		require.False(t, ids.IsNativeChain(id),
			"trap: ids.IsNativeChain is false for real hash C-Chain ID %s — do NOT discriminate on the blockchain ID", s)
	}

	// C/X/Q validate on the PRIMARY NETWORK: chainParams.ChainID == PrimaryNetworkID. That is the
	// signal buildChain now passes, so the durable fix fires for the real (hash-ID) C-Chain.
	require.True(t, chainValidatesOnPrimaryNetwork(constants.PrimaryNetworkID))
	require.True(t, chainExpectsStakedBeacons(true, chainValidatesOnPrimaryNetwork(constants.PrimaryNetworkID), false),
		"a sybil-protected primary-network non-platform chain (C/X/Q) MUST expect staked beacons regardless of its hash blockchain ID")

	// An L2 validates on its OWN sovereign net, not the primary network → stays on the
	// empty-beacon single-node path (behavior unchanged for L2s).
	l2Net := ids.GenerateTestID()
	require.False(t, chainValidatesOnPrimaryNetwork(l2Net))
	require.False(t, chainExpectsStakedBeacons(true, chainValidatesOnPrimaryNetwork(l2Net), false))
}

// TestNodeBootstrap_SelfOnlyStakedSet_ReportsNoBeacons covers the single-VALIDATOR counterpart of
// the fix: a sybil-protected chain whose staked set is EXACTLY this node (a single-validator
// devnet, or the sole validator of an L1) has no OTHER beacon to sync from, so FrontierTip reports
// FrontierNoBeacons (immediate start at the node's own tip) rather than hanging in FrontierConnecting.
// This is what lets skip-bootstrap stop emptying native beacons WITHOUT bricking a single-validator
// net. It is NOT an eclipse: the staked set is read from P-chain STATE (hasExternalBeacons), so a
// hidden peer would still appear in `weights`.
func TestNodeBootstrap_SelfOnlyStakedSet_ReportsNoBeacons(t *testing.T) {
	const N = 10
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, N) // the sole validator IS at the frontier
	self := ids.GenerateTestNodeID()
	bh, chainID := newBSHandlerWeighted(t, vm, map[ids.NodeID]uint64{self: 100})
	bh.selfNodeID = self // the staked set is EXACTLY this node — a genuine single-validator net
	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, byID: byID, tip: chain[N]}
	bh.msgCreator = bsMsgBuilder{}

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(context.Background())
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNoBeacons, status,
		"self-only staked set (single validator) → NoBeacons (nothing to sync to), never a FrontierConnecting hang")
	require.Equal(t, ids.Empty, tip)

	// Discriminator: add ONE other validator and the SAME node must now run the quorum (has a peer
	// to sync from) — proving the self-only shortcut fires ONLY for a genuinely alone validator.
	require.True(t, bh.hasExternalBeacons(map[ids.NodeID]uint64{self: 100, ids.GenerateTestNodeID(): 100}),
		"a ≥2-validator staked set has an external beacon → runs the ⅔-by-stake quorum, not the self-only shortcut")
}

// TestNodeBootstrap_BehindValidator_StakedSet_CatchesUpNoWipe is THE REJOIN INVARIANT (tasks
// #66/#74), the code reproduction of the mainnet luxd-0 wedge: a staked validator that fell behind
// must sync from its peers on restart and reach the network frontier WITHOUT a chaindata wipe.
//
// The node reloads its STALE on-disk tip at height M (the "restart" — no wipe) and holds a
// sybil-protected native-chain staked beacon set (expectsStakedBeacons=true — exactly what
// buildChain now leaves in place under --skip-bootstrap, instead of emptying it). The 4 healthy
// producers name+serve the frontier N over the real GetAcceptedFrontier/GetAncestors transport, so
// the behind node fetch+executes M+1..N and ends ACCEPTED at N — never stuck at the stale M.
func TestNodeBootstrap_BehindValidator_StakedSet_CatchesUpNoWipe(t *testing.T) {
	const N = 60 // network frontier (the healthy producers)
	const M = 42 // our STALE local height after a restart — behind by N-M, NO wipe
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M) // reload the stale on-disk tip (the restart)

	// A 5-validator staked set (equal stake): this node is one, the other 4 are the connected,
	// serving producers at the frontier N. This mirrors lux-mainnet's 5-validator C-Chain.
	weights := map[ids.NodeID]uint64{}
	producers := make([]ids.NodeID, 4)
	for i := range producers {
		producers[i] = ids.GenerateTestNodeID()
		weights[producers[i]] = 100
	}
	self := ids.GenerateTestNodeID()
	weights[self] = 100
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.selfNodeID = self
	require.True(t, bh.expectsStakedBeacons,
		"a native sybil chain expects staked beacons even under skip-bootstrap — the fix that keeps peer-sync alive")

	bh.net = &bsBeaconNet{bh: bh, chainID: chainID, connected: producers, byID: byID, tip: chain[N], serveAncestors: true}
	bh.msgCreator = bsMsgBuilder{}

	ctx := context.Background()
	require.NoError(t, runBS(t, bh),
		"behind validator must converge to the frontier from its peers — no wipe, chain keeps finalizing")

	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last,
		"REJOIN: behind validator caught up from peers to frontier N=%d (was stuck at stale M=%d, no wipe)", N, M)
	require.True(t, bh.Accepted(ctx, chain[N].id),
		"node holds + ACCEPTED the frontier tip after the rejoin (genuine catch-up, not a stale false-complete)")
}

// TestWatchBootstrapProgress_StallThenConvergeIsRecoverable pins the semantics
// monitorBootstrap relies on after a stall stopped being terminal.
//
// The bug it guards: a no-progress window used to stop the engine, which cancels the
// chain's context, so every later VM.Accept returned "context canceled" and the
// finality guard refused to advance forever. Measured on lux-testnet, a node declared
// stalled at 13955 went on to reach 14154 — the healthy fleet's exact height — while
// refusing every incoming cert, because the verdict outlived the condition.
//
// monitorBootstrap now keeps watching after a stall, so what has to hold is that the
// SAME source, watched again, still reports ready once it converges. Watching once and
// abandoning the chain is what made the stall permanent.
func TestWatchBootstrapProgress_StallThenConvergeIsRecoverable(t *testing.T) {
	var height uint64 = 100
	stuck := true
	heightOf := func() uint64 {
		if !stuck {
			height++ // moving again
		}
		return height
	}
	ready := func() bool { return !stuck && height > 105 }

	// First pass: pinned height for the whole window => a genuine stall.
	outcome, at := watchBootstrapProgress(ready, nil, nil, heightOf, time.Millisecond, 60*time.Millisecond, nil)
	require.Equal(t, bootstrapStalled, outcome, "a pinned height must still be reported as a stall")
	require.Equal(t, uint64(100), at, "the stall must report the height it stalled at")

	// The sync was slow, not dead. Under the old behavior the engine was already stopped
	// here and this second pass never happened.
	stuck = false
	outcome, _ = watchBootstrapProgress(ready, nil, nil, heightOf, time.Millisecond, 60*time.Millisecond, nil)
	require.Equal(t, bootstrapReady, outcome,
		"a chain that converges AFTER a stall window must still be able to finish bootstrapping")
}
