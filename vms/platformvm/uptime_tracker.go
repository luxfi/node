// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/validators/uptime"
)

var (
	errAlreadyStartedTracking = errors.New("uptime tracker already started tracking")
	errNotStartedTracking     = errors.New("uptime tracker has not started tracking")
)

// uptimeTracker implements uptime.Calculator plus the connection/tracking hooks
// the platform VM drives directly (Connect, Disconnect, StartTracking,
// StopTracking, IsConnected).
//
// It is a faithful port of avalanchego's snow/uptime.Manager, adapted to the
// luxfi validators uptime.State interface (which keys by netID and returns
// lastUpdated encoded as a second-granular duration since the Unix epoch).
//
// Model, in one sentence: each connected peer's session accrues into a per
// validator up-duration that is folded forward to "now" on read, and persisted
// on flush.
//
//   - Connect records a peer's connection time. It is captured from the very
//     first handshake — including during bootstrap, before StartTracking — so a
//     stable, continuously-connected validator set is fully observed the moment
//     tracking begins.
//   - StartTracking, called once at P-chain normal-operations start, baselines
//     every validator (crediting the pre-tracking window as online, matching
//     avalanchego) and switches into live-tracking mode.
//   - CalculateUptime folds the currently-connected session forward to now, so a
//     validator that stays connected accrues up-duration WITHOUT ever needing a
//     Disconnect.
//   - Disconnect / StopTracking flush the accrued session into persistent state.
//
// The prior custom tracker could not accrue uptime for a stable set: it never
// started tracking, only flushed on Disconnect, and — because peer connection
// events were never delivered to the VM — never populated its connected map.
type uptimeTracker struct {
	mu    sync.RWMutex
	clk   func() time.Time
	state uptime.State
	netID ids.ID

	// connections maps a currently-connected nodeID to the (second-granular)
	// time it connected.
	connections map[ids.NodeID]time.Time

	// startedTracking gates live-session accounting. Before StartTracking, a
	// validator is assumed online since its last persisted update (so the
	// bootstrap window is not spuriously counted as downtime). After
	// StartTracking, only genuinely-connected sessions accrue.
	startedTracking bool
}

func newUptimeTracker(state uptime.State, netID ids.ID, clk func() time.Time) *uptimeTracker {
	return &uptimeTracker{
		clk:         clk,
		state:       state,
		netID:       netID,
		connections: make(map[ids.NodeID]time.Time),
	}
}

// now returns the tracker clock truncated to second precision. lastUpdated is
// persisted at second granularity (state stores lastUpdated.Unix()), so working
// at one resolution keeps the interval arithmetic exact and side-steps
// monotonic-clock skew.
func (t *uptimeTracker) now() time.Time {
	return time.Unix(t.clk().Unix(), 0)
}

// lastUpdatedFromDuration reconstructs the persisted lastUpdated timestamp from
// the second-granular "duration since epoch" the uptime.State returns.
func lastUpdatedFromDuration(d time.Duration) time.Time {
	return time.Unix(int64(d/time.Second), 0)
}

// Connect records that [nodeID] connected. It is idempotent: a duplicate Connect
// for an already-connected node keeps the original connection time, so repeated
// router dispatch of the same live connection can never reset the session clock.
func (t *uptimeTracker) Connect(nodeID ids.NodeID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.connections[nodeID]; ok {
		return
	}
	t.connections[nodeID] = t.now()
}

// IsConnected reports whether [nodeID] currently has a live connection.
func (t *uptimeTracker) IsConnected(nodeID ids.NodeID) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, connected := t.connections[nodeID]
	return connected
}

// Disconnect records that [nodeID] disconnected, flushing its accrued session
// into persistent state. It returns whether that flush actually wrote uptime
// state (a SetUptime the caller must Commit). Flushing is a no-op — mutated
// false — before StartTracking (the session is not yet being measured, matching
// avalanchego) and for a peer with no uptime record (a non-validator), so the
// caller can skip an empty state.Commit.
func (t *uptimeTracker) Disconnect(nodeID ids.NodeID) (mutated bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	defer delete(t.connections, nodeID)
	if !t.startedTracking {
		return false, nil
	}
	return t.updateUptimeLocked(nodeID)
}

// StartTracking baselines every validator in [nodeIDs] and switches into live
// tracking mode. It is called once at P-chain normal-operations start. Each
// validator's persisted up-duration is advanced to now assuming it was online
// since its last update (the standard avalanchego assumption), so a validator
// that has been in the set is not penalized for the un-measured pre-tracking
// window.
func (t *uptimeTracker) StartTracking(nodeIDs []ids.NodeID) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.startedTracking {
		return errAlreadyStartedTracking
	}
	for _, nodeID := range nodeIDs {
		if _, err := t.updateUptimeLocked(nodeID); err != nil {
			return err
		}
	}
	t.startedTracking = true
	return nil
}

