// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/throttling/tracker"
)

// mockTracker is a test implementation of tracker.Tracker
type mockTracker struct {
	UsageF           func(ids.NodeID, time.Time) uint64
	TimeUntilUsageF  func(ids.NodeID, uint64) time.Duration
	TotalUsageF      func() uint64
}

func (m *mockTracker) TotalUsage() uint64 {
	if m.TotalUsageF != nil {
		return m.TotalUsageF()
	}
	return 0
}

func (m *mockTracker) Usage(nodeID ids.NodeID, at time.Time) uint64 {
	if m.UsageF != nil {
		return m.UsageF(nodeID, at)
	}
	return 0
}

func (m *mockTracker) Add(nodeID ids.NodeID, usage uint64) {
	// No-op for tests
}

func (m *mockTracker) Remove(nodeID ids.NodeID, usage uint64) {
	// No-op for tests
}

func (m *mockTracker) Len() int {
	return 0
}

func (m *mockTracker) TimeUntilUsage(nodeID ids.NodeID, targetUsage uint64) time.Duration {
	if m.TimeUntilUsageF != nil {
		return m.TimeUntilUsageF(nodeID, targetUsage)
	}
	return 0
}

// mockTargeter is a test implementation of tracker.Targeter
type mockTargeter struct {
	TargetUsageF func(ids.NodeID) uint64
}

func (m *mockTargeter) TargetUsage(nodeID ids.NodeID) uint64 {
	if m.TargetUsageF != nil {
		return m.TargetUsageF(nodeID)
	}
	return 0
}

// Verify interfaces are implemented
var (
	_ tracker.Tracker  = &mockTracker{}
	_ tracker.Targeter = &mockTargeter{}
)