// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/net/endpoints"
)

// TestIPTracker_MaxTrackedIPsLimit verifies that the IP tracker enforces
// the maximum tracked IPs limit to prevent memory exhaustion.
func TestIPTracker_MaxTrackedIPsLimit(t *testing.T) {
	require := require.New(t)

	tracker, err := newIPTracker(
		set.Set[ids.ID]{},
		log.NewNoOpLogger(),
		metric.NewRegistry(),
	)
	require.NoError(err)

	// Add MaxTrackedIPs + 10 nodes with IPs
	excessNodes := 10
	totalNodes := MaxTrackedIPs + excessNodes

	for i := 0; i < totalNodes; i++ {
		nodeID := ids.GenerateTestNodeID()
		tracker.ManuallyTrack(nodeID)

		// Give each node an IP so eviction can work properly
		ip := &endpoints.ClaimedIPPort{
			Timestamp: uint64(1000 + i),
			NodeID:    nodeID,
		}
		tracker.Connected(ip, set.Set[ids.ID]{})
		tracker.Disconnected(nodeID) // Make eligible for eviction
	}

	// Verify that the tracker never exceeds MaxTrackedIPs
	tracker.lock.RLock()
	actualCount := len(tracker.tracked)
	tracker.lock.RUnlock()

	require.LessOrEqual(actualCount, MaxTrackedIPs,
		"IP tracker should not exceed MaxTrackedIPs limit")
}

// TestIPTracker_BloomFilterSizeLimit verifies that bloom filter size
// is capped at MaxBloomFilterEntries to prevent unbounded growth.
func TestIPTracker_BloomFilterSizeLimit(t *testing.T) {
	require := require.New(t)

	tracker, err := newIPTracker(
		set.Set[ids.ID]{},
		log.NewNoOpLogger(),
		metric.NewRegistry(),
	)
	require.NoError(err)

	// Fill tracker to maximum
	for i := 0; i < MaxTrackedIPs; i++ {
		nodeID := ids.GenerateTestNodeID()
		tracker.ManuallyTrack(nodeID)

		// Add IP for each node
		ip := &endpoints.ClaimedIPPort{
			Cert:      nil,
			Timestamp: uint64(time.Now().Unix()),
			NodeID:    nodeID,
			Signature: nil,
		}
		tracker.Connected(ip, set.Set[ids.ID]{})
	}

	// Force bloom filter reset
	err = tracker.ResetBloom()
	require.NoError(err)

	// Verify bloom filter parameters are within bounds
	tracker.lock.RLock()
	bloomSize := len(tracker.bloom.Marshal())
	tracker.lock.RUnlock()

	// The bloom filter size should be reasonable
	// Each entry is a byte, so MaxBloomFilterEntries bytes is the theoretical max
	require.Less(bloomSize, MaxBloomFilterEntries*2,
		"Bloom filter exceeded expected size bounds")
}

// TestIPTracker_EvictsOldestFirst verifies that when the tracker is full,
// it evicts the oldest tracked IP that can be deleted.
func TestIPTracker_EvictsOldestFirst(t *testing.T) {
	require := require.New(t)

	tracker, err := newIPTracker(
		set.Set[ids.ID]{},
		log.NewNoOpLogger(),
		metric.NewRegistry(),
	)
	require.NoError(err)

	// Fill tracker to near maximum
	oldestNodeID := ids.GenerateTestNodeID()
	tracker.ManuallyTrack(oldestNodeID)

	oldestIP := &endpoints.ClaimedIPPort{
		Cert:      nil,
		Timestamp: 1000, // Old timestamp
		NodeID:    oldestNodeID,
		Signature: nil,
	}
	tracker.Connected(oldestIP, set.Set[ids.ID]{})
	tracker.Disconnected(oldestNodeID) // Make it eligible for eviction

	// Fill remaining slots
	for i := 1; i < MaxTrackedIPs; i++ {
		nodeID := ids.GenerateTestNodeID()
		tracker.ManuallyTrack(nodeID)

		ip := &endpoints.ClaimedIPPort{
			Cert:      nil,
			Timestamp: uint64(2000 + i), // Newer timestamps
			NodeID:    nodeID,
			Signature: nil,
		}
		tracker.Connected(ip, set.Set[ids.ID]{})
		tracker.Disconnected(nodeID)
	}

	// Add one more node to trigger eviction
	newNodeID := ids.GenerateTestNodeID()
	tracker.ManuallyTrack(newNodeID)

	// Verify the oldest node was evicted
	tracker.lock.RLock()
	_, stillTracked := tracker.tracked[oldestNodeID]
	_, newTracked := tracker.tracked[newNodeID]
	tracker.lock.RUnlock()

	require.False(stillTracked, "Oldest node should have been evicted")
	require.True(newTracked, "New node should be tracked")
}
