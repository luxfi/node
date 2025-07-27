// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package flare

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/utils/set"

	"github.com/luxfi/node/consensus/focus"
	"github.com/luxfi/node/consensus/photon"
	"github.com/luxfi/node/consensus/wave"
)

// Engine implements the flare DAG consensus engine
// (Previously Avalanche DAG engine)
type Engine struct {
	mu sync.RWMutex

	// Photonic consensus components
	sampler    photon.Sampler
	threshold  wave.Threshold
	confidence map[ids.ID]focus.Confidence // Per conflict set

	// Vertex tracking
	vertices       map[ids.ID]Vertex
	children       map[ids.ID]set.Set[ids.ID]
	conflictSets   map[ids.ID]*ConflictSet
	vertexToCS     map[ids.ID]ids.ID // Vertex ID to conflict set ID
	
	// Consensus state
	preferred      set.Set[ids.ID]
	virtuous       set.Set[ids.ID]
	processing     set.Set[ids.ID]
	
	// Metrics
	metrics FlareMetrics
	
	// Configuration
	params Parameters
}

// Parameters configures the flare engine
type Parameters struct {
	K               int           // Photon sample size
	AlphaPreference int           // Wave threshold for preference
	AlphaConfidence int           // Wave threshold for confidence
	Beta            int           // Focus rounds for finality
	MaxPollTime     time.Duration // Maximum time for a poll
}

// NewEngine creates a new flare consensus engine
func NewEngine(params Parameters) *Engine {
	return &Engine{
		params:       params,
		sampler:      photon.NewFactory(params.K).NewBinary(),
		threshold:    wave.NewFactory(params.AlphaPreference, params.AlphaConfidence).NewDynamic(),
		confidence:   make(map[ids.ID]focus.Confidence),
		vertices:     make(map[ids.ID]Vertex),
		children:     make(map[ids.ID]set.Set[ids.ID]),
		conflictSets: make(map[ids.ID]*ConflictSet),
		vertexToCS:   make(map[ids.ID]ids.ID),
		preferred:    set.NewSet[ids.ID](10),
		virtuous:     set.NewSet[ids.ID](10),
		processing:   set.NewSet[ids.ID](10),
	}
}

// Add a new vertex to the DAG
func (e *Engine) Add(ctx context.Context, vertex Vertex) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	vertexID := vertex.ID()
	
	// Check if already added
	if _, exists := e.vertices[vertexID]; exists {
		return fmt.Errorf("vertex %s already exists", vertexID)
	}

	// Verify vertex
	if err := vertex.Verify(ctx); err != nil {
		return fmt.Errorf("vertex verification failed: %w", err)
	}

	// Add to tracking structures
	e.vertices[vertexID] = vertex
	e.processing.Add(vertexID)

	// Track parent-child relationships
	for _, parentID := range vertex.Parents() {
		if _, exists := e.children[parentID]; !exists {
			e.children[parentID] = set.NewSet[ids.ID](2)
		}
		e.children[parentID].Add(vertexID)
	}

	// Detect conflicts and assign to conflict sets
	conflictSetID := e.detectConflicts(vertex)
	if flareVertex, ok := vertex.(*FlareVertex); ok {
		flareVertex.SetConflictSet(conflictSetID)
	}
	e.vertexToCS[vertexID] = conflictSetID

	// Initialize as virtuous if no conflicts
	if conflictSetID == ids.Empty {
		e.virtuous.Add(vertexID)
		e.preferred.Add(vertexID)
	}

	e.metrics.VerticesAdded++
	return nil
}

// detectConflicts determines which conflict set a vertex belongs to
func (e *Engine) detectConflicts(vertex Vertex) ids.ID {
	// In a real implementation, this would analyze transaction conflicts
	// For now, we'll use a simple heuristic based on vertex data
	
	// Check if any transactions conflict with existing vertices
	for _, tx := range vertex.Txs() {
		for existingID, existing := range e.vertices {
			if existingID == vertex.ID() {
				continue
			}
			
			for _, existingTx := range existing.Txs() {
				if tx == existingTx {
					// Found conflict - check if existing vertex has conflict set
					if csID, exists := e.vertexToCS[existingID]; exists && csID != ids.Empty {
						// Add to existing conflict set
						e.conflictSets[csID].Vertices.Add(vertex.ID())
						return csID
					}
					
					// Create new conflict set
					csID := ids.GenerateTestID()
					e.conflictSets[csID] = &ConflictSet{
						ID:       csID,
						Vertices: set.NewSet[ids.ID](2),
					}
					e.conflictSets[csID].Vertices.Add(vertex.ID())
					e.conflictSets[csID].Vertices.Add(existingID)
					
					// Update existing vertex's conflict set
					e.vertexToCS[existingID] = csID
					e.virtuous.Remove(existingID)
					
					e.metrics.ConflictSetsFormed++
					return csID
				}
			}
		}
	}
	
	return ids.Empty // No conflicts
}

// Vote for vertex preferences
func (e *Engine) Vote(ctx context.Context, vertexID ids.ID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	vertex, exists := e.vertices[vertexID]
	if !exists {
		return fmt.Errorf("vertex %s not found", vertexID)
	}

	// Update flare score
	if flareVertex, ok := vertex.(*FlareVertex); ok {
		flareVertex.IncrementPhotons()
		flareVertex.UpdateFlareScore(flareVertex.FlareScore() + 1)
	}

	return nil
}

