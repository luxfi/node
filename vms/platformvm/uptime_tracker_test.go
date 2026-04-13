// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"sync"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

// fakeUptimeState implements uptime.State for testing.
type fakeUptimeState struct {
	uptimes    map[ids.NodeID]time.Duration
	lastUpdate map[ids.NodeID]time.Time
	startTimes map[ids.NodeID]time.Time
}

func newFakeUptimeState() *fakeUptimeState {
	return &fakeUptimeState{
		uptimes:    make(map[ids.NodeID]time.Duration),
		lastUpdate: make(map[ids.NodeID]time.Time),
		startTimes: make(map[ids.NodeID]time.Time),
	}
}

func (f *fakeUptimeState) addValidator(nodeID ids.NodeID, netID ids.ID, startTime time.Time) {
	_ = netID
	f.uptimes[nodeID] = 0
	f.lastUpdate[nodeID] = startTime
	f.startTimes[nodeID] = startTime
}

func (f *fakeUptimeState) GetUptime(nodeID ids.NodeID, _ ids.ID) (time.Duration, time.Duration, error) {
	up, ok := f.uptimes[nodeID]
	if !ok {
		return 0, 0, errNotFound
	}
	lastUpdatedDuration := time.Duration(f.lastUpdate[nodeID].Unix()) * time.Second
	return up, lastUpdatedDuration, nil
}

func (f *fakeUptimeState) SetUptime(nodeID ids.NodeID, _ ids.ID, uptime time.Duration, lastUpdated time.Time) error {
	if _, ok := f.uptimes[nodeID]; !ok {
		return errNotFound
	}
	f.uptimes[nodeID] = uptime
	f.lastUpdate[nodeID] = lastUpdated
	return nil
}

func (f *fakeUptimeState) GetStartTime(nodeID ids.NodeID, _ ids.ID) (time.Time, error) {
	st, ok := f.startTimes[nodeID]
	if !ok {
		return time.Time{}, errNotFound
	}
	return st, nil
}

var errNotFound = errTestNotFound{}

type errTestNotFound struct{}

func (errTestNotFound) Error() string { return "not found" }

func TestUptimeTrackerConnectDisconnect(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now.Add(-time.Hour))

	tracker := newUptimeTracker(state, netID, clk)

	// Before connect: 0% uptime.
	pct, err := tracker.CalculateUptimePercent(nodeID, netID)
	require.NoError(err)
	require.InDelta(0.0, pct, 0.01)

	// Connect.
	tracker.Connect(nodeID)

	// Advance clock by 30 minutes.
	now = now.Add(30 * time.Minute)

	// While connected: should show ~50% (30min connected / 60min+30min total).
	pct, err = tracker.CalculateUptimePercent(nodeID, netID)
	require.NoError(err)
	// 30min / 90min = 0.333...
	require.InDelta(0.333, pct, 0.01)

	// Disconnect.
	require.NoError(tracker.Disconnect(nodeID))

	// State should now have 30 minutes of uptime persisted.
	require.Equal(30*time.Minute, state.uptimes[nodeID])

	// After disconnect, no live connection bonus.
	pct, err = tracker.CalculateUptimePercent(nodeID, netID)
	require.NoError(err)
	require.InDelta(0.333, pct, 0.01)
}

func TestUptimeTrackerDoubleConnect(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now.Add(-time.Hour))
	tracker := newUptimeTracker(state, netID, clk)

	tracker.Connect(nodeID)
	tracker.Connect(nodeID) // second connect should be no-op

	now = now.Add(10 * time.Minute)
	require.NoError(tracker.Disconnect(nodeID))

	// Should be exactly 10 minutes, not 20.
	require.Equal(10*time.Minute, state.uptimes[nodeID])
}

func TestUptimeTrackerShutdown(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeA := ids.GenerateTestNodeID()
	nodeB := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	state.addValidator(nodeA, netID, now.Add(-time.Hour))
	state.addValidator(nodeB, netID, now.Add(-time.Hour))
	tracker := newUptimeTracker(state, netID, clk)

	tracker.Connect(nodeA)
	tracker.Connect(nodeB)

	now = now.Add(5 * time.Minute)
	require.NoError(tracker.Shutdown())

	require.Equal(5*time.Minute, state.uptimes[nodeA])
	require.Equal(5*time.Minute, state.uptimes[nodeB])
}

func TestUptimeTrackerUnknownValidator(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	unknownNode := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	tracker := newUptimeTracker(state, netID, clk)

	// Connect an unknown validator. Disconnect should not error.
	tracker.Connect(unknownNode)
	now = now.Add(time.Minute)
	require.NoError(tracker.Disconnect(unknownNode))

	// CalculateUptimePercent for unknown validator should error.
	_, err := tracker.CalculateUptimePercent(unknownNode, netID)
	require.Error(err)
}

func TestUptimeTrackerWrongNet(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	otherNet := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now.Add(-time.Hour))
	tracker := newUptimeTracker(state, netID, clk)

	// Querying the wrong net should return 0.
	pct, err := tracker.CalculateUptimePercent(nodeID, otherNet)
	require.NoError(err)
	require.Equal(0.0, pct)
}

func TestUptimeTrackerDisconnectWithoutConnect(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now.Add(-time.Hour))
	tracker := newUptimeTracker(state, netID, clk)

	// Disconnect without prior connect should be no-op.
	require.NoError(tracker.Disconnect(nodeID))
	require.Equal(time.Duration(0), state.uptimes[nodeID])
}

// --- Additional edge-case and inversion tests ---

