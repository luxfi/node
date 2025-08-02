// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package nova

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/set"
)

// BroadcastFinalizer implements network-wide finality broadcasting
// (Previously part of Avalanche's finalization mechanism)
//
// The broadcast finalizer creates "nova explosions" - when a vertex
// achieves local finality, it broadcasts this achievement across the
// network, creating a cascade of finality confirmations.
type BroadcastFinalizer struct {
	mu sync.RWMutex

	// Vertex tracking
	vertices    map[ids.ID]*vertexState
	frontier    set.Set[ids.ID] // Current nova frontier
	
	// Network state
	nodeID      ids.NodeID
	validators  set.Set[ids.NodeID]
	quorumSize  int
	
	// Metrics
	metrics     NovaMetrics
	
	// Configuration
	params      Parameters
}

// vertexState tracks the finality state of a vertex
type vertexState struct {
	id              ids.ID
	dependencies    []ids.ID
	localFinality   bool
	networkFinality bool
	novaTime        time.Time
	witnesses       set.Set[ids.NodeID]
	registeredTime  time.Time
}

// Parameters configures the broadcast finalizer
type Parameters struct {
	QuorumThreshold   float64       // Fraction of validators needed for nova
	BroadcastTimeout  time.Duration // Timeout for broadcast confirmations
	RetryInterval     time.Duration // Interval between broadcast retries
	MaxRetries        int           // Maximum broadcast retries
}

// NewBroadcastFinalizer creates a new broadcast finalizer
func NewBroadcastFinalizer(nodeID ids.NodeID, validators set.Set[ids.NodeID], params Parameters) *BroadcastFinalizer {
	quorumSize := int(float64(validators.Len()) * params.QuorumThreshold)
	if quorumSize < 1 {
		quorumSize = 1
	}

	return &BroadcastFinalizer{
		vertices:   make(map[ids.ID]*vertexState),
		frontier:   set.NewSet[ids.ID](10),
		nodeID:     nodeID,
		validators: validators,
		quorumSize: quorumSize,
		params:     params,
	}
}

// RegisterVertex registers a vertex for finality tracking
func (b *BroadcastFinalizer) RegisterVertex(ctx context.Context, vertexID ids.ID, dependencies []ids.ID) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.vertices[vertexID]; exists {
		return fmt.Errorf("vertex %s already registered", vertexID)
	}

	b.vertices[vertexID] = &vertexState{
		id:             vertexID,
		dependencies:   dependencies,
		witnesses:      set.NewSet[ids.NodeID](b.quorumSize),
		registeredTime: time.Now(),
	}

	return nil
}

// RecordFinalization records local finalization of a vertex
func (b *BroadcastFinalizer) RecordFinalization(ctx context.Context, vertexID ids.ID) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, exists := b.vertices[vertexID]
	if !exists {
		return fmt.Errorf("vertex %s not registered", vertexID)
	}

	if state.localFinality {
		return nil // Already finalized locally
	}

	// Check dependencies
	for _, dep := range state.dependencies {
		if depState, exists := b.vertices[dep]; !exists || !depState.localFinality {
			return fmt.Errorf("dependency %s not finalized", dep)
		}
	}

	state.localFinality = true
	state.witnesses.Add(b.nodeID) // Self witness
	b.metrics.LocalFinalizations++

	// Check if we already have network finality
	if state.witnesses.Len() >= b.quorumSize {
		b.achieveNova(state)
	}

	return nil
}

// BroadcastFinality broadcasts finality to network peers
func (b *BroadcastFinalizer) BroadcastFinality(ctx context.Context, vertexID ids.ID) error {
	b.mu.RLock()
	state, exists := b.vertices[vertexID]
	if !exists || !state.localFinality {
		b.mu.RUnlock()
		return fmt.Errorf("vertex %s not locally finalized", vertexID)
	}
	b.mu.RUnlock()

	// In production, this would send network messages
	// For now, we simulate broadcast
	b.metrics.BroadcastsSent++
	
	// Simulate some validators receiving the broadcast
	go b.simulateBroadcastReception(ctx, vertexID)
	
	return nil
}

// simulateBroadcastReception simulates network broadcast reception
func (b *BroadcastFinalizer) simulateBroadcastReception(ctx context.Context, vertexID ids.ID) {
	// In production, this would be replaced by actual network messages
	validators := b.validators.List()
	
	// Simulate 80% of validators receiving the broadcast
	for i, validator := range validators {
		if i > len(validators)*8/10 {
			break
		}
		
		// Small delay to simulate network propagation
		time.Sleep(time.Millisecond * 10)
		
		if err := b.ReceiveFinality(ctx, vertexID, validator); err != nil {
			// Log error in production
			continue
		}
	}
}

