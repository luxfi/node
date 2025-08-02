// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/quasar/choices"
	"github.com/luxfi/node/v2/quasar/params"
	"github.com/luxfi/node/v2/utils/set"
)

// Topological is the implementation of the Snowman/Chain consensus algorithm
type Topological struct {
	// parameters
	params params.Parameters
	
	// context
	ctx *quasar.Context
	
	// metrics 
	metrics *metrics
	
	// state tracking
	blocks          map[string]Block  // blockID -> Block
	processing      set.Set[ids.ID]   // blocks that are currently processing
	lastAcceptedID  string
	preferenceID    string
	finalized       bool
	
	// voting state
	pollNumber      uint64
	nodesByHeight   map[uint64]set.Set[ids.ID]  // height -> set of block IDs
	heightIndex     map[string]uint64           // blockID -> height
	
	// confidence tracking for Beta parameter
	blockConfidence map[string]int  // blockID -> consecutive polls as preferred
}

// Initialize the consensus engine
func (ts *Topological) Initialize(ctx context.Context, parameters Parameters, lastAcceptedID string, lastAcceptedHeight uint64, lastAcceptedTime uint64) error {
	// Extract consensus context from context
	consensusCtx, ok := ctx.Value("consensus").(*quasar.Context)
	if !ok {
		consensusCtx = &quasar.Context{
			ChainID: ids.GenerateTestID(),
		}
	}
	
	ts.ctx = consensusCtx
	ts.params = params.Parameters{
		K:                     parameters.K,
		AlphaPreference:       parameters.AlphaPreference,
		AlphaConfidence:       parameters.AlphaConfidence,
		Beta:                  parameters.Beta,
		ConcurrentRepolls:     parameters.ConcurrentRepolls,
		OptimalProcessing:     parameters.OptimalProcessing,
		MaxOutstandingItems:   parameters.MaxOutstandingItems,
		MaxItemProcessingTime: time.Duration(parameters.MaxItemProcessingTime),
	}
	ts.blocks = make(map[string]Block)
	ts.processing = set.NewSet[ids.ID](parameters.OptimalProcessing)
	ts.lastAcceptedID = lastAcceptedID
	ts.preferenceID = lastAcceptedID
	ts.finalized = true
	ts.nodesByHeight = make(map[uint64]set.Set[ids.ID])
	ts.heightIndex = make(map[string]uint64)
	ts.blockConfidence = make(map[string]int)
	
	namespace := fmt.Sprintf("consensus_%s_chain", ts.ctx.ChainID)
	if ts.ctx.Registerer != nil {
		promRegisterer := quasar.NewMetricsRegistererWrapper(ts.ctx.Registerer)
		ts.metrics = newMetrics(namespace, promRegisterer)
	}
	
	return nil
}

// Parameters returns the parameters of this consensus instance
func (ts *Topological) Parameters() Parameters {
	return Parameters{
		K:                     ts.params.K,
		AlphaPreference:       ts.params.AlphaPreference,
		AlphaConfidence:       ts.params.AlphaConfidence,
		Beta:                  ts.params.Beta,
		ConcurrentRepolls:     ts.params.ConcurrentRepolls,
		OptimalProcessing:     ts.params.OptimalProcessing,
		MaxOutstandingItems:   ts.params.MaxOutstandingItems,
		MaxItemProcessingTime: int64(ts.params.MaxItemProcessingTime),
	}
}

// Add a block to consensus
func (ts *Topological) Add(ctx context.Context, blk Block) error {
	blkID := blk.ID()
	
	// Check if already added
	if _, exists := ts.blocks[blkID]; exists {
		return nil
	}
	
	// Verify the block
	if err := blk.Verify(ctx); err != nil {
		return err
	}
	
	// Add to tracking
	ts.blocks[blkID] = blk
	
	// Check parent exists
	parentID := blk.Parent()
	if parentID != ids.Empty && parentID.String() != ts.lastAcceptedID {
		if _, exists := ts.blocks[parentID.String()]; !exists {
			// Parent doesn't exist, can't process yet
			return nil
		}
	}
	
	// Add to processing
	id, _ := ids.FromString(blkID)
	ts.processing.Add(id)
	
	// Track by height
	height := blk.Height()
	ts.heightIndex[blkID] = height
	if heightSet, exists := ts.nodesByHeight[height]; exists {
		heightSet.Add(id)
	} else {
		newSet := set.NewSet[ids.ID](10)
		newSet.Add(id)
		ts.nodesByHeight[height] = newSet
	}
	
	// Update preference if this extends the preferred chain
	if blk.Parent().String() == ts.preferenceID {
		ts.preferenceID = blkID
	}
	
	if ts.metrics != nil {
		ts.metrics.numProcessing.Set(float64(ts.processing.Len()))
	}
	return nil
}

// Issued returns whether the block has been issued into consensus
func (ts *Topological) Issued(blk Block) bool {
	_, exists := ts.blocks[blk.ID()]
	return exists
}

// Processing returns whether the block ID is currently processing
func (ts *Topological) Processing(blkID ids.ID) bool {
	return ts.processing.Contains(blkID)
}