// RecordPoll records the result of photon sampling
func (e *Engine) RecordPoll(ctx context.Context, votes map[ids.ID]int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()
	defer func() {
		e.metrics.PollsProcessed++
		e.metrics.AveragePollTime = time.Since(start)
	}()

	// Group votes by conflict set
	csVotes := make(map[ids.ID]map[ids.ID]int)
	
	for vertexID, voteCount := range votes {
		csID := e.vertexToCS[vertexID]
		if csID == ids.Empty {
			csID = vertexID // Virtuous vertices are their own "conflict set"
		}
		
		if _, exists := csVotes[csID]; !exists {
			csVotes[csID] = make(map[ids.ID]int)
		}
		csVotes[csID][vertexID] = voteCount
	}

	// Process each conflict set
	for csID, vertexVotes := range csVotes {
		// Get or create confidence tracker for this conflict set
		if _, exists := e.confidence[csID]; !exists {
			e.confidence[csID] = focus.NewFactory(e.params.Beta).NewBinary()
		}
		
		// Reset threshold for this conflict set
		threshold := wave.NewFactory(e.params.AlphaPreference, e.params.AlphaConfidence).NewDynamic()
		
		// Add votes
		for vertexID, count := range vertexVotes {
			for i := 0; i < count; i++ {
				threshold.AddVote(ids.GenerateTestNodeID(), vertexID)
			}
		}
		
		// Get leader and check threshold
		leader, voteCount := threshold.GetLeader()
		thresholdReached := voteCount >= threshold.GetThreshold()
		
		// Update confidence
		e.confidence[csID].RecordPoll(thresholdReached, leader)
		
		// Update preferences
		if thresholdReached && leader != ids.Empty {
			if csID == leader {
				// Virtuous vertex
				e.preferred.Add(leader)
			} else {
				// Conflicting vertex - update conflict set leader
				if cs, exists := e.conflictSets[csID]; exists {
					cs.Leader = leader
					// Update preferred set
					for v := range cs.Vertices {
						if v == leader {
							e.preferred.Add(v)
						} else {
							e.preferred.Remove(v)
						}
					}
				}
			}
		}
		
		// Check for finalization
		if e.confidence[csID].IsFocused() {
			choice := e.confidence[csID].GetChoice()
			if err := e.finalizeVertex(ctx, choice, csID); err != nil {
				return fmt.Errorf("failed to finalize vertex: %w", err)
			}
		}
	}

	return nil
}

// finalizeVertex marks a vertex as finalized and rejects conflicts
func (e *Engine) finalizeVertex(ctx context.Context, vertexID ids.ID, csID ids.ID) error {
	vertex, exists := e.vertices[vertexID]
	if !exists || vertex.Status() != choices.Processing {
		return nil
	}

	// Accept the vertex
	if err := vertex.Accept(ctx); err != nil {
		return err
	}
	
	e.processing.Remove(vertexID)
	e.metrics.VerticesFinalized++

	// If part of conflict set, reject others
	if cs, exists := e.conflictSets[csID]; exists && csID != vertexID {
		for conflictID := range cs.Vertices {
			if conflictID != vertexID {
				if conflict, exists := e.vertices[conflictID]; exists {
					conflict.Reject(ctx)
					e.processing.Remove(conflictID)
					e.preferred.Remove(conflictID)
					e.virtuous.Remove(conflictID)
				}
			}
		}
		e.metrics.ConflictsResolved++
	}

	return nil
}

// Preferred returns current preferred vertices
func (e *Engine) Preferred() set.Set[ids.ID] {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	// Return copy to avoid external modification
	return e.preferred.Copy()
}

// Virtuous returns virtuous vertices (no conflicts)
func (e *Engine) Virtuous() set.Set[ids.ID] {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	return e.virtuous.Copy()
}

// Conflicts returns conflicting vertex sets
func (e *Engine) Conflicts(vertexID ids.ID) set.Set[ids.ID] {
	e.mu.RLock()
	defer e.mu.RUnlock()

	csID := e.vertexToCS[vertexID]
	if csID == ids.Empty || csID == vertexID {
		return set.NewSet[ids.ID](0) // No conflicts
	}

	if cs, exists := e.conflictSets[csID]; exists {
		conflicts := cs.Vertices.Copy()
		conflicts.Remove(vertexID) // Don't include self
		return conflicts
	}

	return set.NewSet[ids.ID](0)
}

// IsVirtuous checks if a vertex is virtuous
func (e *Engine) IsVirtuous(vertexID ids.ID) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	return e.virtuous.Contains(vertexID)
}

// HealthCheck returns DAG health metrics
func (e *Engine) HealthCheck(ctx context.Context) (Health, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	totalVertices := len(e.vertices)
	
	health := Health{
		Healthy:           totalVertices > 0 && e.preferred.Len() > 0,
		FlareCoherence:    e.calculateCoherence(),
		ConflictRatio:     e.calculateConflictRatio(),
		VirtuousRatio:     float64(e.virtuous.Len()) / float64(totalVertices),
		LastPollTime:      time.Now(), // Should track actual
		OutstandingVertex: e.processing.Len(),
		ConflictSets:      len(e.conflictSets),
	}

	return health, nil
}

// calculateCoherence calculates DAG coherence
func (e *Engine) calculateCoherence() float64 {
	if len(e.vertices) == 0 {
		return 1.0
	}
	
	// Coherence based on ratio of preferred to total
	return float64(e.preferred.Len()) / float64(len(e.vertices))
}

// calculateConflictRatio calculates conflict ratio
func (e *Engine) calculateConflictRatio() float64 {
	if len(e.vertices) == 0 {
		return 0
	}
	
	conflicting := 0
	for _, csID := range e.vertexToCS {
		if csID != ids.Empty {
			conflicting++
		}
	}
	
	return float64(conflicting) / float64(len(e.vertices))
}