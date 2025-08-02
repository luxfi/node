// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package wave

import (
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/set"
)

// StaticWaveThreshold implements fixed alpha thresholding
// (Previously StaticQuorum in Avalanche)
//
// Uses a fixed threshold for determining when constructive
// interference (consensus) has been achieved.
type StaticWaveThreshold struct {
	mu         sync.RWMutex
	alpha      int                    // Fixed threshold
	votes      map[ids.ID]set.Set[ids.NodeID] // Votes per choice
	totalVotes int
}

// NewStaticWaveThreshold creates a threshold with fixed alpha
func NewStaticWaveThreshold(alpha int) *StaticWaveThreshold {
	return &StaticWaveThreshold{
		alpha: alpha,
		votes: make(map[ids.ID]set.Set[ids.NodeID]),
	}
}

// AddVote adds a vote and returns true if threshold is reached
func (s *StaticWaveThreshold) AddVote(nodeID ids.NodeID, choice ids.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize vote set if needed
	if _, exists := s.votes[choice]; !exists {
		s.votes[choice] = set.NewSet[ids.NodeID](s.alpha)
	}

	// Add vote if not already present
	votesSet := s.votes[choice]
	if !votesSet.Contains(nodeID) {
		votesSet.Add(nodeID)
		s.totalVotes++
	}

	// Check if we've reached threshold
	return s.votes[choice].Len() >= s.alpha
}

// GetThreshold returns the alpha threshold
func (s *StaticWaveThreshold) GetThreshold() int {
	return s.alpha
}

// GetVoteCount returns votes for a specific choice
func (s *StaticWaveThreshold) GetVoteCount(choice ids.ID) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if voters, exists := s.votes[choice]; exists {
		return voters.Len()
	}
	return 0
}

// GetTotalVotes returns total votes received
func (s *StaticWaveThreshold) GetTotalVotes() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalVotes
}

// GetLeader returns the current leading choice and its vote count
func (s *StaticWaveThreshold) GetLeader() (ids.ID, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var leader ids.ID
	maxVotes := 0

	for choice, voters := range s.votes {
		if voters.Len() > maxVotes {
			leader = choice
			maxVotes = voters.Len()
		}
	}

	return leader, maxVotes
}

// Reset clears all votes
func (s *StaticWaveThreshold) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.votes = make(map[ids.ID]set.Set[ids.NodeID])
	s.totalVotes = 0
}

// GetWavePattern returns the current voting state
func (s *StaticWaveThreshold) GetWavePattern() *WavePattern {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pattern := &WavePattern{
		Choices:    make(map[ids.ID]int),
		TotalVotes: s.totalVotes,
		Threshold:  s.alpha,
	}

	for choice, voters := range s.votes {
		pattern.Choices[choice] = voters.Len()
	}

	return pattern
}