// Decided returns whether the block has been decided
func (ts *Topological) Decided(blk Block) bool {
	return blk.Status() == choices.Accepted || blk.Status() == choices.Rejected
}

// IsPreferred returns whether the block is on the preferred chain
func (ts *Topological) IsPreferred(blk Block) bool {
	// Walk up from preference to see if we hit this block
	currentID := ts.preferenceID
	for currentID != "" && currentID != ts.lastAcceptedID {
		if currentID == blk.ID() {
			return true
		}
		
		current, exists := ts.blocks[currentID]
		if !exists {
			break
		}
		currentID = current.Parent().String()
	}
	
	return blk.ID() == ts.lastAcceptedID
}

// Preference returns the ID of the preferred block
func (ts *Topological) Preference() ids.ID {
	id, _ := ids.FromString(ts.preferenceID)
	return id
}

// RecordPoll records the results of a network poll
func (ts *Topological) RecordPoll(ctx context.Context, votes []ids.ID) error {
	ts.pollNumber++

	// Count votes for each block and propagate to ancestors
	voteCounts := make(map[ids.ID]int)
	for _, vote := range votes {
		// Add vote to the block itself
		voteCounts[vote]++
		
		// Propagate vote to ancestors
		if voteStr := vote.String(); voteStr != "" {
			currentID := voteStr
			for {
				if blk, exists := ts.blocks[currentID]; exists {
					parentID := blk.Parent()
					if parentID == ids.Empty || parentID.String() == ts.lastAcceptedID {
						break
					}
					// Add vote to parent
					voteCounts[parentID]++
					currentID = parentID.String()
				} else {
					break
				}
			}
		}
	}

	// Find the most voted block at each height
	heightPreferences := make(map[uint64]ids.ID)
	maxVotesPerHeight := make(map[uint64]int)
	
	for id, count := range voteCounts {
		if height, exists := ts.heightIndex[id.String()]; exists {
			if count >= ts.params.AlphaPreference && count > maxVotesPerHeight[height] {
				maxVotesPerHeight[height] = count
				heightPreferences[height] = id
			}
		}
	}

	// Update preference based on voting
	oldPreference := ts.preferenceID
	newPreference := ts.calculatePreference(heightPreferences)
	
	if newPreference != oldPreference {
		// Preference changed, reset confidence for all blocks
		for id := range ts.blockConfidence {
			ts.blockConfidence[id] = 0
		}
		ts.preferenceID = newPreference
	}

	// Update confidence for blocks on the preferred chain
	ts.updateConfidence()

	// Process blocks that have reached Beta confidence
	ts.processConfidentBlocks(ctx)

	return nil
}

// calculatePreference determines the new preference based on votes
func (ts *Topological) calculatePreference(heightPreferences map[uint64]ids.ID) string {
	// Start from current preference
	currentPref := ts.preferenceID
	
	// Find the highest height with votes
	var maxHeight uint64
	for height := range heightPreferences {
		if height > maxHeight {
			maxHeight = height
		}
	}
	
	// Walk down from highest height to find the best chain
	for height := maxHeight; height > 0; height-- {
		if prefID, exists := heightPreferences[height]; exists {
			// Check if this block extends from last accepted or current preference
			if ts.isDescendantOf(prefID.String(), ts.lastAcceptedID) {
				return prefID.String()
			}
		}
	}
	
	return currentPref
}

// isDescendantOf checks if blockID is a descendant of ancestorID
func (ts *Topological) isDescendantOf(blockID, ancestorID string) bool {
	currentID := blockID
	for currentID != "" && currentID != ts.lastAcceptedID {
		if currentID == ancestorID {
			return true
		}
		
		current, exists := ts.blocks[currentID]
		if !exists {
			return false
		}
		currentID = current.Parent().String()
	}
	
	return ancestorID == ts.lastAcceptedID
}

// updateConfidence increments confidence for blocks on the preferred chain
func (ts *Topological) updateConfidence() {
	// Walk from preference back to last accepted, incrementing confidence
	currentID := ts.preferenceID
	for currentID != "" && currentID != ts.lastAcceptedID {
		ts.blockConfidence[currentID]++
		
		current, exists := ts.blocks[currentID]
		if !exists {
			break
		}
		currentID = current.Parent().String()
	}
}

// processConfidentBlocks accepts blocks that have reached Beta confidence
func (ts *Topological) processConfidentBlocks(ctx context.Context) {
	// Process from last accepted forward
	blocksToAccept := []string{}
	currentID := ts.preferenceID
	
	// Collect blocks with sufficient confidence
	for currentID != "" && currentID != ts.lastAcceptedID {
		if ts.blockConfidence[currentID] >= ts.params.Beta {
			blocksToAccept = append(blocksToAccept, currentID)
		}
		
		current, exists := ts.blocks[currentID]
		if !exists {
			break
		}
		currentID = current.Parent().String()
	}
	
	// Accept blocks from oldest to newest
	for i := len(blocksToAccept) - 1; i >= 0; i-- {
		if blk, exists := ts.blocks[blocksToAccept[i]]; exists {
			ts.accept(ctx, blk)
		}
	}
}

