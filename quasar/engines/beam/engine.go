// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
	"github.com/luxfi/node/utils/set"

	"github.com/luxfi/node/quasar/focus"
	"github.com/luxfi/node/quasar/photon"
	"github.com/luxfi/node/quasar/wave"
)

// Engine implements the beam linearizer for linear chain consensus
// (Previously Topological/Snowman++ in Avalanche)
type Engine struct {
	mu sync.RWMutex

	// Photonic consensus components
	sampler    photon.Sampler
	threshold  wave.Threshold
	confidence focus.Confidence

	// Block tracking
	blocks          map[ids.ID]Block
	blocksByHeight  map[uint64]set.Set[ids.ID]
	children        map[ids.ID]set.Set[ids.ID]
	
	// Consensus state
	preferredID     ids.ID
	preferredHeight uint64
	finalizedID     ids.ID
	finalizedHeight uint64
	
	// Metrics
	metrics BeamMetrics
	
	// Configuration
	params Parameters
}

// Parameters configures the beam engine
type Parameters struct {
	K               int           // Photon sample size
	AlphaPreference int           // Wave threshold for preference
	AlphaConfidence int           // Wave threshold for confidence
	Beta            int           // Focus rounds for finality
	MaxPollTime     time.Duration // Maximum time for a poll
}

// NewEngine creates a new beam consensus engine
func NewEngine(params Parameters) *Engine {
	return &Engine{
		params:         params,
		sampler:        photon.NewFactory(params.K).NewBinary(),
		threshold:      wave.NewFactory(params.AlphaPreference, params.AlphaConfidence).NewDynamic(),
		confidence:     focus.NewFactory(params.Beta).NewBinary(),
		blocks:         make(map[ids.ID]Block),
		blocksByHeight: make(map[uint64]set.Set[ids.ID]),
		children:       make(map[ids.ID]set.Set[ids.ID]),
	}
}

// Add a new block to the beam
func (e *Engine) Add(ctx context.Context, block Block) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	blockIDStr := block.ID()
	blockID, err := ids.FromString(blockIDStr)
	if err != nil {
		return fmt.Errorf("invalid block ID: %w", err)
	}
	
	// Check if already added
	if _, exists := e.blocks[blockID]; exists {
		return fmt.Errorf("block %s already exists", blockID)
	}

	// TODO: Verify block when method is available
	// if err := block.Verify(ctx); err != nil {
	// 	return fmt.Errorf("block verification failed: %w", err)
	// }

	// Add to tracking structures
	e.blocks[blockID] = block
	
	height := block.BeamHeight()
	if _, exists := e.blocksByHeight[height]; !exists {
		e.blocksByHeight[height] = set.NewSet[ids.ID](1)
	}
	heightSet := e.blocksByHeight[height]
	heightSet.Add(blockID)

	// Track parent-child relationships
	parentID := block.Parent()
	if parentID != ids.Empty {
		if _, exists := e.children[parentID]; !exists {
			e.children[parentID] = set.NewSet[ids.ID](1)
		}
		childrenSet := e.children[parentID]
		childrenSet.Add(blockID)
	}

	// Update preferred if this extends it
	if parentID == e.preferredID && height > e.preferredHeight {
		e.preferredID = blockID
		e.preferredHeight = height
	}

	e.metrics.BlocksAdded++
	return nil
}

