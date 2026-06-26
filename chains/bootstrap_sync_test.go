// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// bootstrap_sync_test.go — node-side proof that the blockHandler's bootstrap wiring
// (BlockSource transport adapter + Chain execute sink) converges an EMPTY node from
// genesis to a peer's tip over the real GetAcceptedFrontier/GetAncestors message path,
// plus unit coverage of the framing decode and the reply gating. The fetch+execute
// ALGORITHM itself is proven in consensus (engine/chain/bootstrap); here we prove the
// node TRANSPORT correlates frontier/ancestor replies and drives the loop to the tip.
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

// ----- mock outbound message + builder + peer net ---------------------------

type bsOutMsg struct {
	op        string
	blockID   ids.ID
	requestID uint32
}

func (*bsOutMsg) BypassThrottling() bool   { return true }
func (*bsOutMsg) Op() message.Op           { return 0 }
func (*bsOutMsg) Bytes() []byte            { return nil }
func (*bsOutMsg) BytesSavedCompression() int { return 0 }

type bsMsgBuilder struct{ message.OutboundMsgBuilder }

func (bsMsgBuilder) GetAcceptedFrontier(_ ids.ID, requestID uint32, _ time.Duration) (message.OutboundMessage, error) {
	return &bsOutMsg{op: "frontier", requestID: requestID}, nil
}
func (bsMsgBuilder) GetAncestors(_ ids.ID, requestID uint32, _ time.Duration, blockID ids.ID, _ p2p.EngineType) (message.OutboundMessage, error) {
	return &bsOutMsg{op: "ancestors", blockID: blockID, requestID: requestID}, nil
}

// bsPeerNet is a peer holding the full chain. Send dispatches the matching reply back
// into the handler's bootstrap channels (synchronously — the channels are buffered, so
// the reply is queued before the waiting FrontierTip/Ancestors selects on it).
type bsPeerNet struct {
	network.Network
	bh      *blockHandler
	chainID ids.ID
	byID    map[ids.ID]*bsTestBlock
	tip     *bsTestBlock
	peer    ids.NodeID
}

func (n *bsPeerNet) PeerInfo([]ids.NodeID) []peer.Info {
	return []peer.Info{{ID: n.peer, TrackedChains: set.Of(n.chainID)}}
}

func (n *bsPeerNet) Send(msg message.OutboundMessage, _ set.Set[ids.NodeID], _ ids.ID, _ uint32) set.Set[ids.NodeID] {
	m, ok := msg.(*bsOutMsg)
	if !ok {
		return nil
	}
	switch m.op {
	case "frontier":
		n.bh.deliverBootstrapFrontier(n.tip.id)
	case "ancestors":
		n.bh.deliverBootstrapAncestors(m.requestID, n.frame(m.blockID))
	}
	return nil
}

