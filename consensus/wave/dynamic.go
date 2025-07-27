// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package wave

import (
	"sync"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/set"
)

// DynamicWaveThreshold implements decoupled alpha thresholding
// (Previously DynamicQuorum/DecoupledQuorum in Avalanche)
//
// Uses separate thresholds for preference (alpha1) and confidence (alpha2),
// allowing for more nuanced consensus detection through wave interference.
type DynamicWaveThreshold struct {
	mu              sync.RWMutex
	alphaPreference int                             // Alpha1: threshold for preference
	alphaConfidence int                             // Alpha2: threshold for confidence
	votes           map[ids.ID]set.Set[ids.NodeID]  // Votes per choice
	totalVotes      int
	preferenceMode  bool                            // Whether we're in preference or confidence mode
}

// NewDynamicWaveThreshold creates a threshold with decoupled alphas
func NewDynamicWaveThreshold(alphaPreference, alphaConfidence int) *DynamicWaveThreshold {
	return &DynamicWaveThreshold{
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
		votes:           make(map[ids.ID]set.Set[ids.NodeID]),
		preferenceMode:  true, // Start in preference mode
	}
}

// AddVote adds a vote and returns true if current threshold is reached
func (d *DynamicWaveThreshold) AddVote(nodeID ids.NodeID, choice ids.ID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Initialize vote set if needed
	if _, exists := d.votes[choice]; !exists {
		d.votes[choice] = set.NewSet[ids.NodeID](d.alphaConfidence)
	}

	// Add vote if not already present
	if d.votes[choice].Add(nodeID) {
		d.totalVotes++
	}

	// Check against appropriate threshold
	threshold := d.alphaPreference
	if !d.preferenceMode {
		threshold = d.alphaConfidence
	}

	return d.votes[choice].Len() >= threshold
}

// GetThreshold returns the current active threshold
func (d *DynamicWaveThreshold) GetThreshold() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.preferenceMode {
		return d.alphaPreference
	}
	return d.alphaConfidence
}

// SetMode switches between preference and confidence modes
func (d *DynamicWaveThreshold) SetMode(preferenceMode bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.preferenceMode = preferenceMode
}

// GetVoteCount returns votes for a specific choice
func (d *DynamicWaveThreshold) GetVoteCount(choice ids.ID) int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if voters, exists := d.votes[choice]; exists {
		return voters.Len()
	}
	return 0
}

// GetTotalVotes returns total votes received
func (d *DynamicWaveThreshold) GetTotalVotes() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.totalVotes
}

// GetLeader returns the current leading choice and its vote count
func (d *DynamicWaveThreshold) GetLeader() (ids.ID, int) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var leader ids.ID
	maxVotes := 0

	for choice, voters := range d.votes {
		if voters.Len() > maxVotes {
			leader = choice
			maxVotes = voters.Len()
		}
	}

	return leader, maxVotes
}

// Reset clears all votes
func (d *DynamicWaveThreshold) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.votes = make(map[ids.ID]set.Set[ids.NodeID])
	d.totalVotes = 0
	d.preferenceMode = true
}

// CheckPreference returns true if any choice meets preference threshold
func (d *DynamicWaveThreshold) CheckPreference() (ids.ID, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for choice, voters := range d.votes {
		if voters.Len() >= d.alphaPreference {
			return choice, true
		}
	}
	return ids.Empty, false
}

// CheckConfidence returns true if any choice meets confidence threshold
func (d *DynamicWaveThreshold) CheckConfidence() (ids.ID, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for choice, voters := range d.votes {
		if voters.Len() >= d.alphaConfidence {
			return choice, true
		}
	}
	return ids.Empty, false
}