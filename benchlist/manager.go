// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"sync"
	"time"

	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	metrics "github.com/luxfi/metric"
)

// NewManager creates a new benchlist manager
func NewManager(log log.Logger, reg metrics.Registerer, config *Config) Manager {
	return &manager{
		log:          log,
		benchedNodes: make(map[ids.ID]map[ids.NodeID]time.Time),
		validators:   make(map[ids.ID]validators.Manager),
		mu:           &sync.RWMutex{},
	}
}

type manager struct {
	log          log.Logger
	benchedNodes map[ids.ID]map[ids.NodeID]time.Time // chainID -> nodeID -> benchTime
	validators   map[ids.ID]validators.Manager
	mu           *sync.RWMutex
}

func (m *manager) IsBenched(nodeID ids.NodeID, chainID ids.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if benched, ok := m.benchedNodes[chainID]; ok {
		if benchTime, exists := benched[nodeID]; exists {
			// Check if bench period has expired (e.g., 24 hours)
			if time.Since(benchTime) < 24*time.Hour {
				return true
			}
			// Clean up expired bench
			delete(benched, nodeID)
		}
	}
	return false
}

func (m *manager) GetBenched(chainID ids.ID) []ids.NodeID {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var benched []ids.NodeID
	if benchedMap, ok := m.benchedNodes[chainID]; ok {
		for nodeID, benchTime := range benchedMap {
			if time.Since(benchTime) < 24*time.Hour {
				benched = append(benched, nodeID)
			}
		}
	}
	return benched
}

func (m *manager) RegisterChain(chainID ids.ID, vdrs validators.Manager) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.validators[chainID] = vdrs
	if m.benchedNodes[chainID] == nil {
		m.benchedNodes[chainID] = make(map[ids.NodeID]time.Time)
	}
	return nil
}

func (m *manager) Benchable(chainID ids.ID, nodeID ids.NodeID) Benchable {
	return &benchable{
		manager: m,
		chainID: chainID,
		nodeID:  nodeID,
	}
}

type benchable struct {
	manager *manager
	chainID ids.ID
	nodeID  ids.NodeID
}

func (b *benchable) Benched(chainID ids.ID, nodeID ids.NodeID) {
	b.manager.mu.Lock()
	defer b.manager.mu.Unlock()

	if b.manager.benchedNodes[chainID] == nil {
		b.manager.benchedNodes[chainID] = make(map[ids.NodeID]time.Time)
	}
	b.manager.benchedNodes[chainID][nodeID] = time.Now()
	b.manager.log.Info("node benched",
		log.Stringer("nodeID", nodeID),
		log.Stringer("chainID", chainID),
	)
}

func (b *benchable) Unbenched(chainID ids.ID, nodeID ids.NodeID) {
	b.manager.mu.Lock()
	defer b.manager.mu.Unlock()

	if benched, ok := b.manager.benchedNodes[chainID]; ok {
		delete(benched, nodeID)
		b.manager.log.Info("node unbenched",
			log.Stringer("nodeID", nodeID),
			log.Stringer("chainID", chainID),
		)
	}
}