// frame serves up to 256 blocks ending at blockID, OLDEST-FIRST, in the same outer
// [entryLen:4][entry] framing GetContext produces (entry = encodeCatchupEntry).
func (n *bsPeerNet) frame(blockID ids.ID) []byte {
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

// newBSHandlerAndEngine wires a blockHandler over a fresh engine (K=1, no verifier) and
// the mock VM, seeded at genesis — the node-side of an EMPTY node.
func newBSHandlerAndEngine(t *testing.T, vm *bsTestVM) (*blockHandler, ids.ID) {
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
	bh := &blockHandler{
		logger:         log.NewNoOpLogger(),
		engine:         eng,
		vm:             vm,
		chainID:        chainID,
		networkID:      networkID,
		pendingContext: make(map[ids.ID]contextRequest),
		bsAncestorCh:   make(map[uint32]chan [][]byte),
	}
	return bh, chainID
}

// TestNodeBootstrap_EmptyNodeConvergesViaTransport: a fresh/empty node (only genesis)
// FETCHES blocks 1..N from a peer over the real GetAcceptedFrontier/GetAncestors path,
// re-EXECUTES them, and ACCEPTS up to N — the node-level analog of the consensus
// empty→tip test, proving the transport adapter actually drives the loop.
func TestNodeBootstrap_EmptyNodeConvergesViaTransport(t *testing.T) {
	const N = 50
	chain, byID := buildBSChain(N, -1)
	vm := newBSVM(chain)
	bh, chainID := newBSHandlerAndEngine(t, vm)

	peerNet := &bsPeerNet{bh: bh, chainID: chainID, byID: byID, tip: chain[N], peer: ids.GenerateTestNodeID()}
	bh.net = peerNet
	bh.msgCreator = bsMsgBuilder{}

	// Drive the SAME loop buildChain runs, with bsActive set (so the peer replies route
	// to the loop's channels).
	bh.bsActive.Store(true)
	bs := chainbootstrap.New(chainbootstrap.Config{Source: bh, Chain: bh, Log: log.NewNoOpLogger()})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := bs.Run(ctx)
	bh.bsActive.Store(false)
	require.NoError(t, err, "bootstrap loop must converge over the transport")

	// The empty node reached the peer's tip purely by fetch+execute.
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last, "empty node must sync to the peer tip (height %d), not stay at genesis", N)
	require.Equal(t, 1, chain[N].accepts, "tip block must be VM-accepted exactly once")
	require.True(t, bh.Has(ctx, chain[N].id), "node must hold the tip after sync")
}

// TestNodeBootstrap_InvalidBlockHaltsTransport: a peer that serves a corrupt block at
// height bad makes the node accept 1..bad-1 then STOP — it never advances past the
// unverifiable block (the safety property, over the real transport).
func TestNodeBootstrap_InvalidBlockHaltsTransport(t *testing.T) {
	const N = 40
	const bad = 25
	chain, byID := buildBSChain(N, bad)
	vm := newBSVM(chain)
	bh, chainID := newBSHandlerAndEngine(t, vm)

	peerNet := &bsPeerNet{bh: bh, chainID: chainID, byID: byID, tip: chain[N], peer: ids.GenerateTestNodeID()}
	bh.net = peerNet
	bh.msgCreator = bsMsgBuilder{}

	bh.bsActive.Store(true)
	// A short RetryInterval makes the post-halt stall return promptly; the node has
	// already halted at bad-1 on the first pass (the assertion below), the rest is just
	// the loop confirming it cannot pass the invalid block.
	bs := chainbootstrap.New(chainbootstrap.Config{Source: bh, Chain: bh, Log: log.NewNoOpLogger(), RetryInterval: time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = bs.Run(ctx) // stalls (cannot pass the invalid block) — error is the stall, not a panic
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
// path runs unchanged.
func TestDeliverBootstrap_GatedByActive(t *testing.T) {
	bh := &blockHandler{bsAncestorCh: make(map[uint32]chan [][]byte)}

	// Inactive: both hooks are inert (return false → caller uses the live path).
	require.False(t, bh.deliverBootstrapFrontier(ids.GenerateTestID()))
	require.False(t, bh.deliverBootstrapAncestors(1, nil))

	// Active: a registered FrontierTip channel receives the reply.
	bh.bsActive.Store(true)
	fch := make(chan ids.ID, 1)
	bh.bsFrontierCh = fch
	tip := ids.GenerateTestID()
	require.True(t, bh.deliverBootstrapFrontier(tip))
	require.Equal(t, tip, <-fch)

	// Active: a registered Ancestors channel receives the decoded blocks.
	ach := make(chan [][]byte, 1)
	bh.bsAncestorCh[7] = ach
	entry := encodeCatchupEntry([]byte("blk"), nil)
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(entry)))
	require.True(t, bh.deliverBootstrapAncestors(7, append(lp[:], entry...)))
	require.Equal(t, [][]byte{[]byte("blk")}, <-ach)
}
