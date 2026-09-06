// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/utils/bloom"
	"github.com/luxfi/node/version"
	"github.com/luxfi/util"
)

// ---------------------------------------------------------------------------
// Outbound queue: the throttler accounting
// ---------------------------------------------------------------------------

// countingThrottler records every Acquire and Release so a test can hold the
// queue to the throttler's contract: one Release for each Acquire that
// returned true, and none for one that did not.
type countingThrottler struct {
	mu       sync.Mutex
	acquired int
	released int
	refuse   bool
}

func (c *countingThrottler) Acquire(message.OutboundMessage, ids.NodeID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refuse {
		return false
	}
	c.acquired++
	return true
}

func (c *countingThrottler) Release(message.OutboundMessage, ids.NodeID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.released++
}

func (c *countingThrottler) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acquired, c.released
}

// countingFailures records the messages a queue reported it could not send.
type countingFailures struct {
	mu sync.Mutex
	n  int
}

func (c *countingFailures) SendFailed(message.OutboundMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
}

func (c *countingFailures) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// TestThrottledQueue_ReleasesEverythingItAcquired is the leak property. The
// outbound throttler hands out a byte budget per peer and takes it back on
// Release; a message that is acquired and never released shrinks that peer's
// budget for the life of the process, and enough of them stop it sending at
// all. The queue must balance the books whether a message is popped or thrown
// away at close.
func TestThrottledQueue_ReleasesEverythingItAcquired(t *testing.T) {
	require := require.New(t)
	throttler := &countingThrottler{}
	failures := &countingFailures{}

	q := NewThrottledMessageQueue(failures, ids.GenerateTestNodeID(), log.NewNoOpLogger(), throttler)

	for i := 0; i < 10; i++ {
		require.True(q.Push(context.Background(), newSizedMsg(32)))
	}
	acquired, released := throttler.counts()
	require.Equal(10, acquired)
	require.Zero(released, "nothing is released while it is still queued")

	// Four are popped and must release as they go.
	for i := 0; i < 4; i++ {
		_, ok := q.Pop()
		require.True(ok)
	}
	_, released = throttler.counts()
	require.Equal(4, released)

	// Close throws the remaining six away — and must release every one of
	// them, and report each as a failed send so the caller is not left
	// believing they went out.
	q.Close()
	acquired, released = throttler.counts()
	require.Equal(acquired, released,
		"a closed queue must return every byte of budget it was holding")
	require.Equal(6, failures.count(), "each abandoned message must be reported failed")

	// After close nothing is accepted, nothing is acquired, and popping
	// yields nothing.
	require.False(q.Push(context.Background(), newSizedMsg(32)))
	_, ok := q.Pop()
	require.False(ok)
	_, ok = q.PopNow()
	require.False(ok)
	q.Close() // idempotent
}

// TestThrottledQueue_RefusedMessageIsNotReleased is the other side of the
// contract, and the one that is easy to get backwards: a message the throttler
// refused was never acquired, so releasing it would hand back budget that was
// never taken and let the peer exceed its limit.
func TestThrottledQueue_RefusedMessageIsNotReleased(t *testing.T) {
	require := require.New(t)
	throttler := &countingThrottler{refuse: true}
	failures := &countingFailures{}

	q := NewThrottledMessageQueue(failures, ids.GenerateTestNodeID(), log.NewNoOpLogger(), throttler)

	require.False(q.Push(context.Background(), newSizedMsg(32)))
	acquired, released := throttler.counts()
	require.Zero(acquired)
	require.Zero(released, "a message that was never acquired must never be released")
	require.Equal(1, failures.count(), "a rate-limited message must be reported failed")
}

// TestThrottledQueue_CancelledSendIsRefusedBeforeAcquiring. A caller whose
// context has already expired gets its message dropped without touching the
// throttler at all — the budget belongs to messages that might still be sent.
func TestThrottledQueue_CancelledSendIsRefusedBeforeAcquiring(t *testing.T) {
	require := require.New(t)
	throttler := &countingThrottler{}
	failures := &countingFailures{}

	q := NewThrottledMessageQueue(failures, ids.GenerateTestNodeID(), log.NewNoOpLogger(), throttler)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.False(q.Push(ctx, newSizedMsg(32)))
	acquired, released := throttler.counts()
	require.Zero(acquired, "a cancelled send must not spend budget")
	require.Zero(released)
	require.Equal(1, failures.count())
}

