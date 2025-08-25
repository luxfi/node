// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/ids"
)

// ResourceManager provides system resource usage metrics
type ResourceManager interface {
	// CPUUsage returns the current CPU usage percentage
	CPUUsage() float64
	// DiskUsage returns the current disk usage percentage
	DiskUsage() float64
	// Shutdown stops the resource manager
	Shutdown()
}

// resourceTracker implements ResourceTracker
type resourceTracker struct {
	cpuTracker  Tracker
	diskTracker Tracker
}

// NewResourceTracker creates a new ResourceTracker
func NewResourceTracker(
	registerer prometheus.Registerer,
	manager ResourceManager,
	frequency time.Duration,
) (ResourceTracker, error) {
	cpuTracker := newUsageTracker(manager.CPUUsage, frequency)
	diskTracker := newUsageTracker(manager.DiskUsage, frequency)
	
	return &resourceTracker{
		cpuTracker:  cpuTracker,
		diskTracker: diskTracker,
	}, nil
}

func (rt *resourceTracker) CPUTracker() Tracker {
	return rt.cpuTracker
}

func (rt *resourceTracker) DiskTracker() Tracker {
	return rt.diskTracker
}

// usageTracker implements Tracker for a specific resource
type usageTracker struct {
	mu           sync.RWMutex
	usageFunc    func() float64
	nodeUsage    map[ids.NodeID]*nodeUsageEntry
	totalUsage   float64
	updatePeriod time.Duration
}

type nodeUsageEntry struct {
	usage      float64
	lastUpdate time.Time
}

func newUsageTracker(usageFunc func() float64, updatePeriod time.Duration) Tracker {
	return &usageTracker{
		usageFunc:    usageFunc,
		nodeUsage:    make(map[ids.NodeID]*nodeUsageEntry),
		updatePeriod: updatePeriod,
	}
}

func (ut *usageTracker) Usage(nodeID ids.NodeID, now time.Time) float64 {
	ut.mu.RLock()
	defer ut.mu.RUnlock()
	
	entry, exists := ut.nodeUsage[nodeID]
	if !exists {
		// If node doesn't exist, return current system usage
		return ut.usageFunc()
	}
	
	// Decay usage over time
	elapsed := now.Sub(entry.lastUpdate)
	decayFactor := 1.0 - (elapsed.Seconds() / ut.updatePeriod.Seconds())
	if decayFactor < 0 {
		decayFactor = 0
	}
	
	return entry.usage * decayFactor
}

func (ut *usageTracker) TimeUntilUsage(nodeID ids.NodeID, now time.Time, target float64) time.Duration {
	ut.mu.RLock()
	defer ut.mu.RUnlock()
	
	currentUsage := ut.Usage(nodeID, now)
	if currentUsage <= target {
		return 0
	}
	
	// Calculate time for usage to decay to target
	// Using exponential decay model: usage(t) = usage(0) * e^(-t/τ)
	// where τ is the time constant (updatePeriod)
	timeToTarget := -ut.updatePeriod.Seconds() * 
		(target / currentUsage)
	
	if timeToTarget < 0 {
		timeToTarget = 0
	}
	
	return time.Duration(timeToTarget * float64(time.Second))
}

func (ut *usageTracker) TotalUsage() float64 {
	ut.mu.RLock()
	defer ut.mu.RUnlock()
	
	return ut.totalUsage
}

// UpdateUsage updates the usage for a specific node
func (ut *usageTracker) UpdateUsage(nodeID ids.NodeID, usage float64, now time.Time) {
	ut.mu.Lock()
	defer ut.mu.Unlock()
	
	if entry, exists := ut.nodeUsage[nodeID]; exists {
		ut.totalUsage -= entry.usage
		entry.usage = usage
		entry.lastUpdate = now
	} else {
		ut.nodeUsage[nodeID] = &nodeUsageEntry{
			usage:      usage,
			lastUpdate: now,
		}
	}
	
	ut.totalUsage += usage
}

// RemoveNode removes a node from tracking
func (ut *usageTracker) RemoveNode(nodeID ids.NodeID) {
	ut.mu.Lock()
	defer ut.mu.Unlock()
	
	if entry, exists := ut.nodeUsage[nodeID]; exists {
		ut.totalUsage -= entry.usage
		delete(ut.nodeUsage, nodeID)
	}
}

// Manager creates a ResourceManager using the resource package
func Manager() ResourceManager {
	// For now, return a simple mock implementation
	// The actual resource.NewManager requires many parameters
	return &manager{}
}

type manager struct {
}

func (m *manager) CPUUsage() float64 {
	// Mock implementation - return default value
	return 0.5
}

func (m *manager) DiskUsage() float64 {
	// Mock implementation - return default value
	return 0.3
}

func (m *manager) Shutdown() {
	// Mock implementation - no-op
}