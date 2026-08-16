// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
)

// fakeUptimeState implements uptime.State for testing, mirroring how the real
// platformvm state stores uptime: an up-duration plus a second-granular
// lastUpdated, returned as a "duration since the Unix epoch". Validators must be
// registered first; GetUptime/SetUptime on an unregistered node return
// database.ErrNotFound, exactly like metadata_validator.go.
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

func (f *fakeUptimeState) addValidator(nodeID ids.NodeID, _ ids.ID, startTime time.Time) {
	f.uptimes[nodeID] = 0
	f.lastUpdate[nodeID] = startTime
	f.startTimes[nodeID] = startTime
}

func (f *fakeUptimeState) GetUptime(nodeID ids.NodeID, _ ids.ID) (time.Duration, time.Duration, error) {
	up, ok := f.uptimes[nodeID]
	if !ok {
		return 0, 0, database.ErrNotFound
	}
	lastUpdatedDuration := time.Duration(f.lastUpdate[nodeID].Unix()) * time.Second
	return up, lastUpdatedDuration, nil
}

func (f *fakeUptimeState) SetUptime(nodeID ids.NodeID, _ ids.ID, uptime time.Duration, lastUpdated time.Time) error {
	if _, ok := f.uptimes[nodeID]; !ok {
		return database.ErrNotFound
	}
	f.uptimes[nodeID] = uptime
	f.lastUpdate[nodeID] = lastUpdated
	return nil
}

func (f *fakeUptimeState) GetStartTime(nodeID ids.NodeID, _ ids.ID) (time.Time, error) {
	st, ok := f.startTimes[nodeID]
	if !ok {
		return time.Time{}, database.ErrNotFound
	}
	return st, nil
}

// TestUptimeTrackerLongRunningValidatorAccruesUptime is the regression test for
// the ~165M LUX reward gate. A validator that has been staked for 30 days must
// report ~100% uptime, not 0%.
//
// This test uses ONLY the shared Calculator surface (newUptimeTracker +
// CalculateUptimePercentFrom) that both the old and the fixed tracker expose, so
// it can be run against either implementation:
//   - OLD tracker: returns ~0.0 — stored upDuration is 0 and the tracker had no
//     baseline for the un-measured window, so upDuration/total = 0/30d = 0.
//   - FIXED tracker: returns ~1.0 — before tracking begins, a validator is
//     assumed online since its last persisted update.
func TestUptimeTrackerLongRunningValidatorAccruesUptime(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now()
	clk := func() time.Time { return now }

	startTime := now.Add(-30 * 24 * time.Hour) // staked 30 days ago
	state.addValidator(nodeID, netID, startTime)

	tracker := newUptimeTracker(state, netID, clk)

	pct, err := tracker.CalculateUptimePercentFrom(nodeID, netID, startTime)
	require.NoError(err)
	require.InDelta(1.0, pct, 0.001, "a continuously-staked validator must report ~100%% uptime, not 0%%")
}

// TestUptimeTrackerStartTrackingBaselines verifies that StartTracking credits the
// un-measured pre-tracking window (assuming the validator was online) and moves
// the tracker into live-tracking mode.
func TestUptimeTrackerStartTrackingBaselines(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	startTime := now.Add(-time.Hour)
	state.addValidator(nodeID, netID, startTime)

	tracker := newUptimeTracker(state, netID, clk)
	require.False(tracker.StartedTracking())

	require.NoError(tracker.StartTracking([]ids.NodeID{nodeID}))
	require.True(tracker.StartedTracking())

	// The hour since lastUpdated (== startTime) is baked into the persisted
	// up-duration, and lastUpdated advanced to now.
	require.Equal(time.Hour, state.uptimes[nodeID])
	require.Equal(now.Unix(), state.lastUpdate[nodeID].Unix())
}

// TestUptimeTrackerContinuouslyConnectedClimbs is the core behavioral fix: a
// validator that connects (during bootstrap) and stays connected accrues uptime
// over time WITHOUT ever disconnecting, and IsConnected reports true.
func TestUptimeTrackerContinuouslyConnectedClimbs(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	startTime := now
	state.addValidator(nodeID, netID, startTime)

	tracker := newUptimeTracker(state, netID, clk)

	// Peer connects during bootstrap, before tracking starts.
	tracker.Connect(nodeID)
	require.True(tracker.IsConnected(nodeID))

	// Normal operations begin.
	require.NoError(tracker.StartTracking([]ids.NodeID{nodeID}))

	// One hour passes with the validator continuously connected — no Disconnect.
	now = now.Add(time.Hour)

	pct, err := tracker.CalculateUptimePercent(nodeID, netID)
	require.NoError(err)
	require.InDelta(1.0, pct, 0.001, "continuously-connected validator must climb to ~100%%")
	require.True(tracker.IsConnected(nodeID))

	// Two hours in, still ~100%.
	now = now.Add(time.Hour)
	pct, err = tracker.CalculateUptimePercent(nodeID, netID)
	require.NoError(err)
	require.InDelta(1.0, pct, 0.001)
}

