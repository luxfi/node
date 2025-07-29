// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/set"
)

// Set manages a set of polls
type Set struct {
	mu    sync.RWMutex
	polls map[uint32]Poll
	log   log.Logger
}

// Poll represents a consensus poll
type Poll interface {
	// ID returns the poll ID
	ID() uint32

	// Vote registers a vote
	Vote(nodeID ids.NodeID, vote Vote) error

	// Drop removes a node from the poll
	Drop(nodeID ids.NodeID) error

	// Finished returns true if the poll is finished
	Finished() bool

	// Result returns the poll result if finished
	Result() (Result, error)

	// String returns a string representation
	String() string
}

// Vote represents a vote in a poll
type Vote struct {
	PreferredID ids.ID
	AcceptedID  ids.ID
}

// Result represents the result of a poll
type Result struct {
	PreferredID     ids.ID
	AcceptedID      ids.ID
	PreferenceCount int
	AcceptanceCount int
}

// NewSet creates a new poll set
func NewSet() *Set {
	return &Set{
		polls: make(map[uint32]Poll),
		log:   log.NewNoOpLogger(),
	}
}

// Add adds a new poll
func (s *Set) Add(requestID uint32, poll Poll) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.polls[requestID]; exists {
		return fmt.Errorf("poll %d already exists", requestID)
	}

	s.polls[requestID] = poll
	return nil
}

// Vote registers a vote
func (s *Set) Vote(requestID uint32, nodeID ids.NodeID, vote Vote) error {
	s.mu.RLock()
	poll, exists := s.polls[requestID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("poll %d not found", requestID)
	}

	return poll.Vote(nodeID, vote)
}

// Drop removes a node from a poll
func (s *Set) Drop(requestID uint32, nodeID ids.NodeID) error {
	s.mu.RLock()
	poll, exists := s.polls[requestID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("poll %d not found", requestID)
	}

	return poll.Drop(nodeID)
}

// Remove removes a finished poll
func (s *Set) Remove(requestID uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.polls[requestID]; !exists {
		return fmt.Errorf("poll %d not found", requestID)
	}

	delete(s.polls, requestID)
	return nil
}

// Get returns a poll by ID
func (s *Set) Get(requestID uint32) (Poll, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	poll, exists := s.polls[requestID]
	return poll, exists
}

// Len returns the number of active polls
func (s *Set) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.polls)
}

// simplePoll is a basic poll implementation
type simplePoll struct {
	id        uint32
	voters    set.Set[ids.NodeID]
	votes     map[ids.NodeID]Vote
	finished  bool
	startTime time.Time
}

// NewPoll creates a new poll
func NewPoll(id uint32, voters set.Set[ids.NodeID]) Poll {
	return &simplePoll{
		id:        id,
		voters:    voters,
		votes:     make(map[ids.NodeID]Vote),
		startTime: time.Now(),
	}
}

// ID returns the poll ID
func (p *simplePoll) ID() uint32 {
	return p.id
}

// Vote registers a vote
func (p *simplePoll) Vote(nodeID ids.NodeID, vote Vote) error {
	if p.finished {
		return fmt.Errorf("poll %d already finished", p.id)
	}

	if !p.voters.Contains(nodeID) {
		return fmt.Errorf("node %s not in voter set", nodeID)
	}

	p.votes[nodeID] = vote

	// Check if poll is finished
	if len(p.votes) >= p.voters.Len() {
		p.finished = true
	}

	return nil
}

// Drop removes a node from the poll
func (p *simplePoll) Drop(nodeID ids.NodeID) error {
	p.voters.Remove(nodeID)
	delete(p.votes, nodeID)

	// Check if poll is finished
	if len(p.votes) >= p.voters.Len() {
		p.finished = true
	}

	return nil
}

// Finished returns true if the poll is finished
func (p *simplePoll) Finished() bool {
	return p.finished
}

// Result returns the poll result
func (p *simplePoll) Result() (Result, error) {
	if !p.finished {
		return Result{}, fmt.Errorf("poll %d not finished", p.id)
	}

	// Count votes
	preferenceCount := make(map[ids.ID]int)
	acceptanceCount := make(map[ids.ID]int)

	for _, vote := range p.votes {
		preferenceCount[vote.PreferredID]++
		acceptanceCount[vote.AcceptedID]++
	}

	// Find most preferred
	var preferredID ids.ID
	maxPreference := 0
	for id, count := range preferenceCount {
		if count > maxPreference {
			preferredID = id
			maxPreference = count
		}
	}

	// Find most accepted
	var acceptedID ids.ID
	maxAcceptance := 0
	for id, count := range acceptanceCount {
		if count > maxAcceptance {
			acceptedID = id
			maxAcceptance = count
		}
	}

	return Result{
		PreferredID:     preferredID,
		AcceptedID:      acceptedID,
		PreferenceCount: maxPreference,
		AcceptanceCount: maxAcceptance,
	}, nil
}

// String returns a string representation
func (p *simplePoll) String() string {
	return fmt.Sprintf("Poll{id=%d, votes=%d/%d, finished=%v}",
		p.id, len(p.votes), p.voters.Len(), p.finished)
}