// StopTracking flushes every validator in [nodeIDs] and leaves tracking mode.
// It is called on the normal-ops → bootstrapping / shutdown transition so the
// connected sessions are durably persisted before the node stops measuring.
func (t *uptimeTracker) StopTracking(nodeIDs []ids.NodeID) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.startedTracking {
		return errNotStartedTracking
	}
	for _, nodeID := range nodeIDs {
		if _, err := t.updateUptimeLocked(nodeID); err != nil {
			return err
		}
	}
	t.startedTracking = false
	return nil
}

// StartedTracking reports whether StartTracking has run and StopTracking has not
// since. The VM lifecycle uses it to avoid double-start and to gate shutdown
// flushing.
func (t *uptimeTracker) StartedTracking() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.startedTracking
}

// calculateUptimeLocked mirrors avalanchego snow/uptime.Manager.CalculateUptime:
// it returns [nodeID]'s up-duration folded forward to now, plus that now (the
// new lastUpdated). The caller must hold t.mu (read or write).
func (t *uptimeTracker) calculateUptimeLocked(nodeID ids.NodeID) (time.Duration, time.Time, error) {
	upDuration, lastUpdatedDur, err := t.state.GetUptime(nodeID, t.netID)
	if err != nil {
		return 0, time.Time{}, err
	}
	lastUpdated := lastUpdatedFromDuration(lastUpdatedDur)
	now := t.now()

	// Clock skew: never subtract time or double-count.
	if now.Before(lastUpdated) {
		return upDuration, lastUpdated, nil
	}

	// Before tracking, assume the node was online since its last update.
	if !t.startedTracking {
		return upDuration + now.Sub(lastUpdated), now, nil
	}

	// Tracking, but not connected: offline since its last update.
	connectedAt, isConnected := t.connections[nodeID]
	if !isConnected {
		return upDuration, now, nil
	}

	// Tracking and connected: credit from the later of (connect, lastUpdated) so
	// no interval is double-counted.
	if connectedAt.Before(lastUpdated) {
		connectedAt = lastUpdated
	}
	if now.Before(connectedAt) {
		return upDuration, now, nil
	}
	return upDuration + now.Sub(connectedAt), now, nil
}

// updateUptimeLocked persists the current up-duration for [nodeID], returning
// whether it actually wrote (mutated). A node without a state record (i.e. not a
// current validator) is silently skipped — mutated false — so tracking
// non-validator peers is harmless and never dirties the state. The caller must
// hold t.mu (write).
func (t *uptimeTracker) updateUptimeLocked(nodeID ids.NodeID) (mutated bool, err error) {
	upDuration, lastUpdated, err := t.calculateUptimeLocked(nodeID)
	if errors.Is(err, database.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := t.state.SetUptime(nodeID, t.netID, upDuration, lastUpdated); err != nil {
		return false, err
	}
	return true, nil
}

// CalculateUptime returns (upDuration, totalDuration) for [nodeID], where
// totalDuration is the maximum possible uptime since the validator's start.
func (t *uptimeTracker) CalculateUptime(nodeID ids.NodeID, netID ids.ID) (time.Duration, time.Duration, error) {
	if netID != t.netID {
		return 0, 0, nil
	}

	startTime, err := t.state.GetStartTime(nodeID, netID)
	if err != nil {
		return 0, 0, err
	}

	t.mu.RLock()
	upDuration, _, err := t.calculateUptimeLocked(nodeID)
	t.mu.RUnlock()
	if err != nil {
		return 0, 0, err
	}

	total := t.now().Sub(startTime)
	if total < 0 {
		total = 0
	}
	return upDuration, total, nil
}

// CalculateUptimePercent returns [nodeID]'s uptime over its whole staking period
// as a fraction in [0, 1].
func (t *uptimeTracker) CalculateUptimePercent(nodeID ids.NodeID, netID ids.ID) (float64, error) {
	if netID != t.netID {
		return 0, nil
	}
	startTime, err := t.state.GetStartTime(nodeID, netID)
	if err != nil {
		return 0, err
	}
	return t.CalculateUptimePercentFrom(nodeID, netID, startTime)
}

// CalculateUptimePercentFrom returns [nodeID]'s uptime since [from] as a fraction
// in [0, 1].
func (t *uptimeTracker) CalculateUptimePercentFrom(nodeID ids.NodeID, netID ids.ID, from time.Time) (float64, error) {
	if netID != t.netID {
		return 0, nil
	}

	t.mu.RLock()
	upDuration, _, err := t.calculateUptimeLocked(nodeID)
	t.mu.RUnlock()
	if err != nil {
		return 0, err
	}

	bestPossible := t.now().Sub(from)
	if bestPossible <= 0 {
		return 1, nil
	}

	fraction := float64(upDuration) / float64(bestPossible)
	if fraction > 1 {
		fraction = 1
	}
	return fraction, nil
}

// SetCalculator is a no-op: this tracker is a concrete Calculator and does not
// delegate. It exists only to satisfy uptime.Calculator; registration is done by
// the enclosing uptime.LockedCalculator.
func (t *uptimeTracker) SetCalculator(ids.ID, uptime.Calculator) error {
	return nil
}