// TestUptimeTrackerConnectDisconnectFlush verifies that, once tracking, a
// connected session is flushed into persistent state on Disconnect.
func TestUptimeTrackerConnectDisconnectFlush(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	startTime := now
	state.addValidator(nodeID, netID, startTime)

	tracker := newUptimeTracker(state, netID, clk)
	require.NoError(tracker.StartTracking([]ids.NodeID{nodeID}))
	require.Equal(time.Duration(0), state.uptimes[nodeID])

	tracker.Connect(nodeID)
	now = now.Add(30 * time.Minute)
	mutated, err := tracker.Disconnect(nodeID)
	require.NoError(err)
	require.True(mutated) // a tracked validator's session flush writes uptime

	require.Equal(30*time.Minute, state.uptimes[nodeID])
	require.False(tracker.IsConnected(nodeID))

	// After disconnect, no further live-session bonus; percent reflects 30m/60m.
	now = now.Add(30 * time.Minute)
	pct, err := tracker.CalculateUptimePercent(nodeID, netID)
	require.NoError(err)
	require.InDelta(0.5, pct, 0.001)
}

// TestUptimeTrackerDisconnectBeforeTrackingIsNoWrite is the regression test for
// the empty-commit elimination (RED round-2 LOW). A peer disconnect during
// bootstrap — before StartTracking — must report mutated=false so VM.Disconnected
// skips an empty state.Commit (a full-write+fsync). Under bootstrap churn that
// commit ran on every disconnect for no reason.
func TestUptimeTrackerDisconnectBeforeTrackingIsNoWrite(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now)

	tracker := newUptimeTracker(state, netID, clk)
	// Deliberately NO StartTracking — we are still bootstrapping.

	tracker.Connect(nodeID)
	now = now.Add(30 * time.Minute)

	mutated, err := tracker.Disconnect(nodeID)
	require.NoError(err)
	require.False(mutated) // no write → VM.Disconnected must skip Commit
	require.False(tracker.IsConnected(nodeID))
	require.Equal(time.Duration(0), state.uptimes[nodeID]) // untouched
}

// TestUptimeTrackerDisconnectedValidatorGetsZero verifies that, once tracking, a
// validator that never connects earns 0% uptime (it is offline from this node's
// perspective) — the property that lets the reward gate withhold rewards.
func TestUptimeTrackerDisconnectedValidatorGetsZero(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	startTime := now
	state.addValidator(nodeID, netID, startTime)

	tracker := newUptimeTracker(state, netID, clk)
	require.NoError(tracker.StartTracking([]ids.NodeID{nodeID}))

	now = now.Add(time.Hour) // an hour passes, never connected

	pct, err := tracker.CalculateUptimePercent(nodeID, netID)
	require.NoError(err)
	require.InDelta(0.0, pct, 0.001, "never-connected validator (while tracking) must earn 0%%")
	require.False(tracker.IsConnected(nodeID))
}

// TestUptimeTrackerStopTrackingFlushesAll verifies that StopTracking persists all
// connected validators' sessions and leaves tracking mode.
func TestUptimeTrackerStopTrackingFlushesAll(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	const numValidators = 10
	nodeIDs := make([]ids.NodeID, numValidators)
	for i := range nodeIDs {
		nodeIDs[i] = ids.GenerateTestNodeID()
		state.addValidator(nodeIDs[i], netID, now)
	}

	tracker := newUptimeTracker(state, netID, clk)
	require.NoError(tracker.StartTracking(nodeIDs))
	for _, nid := range nodeIDs {
		tracker.Connect(nid)
	}

	now = now.Add(3 * time.Minute)
	require.NoError(tracker.StopTracking(nodeIDs))
	require.False(tracker.StartedTracking())

	for _, nid := range nodeIDs {
		require.Equal(3*time.Minute, state.uptimes[nid])
	}
}

// TestUptimeTrackerDoubleStartTracking verifies StartTracking is not re-entrant.
func TestUptimeTrackerDoubleStartTracking(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	now := time.Now().Truncate(time.Second)

	state.addValidator(nodeID, netID, now)
	tracker := newUptimeTracker(state, netID, func() time.Time { return now })

	require.NoError(tracker.StartTracking([]ids.NodeID{nodeID}))
	require.ErrorIs(tracker.StartTracking([]ids.NodeID{nodeID}), errAlreadyStartedTracking)
}

