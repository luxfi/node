// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"
	"time"

	"github.com/luxfi/ids"
)

// Targeter defines resource targets
type Targeter interface {
	// TargetUsage returns the target resource usage for a node
	TargetUsage(nodeID ids.NodeID) uint64
}

// Tracker tracks resource usage
type Tracker interface {
	// TotalUsage returns the total resource usage
	TotalUsage() uint64

	// Usage returns the resource usage for a specific node at a specific time
	Usage(nodeID ids.NodeID, at time.Time) uint64

	// Add adds resource usage
	Add(nodeID ids.NodeID, usage uint64)

	// Remove removes resource usage
	Remove(nodeID ids.NodeID, usage uint64)

	// Len returns the number of tracked nodes
	Len() int

	// TimeUntilUsage returns the time until the usage threshold is reached
	TimeUntilUsage(nodeID ids.NodeID, targetUsage uint64) time.Duration
}

// ResourceTracker tracks resources by type
type ResourceTracker interface {
	Tracker

	// GetTracker returns a tracker for a specific resource type
	GetTracker(resourceType string) Tracker

	// CPUTracker returns the CPU resource tracker
	CPUTracker() Tracker

	// DiskTracker returns the disk resource tracker
	DiskTracker() Tracker
}

// resourceTracker implements ResourceTracker
type resourceTracker struct {
	lock     sync.RWMutex
	trackers map[string]Tracker
}

// NewResourceTracker creates a new resource tracker
func NewResourceTracker() ResourceTracker {
	return &resourceTracker{
		trackers: make(map[string]Tracker),
	}
}

func (r *resourceTracker) GetTracker(resourceType string) Tracker {
	r.lock.Lock()
	defer r.lock.Unlock()

	if tracker, exists := r.trackers[resourceType]; exists {
		return tracker
	}

	tracker := NewTracker()
	r.trackers[resourceType] = tracker
	return tracker
}

func (r *resourceTracker) CPUTracker() Tracker {
	return r.GetTracker("cpu")
}

func (r *resourceTracker) DiskTracker() Tracker {
	return r.GetTracker("disk")
}

func (r *resourceTracker) TotalUsage() uint64 {
	r.lock.RLock()
	defer r.lock.RUnlock()

	var total uint64
	for _, tracker := range r.trackers {
		total += tracker.TotalUsage()
	}
	return total
}

func (r *resourceTracker) Usage(nodeID ids.NodeID, at time.Time) uint64 {
	r.lock.RLock()
	defer r.lock.RUnlock()

	var total uint64
	for _, tracker := range r.trackers {
		total += tracker.Usage(nodeID, at)
	}
	return total
}

func (r *resourceTracker) Add(nodeID ids.NodeID, usage uint64) {
	// Not implemented for aggregate tracker
}

func (r *resourceTracker) Remove(nodeID ids.NodeID, usage uint64) {
	// Not implemented for aggregate tracker
}

func (r *resourceTracker) Len() int {
	r.lock.RLock()
	defer r.lock.RUnlock()

	// Return 0 for aggregate tracker
	return 0
}

func (r *resourceTracker) TimeUntilUsage(nodeID ids.NodeID, targetUsage uint64) time.Duration {
	// Not implemented for aggregate tracker
	return 0
}

// tracker implements Tracker
type tracker struct {
	lock       sync.RWMutex
	usage      map[ids.NodeID]uint64
	totalUsage uint64
}

// NewTracker creates a new tracker
func NewTracker() Tracker {
	return &tracker{
		usage: make(map[ids.NodeID]uint64),
	}
}

func (t *tracker) TotalUsage() uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.totalUsage
}

func (t *tracker) Usage(nodeID ids.NodeID, at time.Time) uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()
	// For now, ignore the time parameter and return current usage
	return t.usage[nodeID]
}

func (t *tracker) Add(nodeID ids.NodeID, usage uint64) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.usage[nodeID] += usage
	t.totalUsage += usage
}

func (t *tracker) Remove(nodeID ids.NodeID, usage uint64) {
	t.lock.Lock()
	defer t.lock.Unlock()

	currentUsage := t.usage[nodeID]
	if usage >= currentUsage {
		delete(t.usage, nodeID)
		t.totalUsage -= currentUsage
	} else {
		t.usage[nodeID] -= usage
		t.totalUsage -= usage
	}
}

func (t *tracker) Len() int {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return len(t.usage)
}

func (t *tracker) TimeUntilUsage(nodeID ids.NodeID, targetUsage uint64) time.Duration {
	t.lock.RLock()
	defer t.lock.RUnlock()
	
	currentUsage := t.usage[nodeID]
	if currentUsage >= targetUsage {
		return 0
	}
	
	// For now, return a default duration
	return time.Minute
}

// targeter implements Targeter
type targeter struct {
	targetUsage uint64
}

// NewTargeter creates a new targeter
func NewTargeter(targetUsage uint64) Targeter {
	return &targeter{
		targetUsage: targetUsage,
	}
}

func (t *targeter) TargetUsage(nodeID ids.NodeID) uint64 {
	// Return the same target for all nodes
	return t.targetUsage
}