// TestThrottledQueue_PopBlocksThenWakes covers the blocking read and its
// shutdown: a consumer waiting on an empty queue must be released by Close
// rather than left holding a goroutine for the life of the process.
func TestThrottledQueue_PopBlocksThenWakes(t *testing.T) {
	require := require.New(t)
	q := NewThrottledMessageQueue(&countingFailures{}, ids.GenerateTestNodeID(),
		log.NewNoOpLogger(), &countingThrottler{})

	_, ok := q.PopNow()
	require.False(ok, "an empty queue has nothing to hand back")

	popped := make(chan bool, 1)
	go func() {
		_, ok := q.Pop()
		popped <- ok
	}()

	select {
	case <-popped:
		t.Fatal("Pop returned on an empty queue")
	case <-time.After(50 * time.Millisecond):
	}

	msg := newSizedMsg(8)
	require.True(q.Push(context.Background(), msg))
	select {
	case ok := <-popped:
		require.True(ok)
	case <-time.After(5 * time.Second):
		t.Fatal("Pop never woke")
	}

	// And a consumer parked on an empty queue is released by Close.
	go func() {
		_, ok := q.Pop()
		popped <- ok
	}()
	time.Sleep(50 * time.Millisecond)
	q.Close()
	select {
	case ok := <-popped:
		require.False(ok, "a closed queue reports that it has nothing")
	case <-time.After(5 * time.Second):
		t.Fatal("Close left a consumer blocked")
	}
}

// ---------------------------------------------------------------------------
// Gossip tracker callback
// ---------------------------------------------------------------------------

// TestGossipTrackerCallback_MirrorsTheValidatorSet. The tracker decides who
// this node will gossip about; the callback is the only thing keeping it in
// step with the validator set. If a departure is not mirrored, the node keeps
// gossiping a validator that no longer exists.
func TestGossipTrackerCallback_MirrorsTheValidatorSet(t *testing.T) {
	require := require.New(t)

	tracker, err := NewGossipTracker(metric.NewRegistry(), "callback")
	require.NoError(err)
	cb := &GossipTrackerCallback{Log: log.NewNoOpLogger(), GossipTracker: tracker}

	peerID := ids.GenerateTestNodeID()
	require.True(tracker.StartTrackingPeer(peerID))

	validator := ids.GenerateTestNodeID()
	cb.OnValidatorAdded(validator, 100)

	unknown, ok := tracker.GetUnknown(peerID)
	require.True(ok)
	require.Len(unknown, 1, "an added validator must become gossipable")
	require.Equal(validator, unknown[0].NodeID)

	// Weight changes are not gossip: they must not disturb the set.
	cb.OnValidatorLightChanged(validator, 100, 5000)
	unknown, ok = tracker.GetUnknown(peerID)
	require.True(ok)
	require.Len(unknown, 1, "a weight change is not a membership change")

	cb.OnValidatorRemoved(validator, 5000)
	unknown, ok = tracker.GetUnknown(peerID)
	require.True(ok)
	require.Empty(unknown, "a removed validator must stop being gossiped about")
}

