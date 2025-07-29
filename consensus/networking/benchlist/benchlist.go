// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/set"
)

// Benchlist tracks validator reliability.
type Benchlist interface {
	// RegisterResponse registers that we received a response from the given validator.
	RegisterResponse(nodeID ids.NodeID)

	// RegisterFailure registers that the given validator failed to respond.
	RegisterFailure(nodeID ids.NodeID)

	// IsBenched returns true if the given validator is benched.
	IsBenched(nodeID ids.NodeID) bool

	// GetBenched returns all benched validators.
	GetBenched() []ids.NodeID

	// Remove removes a validator from the benchlist.
	Remove(nodeID ids.NodeID)
}

// Config defines benchlist configuration.
type Config struct {
	// Threshold is the number of consecutive failures before benching.
	Threshold int

	// Duration is how long a validator is benched.
	Duration time.Duration

	// MaxPortion is the maximum portion of validators that can be benched.
	MaxPortion float64
}

// benchlist implements Benchlist.
type benchlist struct {
	mu         sync.RWMutex
	config     Config
	failures   map[ids.NodeID]int
	benchTimes map[ids.NodeID]time.Time
	benched    set.Set[ids.NodeID]
}

// NewBenchlist creates a new benchlist.
func NewBenchlist(config Config) Benchlist {
	return &benchlist{
		config:     config,
		failures:   make(map[ids.NodeID]int),
		benchTimes: make(map[ids.NodeID]time.Time),
		benched:    set.Set[ids.NodeID]{},
	}
}

// RegisterResponse implements Benchlist.
func (b *benchlist) RegisterResponse(nodeID ids.NodeID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.failures, nodeID)
}

// RegisterFailure implements Benchlist.
func (b *benchlist) RegisterFailure(nodeID ids.NodeID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures[nodeID]++
	if b.failures[nodeID] >= b.config.Threshold {
		b.benchTimes[nodeID] = time.Now().Add(b.config.Duration)
		b.benched.Add(nodeID)
		delete(b.failures, nodeID)
	}
}

// IsBenched implements Benchlist.
func (b *benchlist) IsBenched(nodeID ids.NodeID) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	benchTime, exists := b.benchTimes[nodeID]
	if !exists {
		return false
	}

	if time.Now().After(benchTime) {
		// Bench time expired
		b.mu.RUnlock()
		b.mu.Lock()
		delete(b.benchTimes, nodeID)
		b.benched.Remove(nodeID)
		b.mu.Unlock()
		b.mu.RLock()
		return false
	}

	return true
}

// GetBenched implements Benchlist.
func (b *benchlist) GetBenched() []ids.NodeID {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.benched.List()
}

// Remove implements Benchlist.
func (b *benchlist) Remove(nodeID ids.NodeID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.failures, nodeID)
	delete(b.benchTimes, nodeID)
	b.benched.Remove(nodeID)
}

// Manager manages benchlists for multiple chains.
type Manager interface {
	// GetBenchlist returns the benchlist for the given chain.
	GetBenchlist(chainID ids.ID) Benchlist

	// RegisterChain registers a new chain with the manager.
	RegisterChain(chainID ids.ID, config Config) Benchlist
}

// manager implements Manager.
type manager struct {
	mu         sync.RWMutex
	benchlists map[ids.ID]Benchlist
}

// NewManager creates a new benchlist manager.
func NewManager() Manager {
	return &manager{
		benchlists: make(map[ids.ID]Benchlist),
	}
}

// GetBenchlist implements Manager.
func (m *manager) GetBenchlist(chainID ids.ID) Benchlist {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.benchlists[chainID]
}

// RegisterChain implements Manager.
func (m *manager) RegisterChain(chainID ids.ID, config Config) Benchlist {
	m.mu.Lock()
	defer m.mu.Unlock()

	bl := NewBenchlist(config)
	m.benchlists[chainID] = bl
	return bl
}