// Vote for a block preference
func (e *Engine) Vote(ctx context.Context, blockID ids.ID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	block, exists := e.blocks[blockID]
	if !exists {
		return fmt.Errorf("block %s not found", blockID)
	}

	// Update beam score
	if beamBlock, ok := block.(*BeamBlock); ok {
		beamBlock.IncrementPhotons()
		beamBlock.UpdateBeamScore(beamBlock.BeamScore() + 1)
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

	// Reset wave threshold for new poll
	e.threshold.Reset()

	// Add all votes to wave threshold
	for blockID, voteCount := range votes {
		for i := 0; i < voteCount; i++ {
			e.threshold.AddVote(ids.GenerateTestNodeID(), blockID)
		}
	}

	// Get the leader
	leader, voteCount := e.threshold.GetLeader()
	
	// Check if we reached threshold
	thresholdReached := voteCount >= e.threshold.GetThreshold()
	
	// Record in confidence tracker
	e.confidence.RecordPoll(thresholdReached, leader)

	// Update preferred if threshold reached
	if thresholdReached && leader != ids.Empty {
		if block, exists := e.blocks[leader]; exists {
			e.preferredID = leader
			e.preferredHeight = block.BeamHeight()
		}
	}

	// Check for finalization
	if e.confidence.IsFocused() {
		choice := e.confidence.GetChoice()
		if block, exists := e.blocks[choice]; exists && block.Status() == choices.Processing {
			if err := e.finalizeBlock(ctx, block); err != nil {
				return fmt.Errorf("failed to finalize block: %w", err)
			}
		}
		// Reset confidence after finalization
		e.confidence.Reset()
	}

	return nil
}

// finalizeBlock marks a block and its ancestors as finalized
func (e *Engine) finalizeBlock(ctx context.Context, block Block) error {
	// Walk back and finalize all ancestors
	toFinalize := []Block{block}
	current := block
	
	for current.Parent() != ids.Empty && current.Parent() != e.finalizedID {
		parent, exists := e.blocks[current.Parent()]
		if !exists {
			break
		}
		if parent.Status() == choices.Processing {
			toFinalize = append(toFinalize, parent)
		}
		current = parent
	}

	// Finalize in order (oldest first)
	for i := len(toFinalize) - 1; i >= 0; i-- {
		b := toFinalize[i]
		if err := b.Accept(); err != nil {
			return err
		}
		blockID, _ := ids.FromString(b.ID())
		e.finalizedID = blockID
		e.finalizedHeight = b.BeamHeight()
		e.metrics.BlocksFinalized++
		
		// Reject conflicting blocks at same height
		if blocks, exists := e.blocksByHeight[b.BeamHeight()]; exists {
			for conflictID := range blocks {
				if conflictID != blockID {
					if conflict, exists := e.blocks[conflictID]; exists {
						conflict.Reject()
					}
				}
			}
		}
	}

	return nil
}

// Preferred returns the current preferred block
func (e *Engine) Preferred() ids.ID {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.preferredID
}

// Finalized returns finalized blocks since last call
func (e *Engine) Finalized() []ids.ID {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// In production, track finalized blocks since last call
	// For now, return current finalized
	if e.finalizedID != ids.Empty {
		return []ids.ID{e.finalizedID}
	}
	return nil
}

// IsFinalized checks if a block has been finalized
func (e *Engine) IsFinalized(blockID ids.ID) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	block, exists := e.blocks[blockID]
	if !exists {
		return false
	}
	return block.Status() == choices.Accepted
}

// HealthCheck returns consensus health metrics
func (e *Engine) HealthCheck(ctx context.Context) (Health, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	focusState := e.confidence.(*focus.BinaryFocusTracker).GetFocusState()
	
	health := Health{
		Healthy:           e.preferredID != ids.Empty,
		BeamCoherence:     e.calculateCoherence(),
		FocusStrength:     focusState.ConsecutivePolls,
		LastPollTime:      time.Now(), // Should track actual last poll
		ConsecutivePolls:  focusState.ConsecutivePolls,
		OutstandingBlocks: len(e.blocks) - int(e.metrics.BlocksFinalized),
	}

	return health, nil
}

// calculateCoherence calculates how coherent the beam is (0-1)
func (e *Engine) calculateCoherence() float64 {
	if e.preferredHeight == 0 {
		return 0
	}
	
	// Simple coherence: ratio of finalized to total height
	coherence := float64(e.finalizedHeight) / float64(e.preferredHeight)
	if coherence > 1 {
		coherence = 1
	}
	
	return coherence
}