// TestGossipTrackerCallback_SurvivesARedundantEvent. The callbacks are driven
// by an external set and must be safe against a repeat or an out-of-order
// event — they log and carry on rather than corrupting the tracker or
// panicking on the callback goroutine.
func TestGossipTrackerCallback_SurvivesARedundantEvent(t *testing.T) {
	require := require.New(t)

	tracker, err := NewGossipTracker(metric.NewRegistry(), "callback")
	require.NoError(err)
	cb := &GossipTrackerCallback{Log: log.NewNoOpLogger(), GossipTracker: tracker}

	peerID := ids.GenerateTestNodeID()
	require.True(tracker.StartTrackingPeer(peerID))

	validator := ids.GenerateTestNodeID()
	cb.OnValidatorAdded(validator, 1)
	cb.OnValidatorAdded(validator, 1) // repeat: already present
	cb.OnValidatorRemoved(validator, 1)
	cb.OnValidatorRemoved(validator, 1) // repeat: already gone
	cb.OnValidatorRemoved(ids.GenerateTestNodeID(), 1)

	unknown, ok := tracker.GetUnknown(peerID)
	require.True(ok)
	require.Empty(unknown, "redundant events must leave the tracker consistent")
}

// TestGossipTracker_TxIDMapsBackToItsNode pins the reverse lookup the gossip
// wire depends on: peers exchange txIDs, and a txID that does not map back to
// the node that registered it makes the compressed form unreadable.
func TestGossipTracker_TxIDMapsBackToItsNode(t *testing.T) {
	require := require.New(t)

	tracker, err := NewGossipTracker(metric.NewRegistry(), "callback")
	require.NoError(err)

	vdr := ValidatorID{NodeID: ids.GenerateTestNodeID(), TxID: ids.GenerateTestID()}
	require.True(tracker.AddValidator(vdr))

	nodeID, ok := tracker.GetNodeID(vdr.TxID)
	require.True(ok)
	require.Equal(vdr.NodeID, nodeID)

	_, ok = tracker.GetNodeID(ids.GenerateTestID())
	require.False(ok, "a txID nobody registered maps to nobody")

	require.True(tracker.RemoveValidator(vdr.NodeID))
	_, ok = tracker.GetNodeID(vdr.TxID)
	require.False(ok, "a departed validator's txID must stop resolving")
}

// ---------------------------------------------------------------------------
// Peer set reporting
// ---------------------------------------------------------------------------

// infoPeer is a peer carrying just enough state for Info() to be meaningful.
func infoPeer(t *testing.T, nodeID ids.NodeID, addr netip.AddrPort, uptime uint32) *peer {
	t.Helper()
	return &peer{
		id:             nodeID,
		conn:           &addrConn{addr: addr},
		ip:             &SignedIP{UnsignedIP: UnsignedIP{AddrPort: addr}},
		version:        &version.Application{Name: "lux", Major: 1, Minor: 2, Patch: 3},
		trackedChains:  set.Of(ids.GenerateTestID()),
		supportedLPs:   set.Of[uint32](176),
		objectedLPs:    set.Set[uint32]{},
		observedUptime: *utils.NewAtomic(uptime),
	}
}

// addrConn is a conn that knows only its remote address, which is all Info
// reads from it.
type addrConn struct {
	scriptConn
	addr netip.AddrPort
}

func (c *addrConn) RemoteAddr() net.Addr { return netAddr{c.addr} }

type netAddr struct{ addr netip.AddrPort }

func (netAddr) Network() string  { return "tcp" }
func (a netAddr) String() string { return a.addr.String() }

// TestPeerSet_ReportsEveryPeerItHolds. AllInfo is what an operator sees when
// they ask a node who it is connected to; it must report every peer in the set
// and no one else, and Info by id must agree with it peer for peer.
func TestPeerSet_ReportsEveryPeerItHolds(t *testing.T) {
	require := require.New(t)

	s := NewSet()
	require.Empty(s.AllInfo(), "an empty set reports no peers")
	require.Empty(s.Info([]ids.NodeID{ids.GenerateTestNodeID()}),
		"asking about a peer that is not here yields nothing, not a blank entry")

	peers := make([]*peer, 3)
	for i := range peers {
		peers[i] = infoPeer(t,
			ids.BuildTestNodeID([]byte{byte(i + 1)}),
			netip.MustParseAddrPort("203.0.113.1:9651"),
			uint32(10*i))
		s.Add(peers[i])
	}

	all := s.AllInfo()
	require.Len(all, 3)
	byID := make(map[ids.NodeID]Info, len(all))
	for _, info := range all {
		byID[info.ID] = info
	}
	for i, p := range peers {
		info, ok := byID[p.id]
		require.True(ok, "peer %d missing from AllInfo", i)
		require.EqualValues(10*i, info.ObservedUptime)
		require.Equal("lux/1.2.3", info.Version)
	}

	// Asking for a subset returns exactly the ones that are present.
	want := []ids.NodeID{peers[0].id, ids.GenerateTestNodeID(), peers[2].id}
	subset := s.Info(want)
	require.Len(subset, 2, "unknown ids are skipped, not reported blank")
	require.Equal(peers[0].id, subset[0].ID)
	require.Equal(peers[2].id, subset[1].ID)

	// A removed peer stops being reported, and the rest survive the swap the
	// removal does internally.
	s.Remove(peers[0].id)
	all = s.AllInfo()
	require.Len(all, 2)
	for _, info := range all {
		require.NotEqual(peers[0].id, info.ID, "a removed peer must not be reported")
	}
}