// TestUptimeTrackerRapidConnectDisconnect verifies that rapid Connect/Disconnect
// cycling does not accumulate phantom uptime. Each cycle should only account
// for the exact clock delta during that connection.
func TestUptimeTrackerRapidConnectDisconnect(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now.Add(-time.Hour))
	tracker := newUptimeTracker(state, netID, clk)

	// Rapid connect/disconnect 100 times with no clock advance.
	for i := 0; i < 100; i++ {
		tracker.Connect(nodeID)
		require.NoError(tracker.Disconnect(nodeID))
	}

	// No time passed, so uptime should be 0.
	require.Equal(time.Duration(0), state.uptimes[nodeID])

	// Now do 50 cycles with 1ms advance each.
	for i := 0; i < 50; i++ {
		tracker.Connect(nodeID)
		now = now.Add(1 * time.Millisecond)
		require.NoError(tracker.Disconnect(nodeID))
	}

	// Should be exactly 50ms of uptime.
	require.Equal(50*time.Millisecond, state.uptimes[nodeID])
}

// TestUptimeTrackerShutdownFlushesAll verifies that Shutdown flushes all
// currently connected validators and leaves the connected map empty.
func TestUptimeTrackerShutdownFlushesAll(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()

	now := time.Now()
	clk := func() time.Time { return now }

	const numValidators = 10
	nodeIDs := make([]ids.NodeID, numValidators)
	for i := range nodeIDs {
		nodeIDs[i] = ids.GenerateTestNodeID()
		state.addValidator(nodeIDs[i], netID, now.Add(-time.Hour))
	}

	tracker := newUptimeTracker(state, netID, clk)

	// Connect all
	for _, nid := range nodeIDs {
		tracker.Connect(nid)
	}

	now = now.Add(3 * time.Minute)
	require.NoError(tracker.Shutdown())

	// All should have 3 minutes of uptime
	for _, nid := range nodeIDs {
		require.Equal(3*time.Minute, state.uptimes[nid])
	}

	// Connected map should be empty after shutdown
	tracker.mu.RLock()
	require.Len(tracker.connected, 0)
	tracker.mu.RUnlock()
}

// TestUptimeTrackerNeverConnected verifies that a validator that was never
// connected has 0% uptime.
func TestUptimeTrackerNeverConnected(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	// Validator has been registered for 1 hour but never connected
	state.addValidator(nodeID, netID, now.Add(-time.Hour))
	tracker := newUptimeTracker(state, netID, clk)

	pct, err := tracker.CalculateUptimePercent(nodeID, netID)
	require.NoError(err)
	require.Equal(0.0, pct, "never-connected validator must have 0%% uptime")
}

// TestUptimeTrackerAlwaysConnected verifies that a validator connected for
// the entire tracking period reports 100% uptime.
func TestUptimeTrackerAlwaysConnected(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	startTime := now
	state.addValidator(nodeID, netID, startTime)
	tracker := newUptimeTracker(state, netID, clk)

	// Connect immediately at start
	tracker.Connect(nodeID)

	// Advance clock by 1 hour
	now = now.Add(time.Hour)

	pct, err := tracker.CalculateUptimePercent(nodeID, netID)
	require.NoError(err)
	require.InDelta(1.0, pct, 0.001, "always-connected validator must have ~100%% uptime")
}

// TestUptimeTrackerConcurrentConnect verifies that concurrent Connect calls
// from multiple goroutines do not cause data races or phantom uptime.
func TestUptimeTrackerConcurrentConnect(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now.Add(-time.Hour))
	tracker := newUptimeTracker(state, netID, clk)

	// Race 50 goroutines calling Connect on the same node.
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			tracker.Connect(nodeID)
		}()
	}
	wg.Wait()

	// Should have exactly one entry in connected map.
	tracker.mu.RLock()
	_, exists := tracker.connected[nodeID]
	require.True(exists)
	tracker.mu.RUnlock()

	// Advance clock and disconnect
	now = now.Add(5 * time.Minute)
	require.NoError(tracker.Disconnect(nodeID))

	// Uptime should be exactly 5 minutes, not 5*50 minutes.
	require.Equal(5*time.Minute, state.uptimes[nodeID])
}

// TestUptimeTrackerConcurrentConnectDisconnect races Connect and Disconnect
// from multiple goroutines on the same node.
func TestUptimeTrackerConcurrentConnectDisconnect(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	mu := sync.Mutex{}
	clk := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	state.addValidator(nodeID, netID, now.Add(-time.Hour))
	tracker := newUptimeTracker(state, netID, clk)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				tracker.Connect(nodeID)
			} else {
				_ = tracker.Disconnect(nodeID)
			}
		}(i)
	}
	wg.Wait()

	// Should not panic. Final state depends on ordering but must be consistent.
	// Clean up: ensure we can still shutdown.
	mu.Lock()
	now = now.Add(time.Minute)
	mu.Unlock()
	require.NoError(tracker.Shutdown())
}

func TestUptimeTrackerCalculateUptimePercentFrom(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	startTime := now.Add(-2 * time.Hour)
	state.addValidator(nodeID, netID, startTime)
	tracker := newUptimeTracker(state, netID, clk)

	// Connect for 1 hour.
	tracker.Connect(nodeID)
	now = now.Add(time.Hour)
	require.NoError(tracker.Disconnect(nodeID))

	// Total time is 3 hours (2h before + 1h connected).
	// Connected for 1 hour. Rate = 1/3.
	// From startTime: same rate applies.
	pct, err := tracker.CalculateUptimePercentFrom(nodeID, netID, startTime)
	require.NoError(err)
	require.InDelta(0.333, pct, 0.01)
}