// ReceiveFinality processes finality announcement from peer
func (b *BroadcastFinalizer) ReceiveFinality(ctx context.Context, vertexID ids.ID, nodeID ids.NodeID) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, exists := b.vertices[vertexID]
	if !exists {
		// Create state if we haven't seen this vertex
		state = &vertexState{
			id:        vertexID,
			witnesses: set.NewSet[ids.NodeID](b.quorumSize),
		}
		b.vertices[vertexID] = state
	}

	// Add witness
	if !state.witnesses.Contains(nodeID) {
		state.witnesses.Add(nodeID)
		b.metrics.BroadcastsRecvd++
	}

	// Check if we've achieved nova
	if state.localFinality && state.witnesses.Len() >= b.quorumSize && !state.networkFinality {
		b.achieveNova(state)
	}

	return nil
}

// achieveNova marks a vertex as having achieved network-wide finality
func (b *BroadcastFinalizer) achieveNova(state *vertexState) {
	state.networkFinality = true
	state.novaTime = time.Now()
	b.frontier.Add(state.id)
	b.metrics.NovaEvents++
	
	// Update average nova time
	if state.localFinality {
		novaDelay := state.novaTime.Sub(state.registeredTime)
		if b.metrics.NovaEvents == 1 {
			b.metrics.AverageNovaTime = novaDelay
		} else {
			// Running average
			b.metrics.AverageNovaTime = (b.metrics.AverageNovaTime + novaDelay) / 2
		}
	}
}

// GetFinalityStatus returns finality status of a vertex
func (b *BroadcastFinalizer) GetFinalityStatus(vertexID ids.ID) FinalityStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()

	state, exists := b.vertices[vertexID]
	if !exists {
		return FinalityStatus{
			VertexID: vertexID,
		}
	}

	return FinalityStatus{
		VertexID:        state.id,
		LocalFinality:   state.localFinality,
		NetworkFinality: state.networkFinality,
		NovaTime:        state.novaTime,
		Confirmations:   state.witnesses.Len(),
		Dependencies:    state.dependencies,
		Witnesses:       copySet(state.witnesses),
	}
}

// GetNovaFrontier returns the current nova frontier
func (b *BroadcastFinalizer) GetNovaFrontier() set.Set[ids.ID] {
	b.mu.RLock()
	defer b.mu.RUnlock()
	
	return copyIDSet(b.frontier)
}

// HealthCheck returns finalizer health
func (b *BroadcastFinalizer) HealthCheck(ctx context.Context) (Health, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	pendingCount := 0
	var lastNova time.Time
	
	for _, state := range b.vertices {
		if state.localFinality && !state.networkFinality {
			pendingCount++
		}
		if state.networkFinality && state.novaTime.After(lastNova) {
			lastNova = state.novaTime
		}
	}

	// Calculate nova rate
	novaRate := 0.0
	if b.metrics.NovaEvents > 0 && !lastNova.IsZero() {
		elapsed := time.Since(lastNova).Seconds()
		if elapsed > 0 {
			novaRate = float64(b.metrics.NovaEvents) / elapsed
		}
	}

	// Calculate network coherence
	coherence := 0.0
	if b.metrics.LocalFinalizations > 0 {
		coherence = float64(b.metrics.NovaEvents) / float64(b.metrics.LocalFinalizations)
	}

	health := Health{
		Healthy:          pendingCount < 100 && coherence > 0.8,
		NovaRate:         novaRate,
		PendingVertices:  pendingCount,
		LastNovaTime:     lastNova,
		NetworkCoherence: coherence,
	}

	return health, nil
}

// PruneFrontier removes old vertices from the frontier
func (b *BroadcastFinalizer) PruneFrontier(before time.Time) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	pruned := 0
	for vertexID := range b.frontier {
		if state, exists := b.vertices[vertexID]; exists {
			if state.novaTime.Before(before) {
				b.frontier.Remove(vertexID)
				delete(b.vertices, vertexID)
				pruned++
			}
		}
	}

	return pruned
}

// copySet creates a copy of a set.Set[ids.NodeID]
func copySet(s set.Set[ids.NodeID]) set.Set[ids.NodeID] {
	newSet := set.NewSet[ids.NodeID](s.Len())
	for _, item := range s.List() {
		newSet.Add(item)
	}
	return newSet
}

// copyIDSet creates a copy of a set.Set[ids.ID]
func copyIDSet(s set.Set[ids.ID]) set.Set[ids.ID] {
	newSet := set.NewSet[ids.ID](s.Len())
	for _, item := range s.List() {
		newSet.Add(item)
	}
	return newSet
}