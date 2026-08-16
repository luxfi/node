// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"fmt"
	"sync"

	"github.com/luxfi/database"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// ChainDBManager manages chain database access using a single global ZapDB.
// All chains share one ZapDB instance with prefix-based isolation:
// 1. Single database - easier to manage, backup, and query across chains
// 2. Prefix isolation - each chain's data is prefixed by its chainID
// 3. G-Chain compatible - dgraph can index the entire database for GraphQL queries
type ChainDBManager struct {
	mu sync.RWMutex

	// Global shared database (ZapDB)
	db database.Database

	// Cached prefixed databases per chain
	chainDBs map[ids.ID]database.Database

	log log.Logger
}

// ChainDBManagerConfig holds configuration for the chain database manager
type ChainDBManagerConfig struct {
	// DB is the global shared database (ZapDB)
	DB database.Database

	Log log.Logger
}

// NewChainDBManager creates a new chain database manager using a single global ZapDB
func NewChainDBManager(config ChainDBManagerConfig) *ChainDBManager {
	return &ChainDBManager{
		db:       config.DB,
		chainDBs: make(map[ids.ID]database.Database),
		log:      config.Log,
	}
}

// chainDB returns a prefixed database for the given chain.
// Uses prefix-based isolation on the single global ZapDB.
func (m *ChainDBManager) chainDB(chainID ids.ID, chainAlias string) (database.Database, error) {
	if m.db == nil {
		return nil, fmt.Errorf("global database not initialized")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check cache first
	if db, exists := m.chainDBs[chainID]; exists {
		return db, nil
	}

	// Create prefixed database for this chain
	chainDB := prefixdb.New(chainID[:], m.db)
	m.chainDBs[chainID] = chainDB

	if !m.log.IsZero() {
		m.log.Info("Created prefixed database for chain",
			log.Stringer("chainID", chainID),
			log.String("alias", chainAlias),
		)
	}

	return chainDB, nil
}

// GetVMDatabase returns a VM-prefixed database for the given chain.
// Adds a "vm" prefix within the chain's prefix for VM-specific data.
func (m *ChainDBManager) GetVMDatabase(chainID ids.ID, chainAlias string) (database.Database, error) {
	chainDB, err := m.chainDB(chainID, chainAlias)
	if err != nil {
		return nil, err
	}

	// Add VM prefix to isolate VM data from other chain data
	return prefixdb.New(VMDBPrefix, chainDB), nil
}
