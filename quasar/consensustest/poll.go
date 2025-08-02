// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensustest

import (
	"fmt"

	"github.com/luxfi/node/v2/utils/set"
)

// Poll defines the interface for a poll.
type Poll interface {
	Vote(vdr string, vote string)
	Finished() bool
	Result() []string
}

// TestPoll is a test poll implementation.
type TestPoll struct {
	vdrs    set.Set[string]
	alpha   int
	result  []string
	success bool
}

// NewPoll creates a new test poll.
func NewPoll(alpha int) *TestPoll {
	return &TestPoll{
		vdrs:  set.Set[string]{},
		alpha: alpha,
	}
}

// Vote adds a vote to the poll.
func (p *TestPoll) Vote(vdr string, vote string) {
	if p.vdrs.Contains(vdr) {
		return
	}
	p.vdrs.Add(vdr)
	p.result = append(p.result, vote)
	
	// Check if we have enough votes
	if len(p.result) >= p.alpha {
		p.success = true
	}
}

// Finished returns true if the poll has finished.
func (p *TestPoll) Finished() bool {
	return p.success
}

// Result returns the poll results.
func (p *TestPoll) Result() []string {
	return p.result
}

// String returns a string representation of the poll.
func (p *TestPoll) String() string {
	return fmt.Sprintf("Poll(alpha=%d, votes=%d, success=%v)", p.alpha, len(p.result), p.success)
}

// PollFactory creates test polls.
type PollFactory struct {
	alpha int
}

// NewPollFactory creates a new poll factory.
func NewPollFactory(alpha int) *PollFactory {
	return &PollFactory{
		alpha: alpha,
	}
}

// New creates a new poll.
func (f *PollFactory) New() Poll {
	return NewPoll(f.alpha)
}

// MockPoll is a mock poll for testing.
type MockPoll struct {
	votes    []string
	finished bool
	result   []string
}

// NewMockPoll creates a new mock poll.
func NewMockPoll() *MockPoll {
	return &MockPoll{
		votes: make([]string, 0),
	}
}

// SetFinished sets the poll as finished.
func (p *MockPoll) SetFinished(finished bool) {
	p.finished = finished
}

// SetResult sets the poll result.
func (p *MockPoll) SetResult(result []string) {
	p.result = result
}

// Vote adds a vote to the poll.
func (p *MockPoll) Vote(vdr string, vote string) {
	p.votes = append(p.votes, vote)
}

// Finished returns true if the poll has finished.
func (p *MockPoll) Finished() bool {
	return p.finished
}

// Result returns the poll results.
func (p *MockPoll) Result() []string {
	return p.result
}

// Votes returns all votes cast.
func (p *MockPoll) Votes() []string {
	return p.votes
}