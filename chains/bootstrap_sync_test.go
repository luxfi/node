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

	consensusconfig "github.com/luxfi/consensus/config"
	consensuschain "github.com/luxfi/consensus/engine/chain"
	cblock "github.com/luxfi/consensus/engine/chain/block"
	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
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

// newBSVMAt seeds the VM with blocks 0..m ACCEPTED (a node already at height m) so a test
// can model a STALE node rather than an empty one. SyncStateFromVM then seeds consensus at
// height m, and the first fetched gap block extends chain[m].
func newBSVMAt(chain []*bsTestBlock, m int) *bsTestVM {
	vm := newBSVM(chain)
	for i := 1; i <= m; i++ {
		vm.accepted[chain[i].id] = true
	}
	vm.lastAcc = chain[m].id
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
	bh             *blockHandler
	chainID        ids.ID
	connected      []ids.NodeID            // beacons that are connected + tracking the chain
	byID           map[ids.ID]*bsTestBlock // honest chain by id (for ancestry serving)
	tip            *bsTestBlock            // the honest tip the beacons report
	serveAncestors bool                    // beacons serve ancestry (false models name-only beacons)

	// tipFor optionally overrides the tip a specific beacon reports (models DISAGREEMENT —
	// beacons connected but split across tips, so no ⅔ quorum forms → FrontierNoQuorum).
	tipFor map[ids.NodeID]ids.ID

	// connectAfterCalls models the CANARY boot race over the REAL transport: while
	// peerInfoCalls ≤ connectAfterCalls the beacons are reported as NOT yet connected (so
	// FrontierTip must return FrontierConnecting and the loop WAITS); afterward they connect.
	connectAfterCalls int
	peerInfoCalls     int

	// Optional malicious NON-beacon peer: connected and tracking, reports a forged tip.
	malicious ids.NodeID
	forgedTip ids.ID
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
	beaconsUp := n.connectAfterCalls == 0 || n.peerInfoCalls > n.connectAfterCalls
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
		// disagreement).
		for id := range nodeIDs {
			tip := n.tip.id
			if n.tipFor != nil {
				if t, ok := n.tipFor[id]; ok {
					tip = t
				}
			}
			n.bh.deliverBootstrapFrontier(id, tip)
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
	require.True(t, bh.Has(ctx, chain[N].id), "node must hold the beacon tip after sync")
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
	require.True(t, bh.Has(ctx, chain[N].id), "node must hold the producer tip after sync")
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
	require.False(t, bh.Has(ctx, chain[N].id), "no frontier tip may be adopted from a non-staked endpoint")
}

// TestRED_TipSplitConvergesToCommonHeight is THE MAINNET CANARY (luxd-2 on v1.30.78) the
// ancestor-tolerant frontier quorum fixes. The 3 producers hold > ⅔ stake but are SPLIT at the
// bleeding edge: 2 have accepted block N, the third's UN-FINALIZED pending block is N+1 (one
// block ahead, on the SAME chain). The chain is idle, so the split is STABLE and never resolves.
//
// THE BUG: the EXACT-tip tally required ⅔-by-stake to report the IDENTICAL id. N drew only the 2
// producers (200 of 300; the floor is `> 200`, so 200 does NOT clear it) and N+1 drew only the
// lone producer (100) — neither cleared ⅔, so FrontierTip returned NoQuorum FOREVER and the
// healthy stale node failed safe at its stale height instead of converging.
//
// THE FIX: a beacon reporting N+1 also has N in its accepted chain, so N draws ALL THREE
// producers (300 > 200) and is NAMED; N+1 draws only the lone producer and is NOT. The node
// converges to N (the ⅔-agreed committed height); live consensus catch-up handles N+1. If this
// regressed to the exact-tip tally, FrontierTip would return NoQuorum here and the test fails.
func TestRED_TipSplitConvergesToCommonHeight(t *testing.T) {
	const N = 40          // the ⅔-agreed COMMON committed height (all 3 producers have it accepted)
	const pending = N + 1 // the lone producer's UN-FINALIZED pending block (1 ahead, same chain)
	const M = 23          // our STALE local height (gap-17 — the canary's gap, within the window)
	chain, byID := buildBSChain(pending, -1)
	vm := newBSVMAt(chain, M)

	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	weights := map[ids.NodeID]uint64{p1: 100, p2: 100, p3: 100} // total 300, ⅔ floor = 200 (need > 200)
	bh, chainID := newBSHandlerWeighted(t, vm, weights)

	// p1, p2 report the common tip N; p3 (luxd-3) reports its pending N+1. All on ONE chain, so
	// N+1 vouches for N. Exact tally: N=200 (not > 200), N+1=100 — NoQuorum. Ancestor-tolerant: N=300.
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3},
		byID: byID, tip: chain[N], serveAncestors: true,
		tipFor: map[ids.NodeID]ids.ID{p3: chain[pending].id},
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	// Sanity: 2-of-3 producers (200) is exactly the floor, NOT above it — the exact-tip tally
	// genuinely cannot name N. The fix must come from ancestor tolerance, not a looser floor.
	require.Equal(t, uint64(200), consensusconfig.TwoThirdsStakeFloor(300))

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status,
		"CANARY: a ±1-block tip split must name the ⅔-COMMON height, not return NoQuorum")
	require.Equal(t, chain[N].id, tip, "must name N (the height all 3 producers share), NOT the minority N+1")
	require.NotEqual(t, chain[pending].id, tip, "the lone producer's pending N+1 must NOT be named")

	// The full loop converges to N — NOT fail-safe, NOT to the minority N+1.
	require.NoError(t, runBS(t, bh), "tip-split stale node must converge to the ⅔-common height")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last,
		"CANARY SUCCESS: converge to the ⅔-common height N=%d (NOT stuck at M=%d, NOT the minority N+1)", N, M)
	require.True(t, bh.Has(ctx, chain[N].id), "node must hold the ⅔-common tip N after sync")
	require.False(t, bh.Has(ctx, chain[pending].id), "node must NOT hold the minority pending N+1 — that is live consensus' job")
}