func (ts *Topological) accept(ctx context.Context, blk Block) {
	// First, accept all ancestors that aren't already accepted
	ancestors := []Block{}
	currentBlk := blk
	for currentBlk != nil && currentBlk.Status() == choices.Processing {
		ancestors = append([]Block{currentBlk}, ancestors...)
		parentID := currentBlk.Parent()
		if parentID == ids.Empty || parentID.String() == ts.lastAcceptedID {
			break
		}
		if parent, exists := ts.blocks[parentID.String()]; exists {
			currentBlk = parent
		} else {
			break
		}
	}
	
	// Accept ancestors from oldest to newest
	for _, ancestor := range ancestors {
		ancestorID := ancestor.ID()
		
		// Accept this block
		if err := ancestor.Accept(); err != nil {
			if ts.metrics != nil {
				ts.metrics.numFailedAccept.Inc()
			}
			continue
		}
		
		// Remove from processing
		id, _ := ids.FromString(ancestorID)
		ts.processing.Remove(id)
		
		// Update last accepted
		ts.lastAcceptedID = ancestorID
		
		// Clear confidence tracking for accepted block
		delete(ts.blockConfidence, ancestorID)
		
		// Reject conflicting blocks at the same height
		height := ancestor.Height()
		if heightSet, exists := ts.nodesByHeight[height]; exists {
			for _, conflictID := range heightSet.List() {
				if conflictID.String() != ancestorID {
					if conflictBlk, exists := ts.blocks[conflictID.String()]; exists {
						ts.reject(ctx, conflictBlk)
					}
				}
			}
		}
		
		if ts.metrics != nil {
			ts.metrics.numAccepted.Inc()
		}
	}
	
	if ts.metrics != nil {
		ts.metrics.numProcessing.Set(float64(ts.processing.Len()))
	}
	
	// Check if finalized
	ts.finalized = ts.processing.Len() == 0
}

func (ts *Topological) reject(ctx context.Context, blk Block) {
	rejectErr := blk.Reject()
	if rejectErr != nil {
		if ts.metrics != nil {
			ts.metrics.numFailedReject.Inc()
		}
		// Continue processing even if reject fails
		// The block stays in processing state but we still need to reject children
	} else {
		// Only remove from processing if reject succeeded
		id, _ := ids.FromString(blk.ID())
		ts.processing.Remove(id)
		
		if ts.metrics != nil {
			ts.metrics.numRejected.Inc()
		}
	}
	
	// Clear confidence tracking regardless of reject success
	delete(ts.blockConfidence, blk.ID())
	
	// Recursively reject children even if parent reject failed
	for _, candidateID := range ts.processing.List() {
		if candidate, exists := ts.blocks[candidateID.String()]; exists {
			if candidate.Parent().String() == blk.ID() {
				ts.reject(ctx, candidate)
			}
		}
	}
	
	if ts.metrics != nil {
		ts.metrics.numProcessing.Set(float64(ts.processing.Len()))
	}
}

// Finalized returns whether consensus has finalized
func (ts *Topological) Finalized() bool {
	return ts.finalized
}

// HealthCheck returns health status of consensus
func (ts *Topological) HealthCheck(ctx context.Context) (interface{}, error) {
	isHealthy := ts.Finalized() || ts.processing.Len() <= ts.params.OptimalProcessing
	details := map[string]interface{}{
		"processing": ts.processing.Len(),
		"preference": ts.preferenceID,
		"finalized":  ts.finalized,
		"healthy":    isHealthy,
	}
	return details, nil
}

// NumProcessing returns the number of currently processing blocks
func (ts *Topological) NumProcessing() int {
	return ts.processing.Len()
}

// metrics tracks consensus metrics
type metrics struct {
	numProcessing   prometheus.Gauge
	numAccepted     prometheus.Counter
	numRejected     prometheus.Counter
	numFailedAccept prometheus.Counter
	numFailedReject prometheus.Counter
}

func newMetrics(namespace string, registerer prometheus.Registerer) *metrics {
	m := &metrics{
		numProcessing: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "processing",
			Help:      "Number of currently processing blocks",
		}),
		numAccepted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "accepted",
			Help:      "Number of accepted blocks",
		}),
		numRejected: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "rejected",
			Help:      "Number of rejected blocks",
		}),
		numFailedAccept: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "failed_accept",
			Help:      "Number of blocks that failed to accept",
		}),
		numFailedReject: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "failed_reject",
			Help:      "Number of blocks that failed to reject",
		}),
	}
	
	// Try to register metrics, but ignore AlreadyRegisteredError
	err := registerer.Register(m.numProcessing)
	if err != nil && !isAlreadyRegisteredError(err) {
		// If it's not an already registered error, use MustRegister to panic
		registerer.MustRegister(m.numProcessing)
	}
	
	_ = registerer.Register(m.numAccepted)
	_ = registerer.Register(m.numRejected)
	_ = registerer.Register(m.numFailedAccept)
	_ = registerer.Register(m.numFailedReject)
	
	return m
}

func isAlreadyRegisteredError(err error) bool {
	return err != nil && err.Error() == "duplicate metrics collector registration attempted"
}