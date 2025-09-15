// Package consensustest provides test utilities for consensus operations
package consensustest

import (
	"sync"
	"testing"

	"github.com/luxfi/consensus/context"
	"github.com/luxfi/ids"
)

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

// TestLock is a simple lock implementation for tests
type TestLock struct {
	sync.RWMutex
}

// Lock acquires the lock
func (l *TestLock) Lock() {
	l.RWMutex.Lock()
}

// Unlock releases the lock
func (l *TestLock) Unlock() {
	l.RWMutex.Unlock()
}

// Context creates a test consensus context
func Context(t *testing.T, chainID ids.ID) *context.Context {
	t.Helper()
	return &context.Context{
		QuantumID:   1,
		NetID:       ids.Empty,
		ChainID:     chainID,
		NodeID:      ids.GenerateTestNodeID(),
		XChainID:    ids.GenerateTestID(),
		CChainID:    ids.GenerateTestID(),
		AVAXAssetID: ids.GenerateTestID(),
	}
}