// TestRED_ForgedHighTipFromMinorityNotNamed is the C1 proof for the ancestor-tolerant path: a
// Byzantine BEACON (in the staked set, but < ⅓ stake) reports a FORGED block built directly on
// the real ⅔-backed block N — a forged sibling of the honest pending N+1. Ancestor tolerance
// must NOT name the forged block (it holds only the Byzantine minority's stake); it must name
// the real ⅔-common N, and the node must never sync or hold the forged block.
//
// This is the adversarial heart of C1 under the new agreement relation: even a forged block that
// descends from a genuinely ⅔-backed block only RATIFIES that real ancestor (which the honest ⅔
// already back) — it can never name ITSELF, because naming requires > ⅔ of the TOTAL staked
// weight behind the block, and the forger holds < ⅓. Only the agreement relation changed
// (exact-tip → in-accepted-chain); the ⅔-by-stake-of-the-real-set requirement did not.
func TestRED_ForgedHighTipFromMinorityNotNamed(t *testing.T) {
	const N = 40
	const pending = N + 1
	const M = 23
	chain, byID := buildBSChain(pending, -1)
	vm := newBSVMAt(chain, M)

	// A forged block at height N+1 whose parent is the REAL N (a forged sibling of the honest
	// pending N+1). The node can PARSE it (any structurally-valid block parses) but has NOT
	// accepted it — model that by adding it to the VM's parse maps but not to `accepted`.
	forged := &bsTestBlock{
		id: ids.GenerateTestID(), parent: chain[N].id, height: pending,
		bytes: []byte("forged-sibling@" + strconv.Itoa(pending) + ":" + ids.GenerateTestID().String()),
		valid: true,
	}
	byID[forged.id] = forged
	vm.all[forged.id] = forged
	vm.byBytes[string(forged.bytes)] = forged

	p1, p2, p3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	byz := ids.GenerateTestNodeID()
	// 4 beacons @100 → total 400, ⅔ floor = 266. Honest p1,p2 at N (200), p3 at the real N+1
	// (100), Byzantine byz at the FORGED sibling (100). No tip clears 266 exactly → ancestor path.
	weights := map[ids.NodeID]uint64{p1: 100, p2: 100, p3: 100, byz: 100}
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{p1, p2, p3, byz},
		byID: byID, tip: chain[N], serveAncestors: true,
		tipFor: map[ids.NodeID]ids.ID{p3: chain[pending].id, byz: forged.id},
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status, "the honest ⅔-common N must still be named")
	require.Equal(t, chain[N].id, tip, "C1: the named frontier is the real ⅔-common N")
	require.NotEqual(t, forged.id, tip, "C1: the Byzantine-minority FORGED block must NEVER be named")
	require.NotEqual(t, chain[pending].id, tip, "the honest minority pending N+1 is not named either")

	require.NoError(t, runBS(t, bh), "node converges to the real ⅔-common height")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last, "C1: node syncs to the real N")
	require.False(t, bh.Has(ctx, forged.id), "C1: the forged block must NOT be finalized")
	require.False(t, bh.Has(ctx, chain[pending].id), "the honest pending N+1 is left to live consensus")
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
	ready := func() bool { return calls >= 120 }                // done only after 120 polls (≫ the 60ms window)
	outcome, _ := watchBootstrapProgress(ready, nil, heightOf, time.Millisecond, 60*time.Millisecond, nil)
	require.Equal(t, bootstrapReady, outcome, "a slow-but-ADVANCING sync must complete, not be timed out")
}

// TestWatchBootstrapProgress_GenuineStallFails: a sync whose height does NOT advance for
// the whole window is failed (a real stall is still surfaced, not masked).
func TestWatchBootstrapProgress_GenuineStallFails(t *testing.T) {
	heightOf := func() uint64 { return 5 } // pinned — no progress
	ready := func() bool { return false }
	start := time.Now()
	outcome, last := watchBootstrapProgress(ready, nil, heightOf, time.Millisecond, 60*time.Millisecond, nil)
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
		func() bool { return false }, nil, func() uint64 { return 0 },
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
