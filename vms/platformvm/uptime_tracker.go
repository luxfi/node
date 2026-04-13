// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/validators/uptime"
)

// uptimeTracker implements uptime.Calculator by tracking peer connections
// and computing real uptime percentages from persistent state.
//
// It wraps an uptime.State (backed by platformvm state) that stores
// cumulative upDuration and lastUpdated per validator. On Connect, it
// records the connection time. On Disconnect, it flushes the elapsed
// connected time into the persistent state. CalculateUptimePercent
// reads from state and accounts for any currently-connected time.
type uptimeTracker struct {
	mu        sync.RWMutex
	clk       func() time.Time
	state     uptime.State
	netID     ids.ID
	connected map[ids.NodeID]time.Time // nodeID -> time they connected
}

func newUptimeTracker(state uptime.State, netID ids.ID, clk func() time.Time) *uptimeTracker {
	return &uptimeTracker{
		clk:       clk,
		state:     state,
		netID:     netID,
		connected: make(map[ids.NodeID]time.Time),
	}
}

// Connect records that a validator connected.
func (t *uptimeTracker) Connect(nodeID ids.NodeID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.connected[nodeID]; ok {
		return // already connected
	}
	t.connected[nodeID] = t.clk()
}

// Disconnect records that a validator disconnected and flushes
// the accumulated uptime into persistent state.
func (t *uptimeTracker) Disconnect(nodeID ids.NodeID) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.disconnectLocked(nodeID)
}

func (t *uptimeTracker) disconnectLocked(nodeID ids.NodeID) error {
	connectedAt, ok := t.connected[nodeID]
	if !ok {
		return nil // wasn't connected
	}
	delete(t.connected, nodeID)
	now := t.clk()
	elapsed := now.Sub(connectedAt)
	return t.addUptime(nodeID, elapsed, now)
}

// addUptime adds elapsed duration to the validator's persistent uptime.
func (t *uptimeTracker) addUptime(nodeID ids.NodeID, elapsed time.Duration, now time.Time) error {
	upDuration, _, err := t.state.GetUptime(nodeID, t.netID)
	if err != nil {
		// Validator not in state (e.g., not a current validator). Skip.
		return nil
	}
	return t.state.SetUptime(nodeID, t.netID, upDuration+elapsed, now)
}

// Shutdown flushes all connected validators' uptime to state.
// Call this before persisting state on node shutdown.
func (t *uptimeTracker) Shutdown() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for nodeID := range t.connected {
		if err := t.disconnectLocked(nodeID); err != nil {
			return err
		}
	}
	return nil
}

// CalculateUptime returns (upDuration, totalDuration, error) for a validator.
func (t *uptimeTracker) CalculateUptime(nodeID ids.NodeID, netID ids.ID) (time.Duration, time.Duration, error) {
	if netID != t.netID {
		return 0, 0, nil
	}

	startTime, err := t.state.GetStartTime(nodeID, netID)
	if err != nil {
		return 0, 0, err
	}

	now := t.clk()
	totalDuration := now.Sub(startTime)
	if totalDuration <= 0 {
		return 0, 0, nil
	}

	upDuration, _, err := t.state.GetUptime(nodeID, netID)
	if err != nil {
		return 0, totalDuration, nil
	}

	// Add any currently-connected time that hasn't been flushed yet.
	t.mu.RLock()
	if connectedAt, ok := t.connected[nodeID]; ok {
		upDuration += now.Sub(connectedAt)
	}
	t.mu.RUnlock()

	if upDuration > totalDuration {
		upDuration = totalDuration
	}
	return upDuration, totalDuration, nil
}

// CalculateUptimePercent returns the uptime as a fraction in [0, 1].
func (t *uptimeTracker) CalculateUptimePercent(nodeID ids.NodeID, netID ids.ID) (float64, error) {
	if netID != t.netID {
		return 0, nil
	}
	upDuration, totalDuration, err := t.CalculateUptime(nodeID, netID)
	if err != nil {
		return 0, err
	}
	if totalDuration == 0 {
		return 1, nil // no time elapsed, consider 100%
	}
	return float64(upDuration) / float64(totalDuration), nil
}

// CalculateUptimePercentFrom returns the uptime as a fraction since [from].
func (t *uptimeTracker) CalculateUptimePercentFrom(nodeID ids.NodeID, netID ids.ID, from time.Time) (float64, error) {
	if netID != t.netID {
		return 0, nil
	}

	now := t.clk()
	totalDuration := now.Sub(from)
	if totalDuration <= 0 {
		return 1, nil
	}

	upDuration, _, err := t.state.GetUptime(nodeID, netID)
	if err != nil {
		return 0, nil
	}

	// Subtract uptime before [from] by using startTime.
	// If from > startTime, some of the stored upDuration may predate [from].
	// We approximate by assuming the same uptime rate.
	startTime, err := t.state.GetStartTime(nodeID, netID)
	if err != nil {
		return 0, nil
	}

	totalSinceStart := now.Sub(startTime)
	if totalSinceStart <= 0 {
		return 1, nil
	}

	// Add any currently-connected time.
	t.mu.RLock()
	if connectedAt, ok := t.connected[nodeID]; ok {
		upDuration += now.Sub(connectedAt)
	}
	t.mu.RUnlock()

	if upDuration > totalSinceStart {
		upDuration = totalSinceStart
	}

	// Scale upDuration to the [from, now] window.
	if from.After(startTime) {
		rate := float64(upDuration) / float64(totalSinceStart)
		return rate, nil
	}

	// from <= startTime, just use overall rate.
	return float64(upDuration) / float64(totalSinceStart), nil
}

// SetCalculator is a no-op; this tracker doesn't delegate.
func (t *uptimeTracker) SetCalculator(ids.ID, uptime.Calculator) error {
	return nil
}
