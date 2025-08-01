// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/throttling/tracker"
	"github.com/luxfi/node/utils/timer/mockable"
)

func TestNewSystemThrottler(t *testing.T) {
	require := require.New(t)
	reg := prometheus.NewRegistry()
	clock := mockable.Clock{}
	clock.Set(time.Now())
	resourceTracker := tracker.NewResourceTracker()
	cpuTracker := resourceTracker.CPUTracker()

	config := SystemThrottlerConfig{
		Clock:           clock,
		MaxRecheckDelay: time.Second,
	}
	targeter := tracker.NewTargeter(10)
	throttlerIntf, err := NewSystemThrottler("", reg, config, cpuTracker, targeter)
	require.NoError(err)
	require.IsType(&systemThrottler{}, throttlerIntf)
	throttler := throttlerIntf.(*systemThrottler)
	require.Equal(clock, config.Clock)
	require.Equal(time.Second, config.MaxRecheckDelay)
	require.Equal(cpuTracker, throttler.tracker)
	require.Equal(targeter, throttler.targeter)
}

func TestSystemThrottler(t *testing.T) {
	require := require.New(t)

	// Setup
	maxRecheckDelay := 100 * time.Millisecond
	config := SystemThrottlerConfig{
		MaxRecheckDelay: maxRecheckDelay,
	}
	vdrID, nonVdrID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	
	// Track the calls for validation
	targetUsageCalls := make(map[ids.NodeID][]uint64)
	usageCalls := make(map[ids.NodeID][]uint64)
	
	mockTracker := &mockTracker{
		UsageF: func(nodeID ids.NodeID, currentTime time.Time) uint64 {
			calls := usageCalls[nodeID]
			if len(calls) == 0 {
				return 90
			}
			result := calls[0]
			usageCalls[nodeID] = calls[1:]
			return result
		},
		TimeUntilUsageF: func(nodeID ids.NodeID, usage uint64) time.Duration {
			return 100 * maxRecheckDelay
		},
	}
	
	mockTargeter := &mockTargeter{
		TargetUsageF: func(nodeID ids.NodeID) uint64 {
			calls := targetUsageCalls[nodeID]
			if len(calls) == 0 {
				return 100
			}
			result := calls[0]
			targetUsageCalls[nodeID] = calls[1:]
			return result
		},
	}
	
	throttler, err := NewSystemThrottler("", prometheus.NewRegistry(), config, mockTracker, mockTargeter)
	require.NoError(err)

	// Case: Actual usage <= target usage; should return immediately
	// for both validator and non-validator
	targetUsageCalls[vdrID] = []uint64{100}
	usageCalls[vdrID] = []uint64{90}

	throttler.Acquire(context.Background(), vdrID)

	targetUsageCalls[nonVdrID] = []uint64{100}
	usageCalls[nonVdrID] = []uint64{90}

	throttler.Acquire(context.Background(), nonVdrID)

	// Case: Actual usage > target usage; we should wait.
	// In the first loop iteration inside acquire,
	// say the actual usage exceeds the target.
	targetUsageCalls[vdrID] = []uint64{0, 100}
	usageCalls[vdrID] = []uint64{100, 0}
	// Note we'll only actually wait [maxRecheckDelay].

	onAcquire := make(chan struct{})

	// Check for validator
	go func() {
		throttler.Acquire(context.Background(), vdrID)
		onAcquire <- struct{}{}
	}()
	// Make sure the min re-check frequency is honored
	select {
	// Use 5*maxRecheckDelay and not just maxRecheckDelay to give a buffer
	// and avoid flakiness. If the min re-check freq isn't honored,
	// we'll wait [timeUntilAtDiskTarget].
	case <-time.After(5 * maxRecheckDelay):
		require.FailNow("should have returned after about [maxRecheckDelay]")
	case <-onAcquire:
	}

	targetUsageCalls[nonVdrID] = []uint64{0, 100}
	usageCalls[nonVdrID] = []uint64{100, 0}

	// Check for non-validator
	go func() {
		throttler.Acquire(context.Background(), nonVdrID)
		onAcquire <- struct{}{}
	}()
	// Make sure the min re-check frequency is honored
	select {
	// Use 5*maxRecheckDelay and not just maxRecheckDelay to give a buffer
	// and avoid flakiness. If the min re-check freq isn't honored,
	// we'll wait [timeUntilAtDiskTarget].
	case <-time.After(5 * maxRecheckDelay):
		require.FailNow("should have returned after about [maxRecheckDelay]")
	case <-onAcquire:
	}
}

func TestSystemThrottlerContextCancel(t *testing.T) {
	require := require.New(t)

	// Setup
	maxRecheckDelay := 10 * time.Second
	config := SystemThrottlerConfig{
		MaxRecheckDelay: maxRecheckDelay,
	}
	vdrID := ids.GenerateTestNodeID()
	
	mockTracker := &mockTracker{
		UsageF: func(nodeID ids.NodeID, currentTime time.Time) uint64 {
			return 100
		},
		TimeUntilUsageF: func(nodeID ids.NodeID, usage uint64) time.Duration {
			return maxRecheckDelay
		},
	}
	
	mockTargeter := &mockTargeter{
		TargetUsageF: func(nodeID ids.NodeID) uint64 {
			return 0
		},
	}
	
	throttler, err := NewSystemThrottler("", prometheus.NewRegistry(), config, mockTracker, mockTargeter)
	require.NoError(err)

	// Case: Actual usage > target usage; we should wait.
	// Mock the tracker so that the first loop iteration inside acquire,
	// it says the actual usage exceeds the target.
	// There should be no second iteration because we've already returned.
	onAcquire := make(chan struct{})
	// Pass a canceled context into Acquire so that it returns immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go func() {
		throttler.Acquire(ctx, vdrID)
		onAcquire <- struct{}{}
	}()
	select {
	case <-onAcquire:
	case <-time.After(maxRecheckDelay / 2):
		// Make sure Acquire returns well before the second check (i.e. "immediately")
		require.Fail("should have returned immediately")
	}
}
