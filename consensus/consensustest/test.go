// Package consensustest provides test utilities for consensus operations
package consensustest

import "testing"

// TestConsensus represents a test consensus instance
type TestConsensus struct {
	ID      string
	Running bool
}

// NewTestConsensus creates a new test consensus
func NewTestConsensus(id string) *TestConsensus {
	return &TestConsensus{
		ID:      id,
		Running: false,
	}
}

// Helper provides test helper functions
func Helper(t *testing.T) {
	t.Helper()
}