// TestPeer_AccessorsReportWhatTheHandshakeEstablished. These are the read
// surfaces the node's own API and the network layer key off; each must return
// what the handshake put there rather than a zero value that reads as "no
// claim".
func TestPeer_AccessorsReportWhatTheHandshakeEstablished(t *testing.T) {
	require := require.New(t)
	victim, attacker := startWirePair(t)

	attacker.sendHandshake(t, attacker.validHandshake())
	peerList, err := attacker.mc.PeerList(nil, true)
	require.NoError(err)
	attacker.send(t, peerList)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(victim.AwaitReady(ctx))

	require.Equal(attacker.raw.config.MyNodeID, victim.ID())
	require.NotNil(victim.Cert())
	require.Equal(attacker.raw.cert.Raw, victim.Cert().Raw,
		"the peer must be identified by the certificate it presented")

	claimed := victim.IP()
	require.NotNil(claimed)
	require.Equal(attacker.signedIP.AddrPort, claimed.AddrPort,
		"the address a peer claimed is the address we hold for it")
	require.Equal(attacker.signedIP.Timestamp, claimed.Timestamp)

	require.Equal(version.CurrentApp.String(), victim.Version().String())

	// The primary network is always tracked, whatever the peer said.
	require.True(victim.TrackedChains().Contains(primaryNetworkTestID()))

	info := victim.Info()
	require.Equal(victim.ID(), info.ID)
	require.Equal(claimed.AddrPort, info.PublicIP)
	require.Equal(victim.Version().String(), info.Version)

	// Asking for a peer list is a nudge, not a queue: repeated asks while one
	// is already pending must not pile up or block.
	for i := 0; i < 100; i++ {
		victim.StartSendGetPeerList()
	}
	require.True(victim.Ready())
}

// primaryNetworkTestID names the chain every peer tracks implicitly.
func primaryNetworkTestID() ids.ID {
	return ids.Empty
}

// TestPeer_GetPeerListAnswersOnlyAfterTheHandshake pins the ordering: a peer
// that has not identified itself gets no address book, and the request is
// dropped rather than treated as hostile. Once it is up, a well-formed request
// is served.
func TestPeer_GetPeerListAnswersOnlyAfterTheHandshake(t *testing.T) {
	require := require.New(t)
	victim, attacker := startWirePair(t)

	getPeerList, err := attacker.mc.GetPeerList(bloom.EmptyFilter.Marshal(), nil, true)
	require.NoError(err)
	attacker.send(t, getPeerList)

	time.Sleep(200 * time.Millisecond)
	require.False(victim.Closed(), "an early request is ignored, not punished")
	require.False(victim.Ready())

	attacker.sendHandshake(t, attacker.validHandshake())
	peerList, err := attacker.mc.PeerList(nil, true)
	require.NoError(err)
	attacker.send(t, peerList)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(victim.AwaitReady(ctx))

	attacker.send(t, getPeerList)
	time.Sleep(200 * time.Millisecond)
	require.False(victim.Closed(), "a well-formed request after the handshake is served")
	require.True(victim.Ready())
}
