// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/version"
)

// CPUTracker tracks CPU usage
type CPUTracker interface {
	// UtilizeTime marks that we were utilizing CPU for the given time
	UtilizeTime(time.Time, time.Time)

	// Utilization returns the current CPU utilization
	Utilization(time.Time, time.Duration) float64

	// Len returns the number of tracked intervals
	Len() int

	// TimeUntilUsage returns the time until a target utilization is reached
	TimeUntilUsage(time.Time, time.Duration, float64) time.Duration
}

// Peers tracks peers
type Peers interface {
	// Connected adds a connected peer
	Connected(nodeID ids.NodeID, nodeVersion *version.Application)

	// Disconnected removes a disconnected peer
	Disconnected(nodeID ids.NodeID)

	// Peers returns the set of connected peers
	Peers() set.Set[ids.NodeID]

	// ConnectedSubnets returns the subnets that connected peers are tracking
	ConnectedSubnets() set.Set[ids.ID]
}

// StartupTracker tracks startup progress
type StartupTracker interface {
	// ShouldStart returns true if startup should begin
	ShouldStart() bool

	// StartingTime returns when startup began
	StartingTime() (time.Time, bool)

	// Started marks startup as started
	Started() bool

	// OnBootstrapStarted marks bootstrap as started
	OnBootstrapStarted()

	// OnBootstrapFinished marks bootstrap as finished
	OnBootstrapFinished()
}

// peers implements Peers
type peers struct {
	lock       sync.RWMutex
	connected  set.Set[ids.NodeID]
	versions   map[ids.NodeID]*version.Application
	mySubnets  set.Set[ids.ID]
	peerSubnets map[ids.NodeID]set.Set[ids.ID]
}

// NewPeers returns a new Peers
func NewPeers() Peers {
	return &peers{
		connected:   set.NewSet[ids.NodeID](16),
		versions:    make(map[ids.NodeID]*version.Application),
		peerSubnets: make(map[ids.NodeID]set.Set[ids.ID]),
	}
}

func (p *peers) Connected(nodeID ids.NodeID, nodeVersion *version.Application) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.connected.Add(nodeID)
	p.versions[nodeID] = nodeVersion
}

func (p *peers) Disconnected(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.connected.Remove(nodeID)
	delete(p.versions, nodeID)
	delete(p.peerSubnets, nodeID)
}

func (p *peers) Peers() set.Set[ids.NodeID] {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.connected
}

func (p *peers) ConnectedSubnets() set.Set[ids.ID] {
	p.lock.RLock()
	defer p.lock.RUnlock()

	subnets := set.NewSet[ids.ID](len(p.mySubnets))
	subnets.Union(p.mySubnets)
	
	for _, peerSubnets := range p.peerSubnets {
		subnets.Union(peerSubnets)
	}
	
	return subnets
}

// ResourceTracker tracks resource usage
type ResourceTracker interface {
	// StartProcessing marks that a node has started processing
	StartProcessing(nodeID ids.NodeID, time time.Time)
	
	// StopProcessing marks that a node has stopped processing
	StopProcessing(nodeID ids.NodeID, time time.Time)
	
	// CPUTracker returns the CPU tracker for a node
	CPUTracker(ids.NodeID) CPUTracker
	
	// DiskTracker returns the disk tracker for a node  
	DiskTracker(ids.NodeID) CPUTracker
}

// NewResourceTracker creates a new resource tracker
func NewResourceTracker(registry interface{}, usage interface{}, factory interface{}, interval time.Duration) (ResourceTracker, error) {
	return &resourceTracker{
		cpuTrackers:  make(map[ids.NodeID]CPUTracker),
		diskTrackers: make(map[ids.NodeID]CPUTracker),
	}, nil
}

type resourceTracker struct {
	mu           sync.RWMutex
	cpuTrackers  map[ids.NodeID]CPUTracker
	diskTrackers map[ids.NodeID]CPUTracker
}

func (rt *resourceTracker) StartProcessing(nodeID ids.NodeID, time time.Time) {
	// No-op for now
}

func (rt *resourceTracker) StopProcessing(nodeID ids.NodeID, time time.Time) {
	// No-op for now
}

func (rt *resourceTracker) CPUTracker(nodeID ids.NodeID) CPUTracker {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	
	if tracker, ok := rt.cpuTrackers[nodeID]; ok {
		return tracker
	}
	return &noOpCPUTracker{}
}

func (rt *resourceTracker) DiskTracker(nodeID ids.NodeID) CPUTracker {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	
	if tracker, ok := rt.diskTrackers[nodeID]; ok {
		return tracker
	}
	return &noOpCPUTracker{}
}

type noOpCPUTracker struct{}

func (*noOpCPUTracker) UtilizeTime(time.Time, time.Time) {}
func (*noOpCPUTracker) Utilization(time.Time, time.Duration) float64 { return 0 }
func (*noOpCPUTracker) Len() int { return 0 }
func (*noOpCPUTracker) TimeUntilUsage(time.Time, time.Duration, float64) time.Duration { return 0 }