// TestUptimeTrackerStopWithoutStart verifies StopTracking errors before start.
func TestUptimeTrackerStopWithoutStart(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	now := time.Now().Truncate(time.Second)
	tracker := newUptimeTracker(state, netID, func() time.Time { return now })

	require.ErrorIs(tracker.StopTracking(nil), errNotStartedTracking)
}

// TestUptimeTrackerUnknownValidator verifies non-validators are handled safely:
// StartTracking/Connect/Disconnect skip them without error, and a percent query
// surfaces the not-found error.
func TestUptimeTrackerUnknownValidator(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	unknownNode := ids.GenerateTestNodeID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	tracker := newUptimeTracker(state, netID, clk)

	// StartTracking must not fail on a node that has no state record.
	require.NoError(tracker.StartTracking([]ids.NodeID{unknownNode}))

	// Connecting then disconnecting an unknown node is a no-op, not an error, and
	// must NOT report a state mutation (no uptime record to write).
	tracker.Connect(unknownNode)
	now = now.Add(time.Minute)
	mutated, err := tracker.Disconnect(unknownNode)
	require.NoError(err)
	require.False(mutated)

	// A percent query for an unknown validator surfaces the error.
	_, err = tracker.CalculateUptimePercent(unknownNode, netID)
	require.Error(err)
}

// TestUptimeTrackerWrongNet verifies a query for a different network returns 0.
func TestUptimeTrackerWrongNet(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	otherNet := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now.Add(-time.Hour))
	tracker := newUptimeTracker(state, netID, clk)

	pct, err := tracker.CalculateUptimePercent(nodeID, otherNet)
	require.NoError(err)
	require.Equal(0.0, pct)

	up, total, err := tracker.CalculateUptime(nodeID, otherNet)
	require.NoError(err)
	require.Equal(time.Duration(0), up)
	require.Equal(time.Duration(0), total)
}

// TestUptimeTrackerDoubleConnect verifies a duplicate Connect keeps the original
// connection time (repeated router dispatch cannot inflate uptime).
func TestUptimeTrackerDoubleConnect(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now)
	tracker := newUptimeTracker(state, netID, clk)
	require.NoError(tracker.StartTracking([]ids.NodeID{nodeID}))

	tracker.Connect(nodeID)
	now = now.Add(5 * time.Minute)
	tracker.Connect(nodeID) // duplicate — must NOT reset the 5-minute-old session
	now = now.Add(5 * time.Minute)
	mutated, err := tracker.Disconnect(nodeID)
	require.NoError(err)
	require.True(mutated)

	// Ten minutes total, not five.
	require.Equal(10*time.Minute, state.uptimes[nodeID])
}

// TestUptimeTrackerRapidConnectDisconnect verifies rapid cycling never fabricates
// uptime beyond the exact connected intervals.
func TestUptimeTrackerRapidConnectDisconnect(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	now := time.Now().Truncate(time.Second)
	clk := func() time.Time { return now }

	state.addValidator(nodeID, netID, now)
	tracker := newUptimeTracker(state, netID, clk)
	require.NoError(tracker.StartTracking([]ids.NodeID{nodeID}))

	// 100 cycles with no clock advance — zero accrual.
	for i := 0; i < 100; i++ {
		tracker.Connect(nodeID)
		_, err := tracker.Disconnect(nodeID)
		require.NoError(err)
	}
	require.Equal(time.Duration(0), state.uptimes[nodeID])

	// 50 cycles, each holding the connection for one second.
	for i := 0; i < 50; i++ {
		tracker.Connect(nodeID)
		now = now.Add(time.Second)
		_, err := tracker.Disconnect(nodeID)
		require.NoError(err)
	}
	require.Equal(50*time.Second, state.uptimes[nodeID])
}

// TestUptimeTrackerConcurrent races Connect/Disconnect/reads and StartTracking to
// assert there are no data races (run with -race).
func TestUptimeTrackerConcurrent(t *testing.T) {
	require := require.New(t)

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	var clkMu sync.Mutex
	now := time.Now().Truncate(time.Second)
	clk := func() time.Time {
		clkMu.Lock()
		defer clkMu.Unlock()
		return now
	}

	state.addValidator(nodeID, netID, now)
	tracker := newUptimeTracker(state, netID, clk)
	require.NoError(tracker.StartTracking([]ids.NodeID{nodeID}))

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			switch idx % 4 {
			case 0:
				tracker.Connect(nodeID)
			case 1:
				_, _ = tracker.Disconnect(nodeID)
			case 2:
				_, _ = tracker.CalculateUptimePercent(nodeID, netID)
			case 3:
				_ = tracker.IsConnected(nodeID)
			}
		}(i)
	}
	wg.Wait()

	clkMu.Lock()
	now = now.Add(time.Minute)
	clkMu.Unlock()
	require.NoError(tracker.StopTracking([]ids.NodeID{nodeID}))
}
