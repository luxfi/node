// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/luxfi/database"
	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// ChainDBManager manages per-chain database instances.
// Each chain gets its own independent BadgerDB for:
// 1. Isolation - chains cannot interfere with each other
// 2. Performance - concurrent writes across chains
// 3. Maintainability - easier to backup/restore individual chains
type ChainDBManager struct {
	mu sync.RWMutex

	// Base directory for all chain databases
	baseDir string

	// Per-chain database instances
	// Key: chainID, Value: BadgerDB instance
	dbs map[ids.ID]database.Database

	// Fallback shared database for legacy/prefixed mode
	sharedDB database.Database

	// Whether to use isolated per-chain databases (true) or shared prefixed DB (false)
	isolated bool

	log log.Logger
}

// ChainDBManagerConfig holds configuration for the chain database manager
type ChainDBManagerConfig struct {
	// BaseDir is the base directory for chain databases
	BaseDir string

	// SharedDB is the shared database for fallback/legacy mode
	SharedDB database.Database

	// Isolated enables per-chain isolated databases
	// When false, uses prefixdb on SharedDB (legacy mode)
	Isolated bool

	Log log.Logger
}

// NewChainDBManager creates a new chain database manager
func NewChainDBManager(config ChainDBManagerConfig) *ChainDBManager {
	return &ChainDBManager{
		baseDir:  config.BaseDir,
		dbs:      make(map[ids.ID]database.Database),
		sharedDB: config.SharedDB,
		isolated: config.Isolated,
		log:      config.Log,
	}
}

// GetDatabase returns a database for the given chain.
// If isolated mode is enabled, creates/returns an independent BadgerDB.
// Otherwise, returns a prefixed database on the shared DB.
func (m *ChainDBManager) GetDatabase(chainID ids.ID, chainAlias string) (database.Database, error) {
	if !m.isolated {
		// Legacy mode: use prefixdb on shared database
		return m.getPrefixedDB(chainID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if database already exists
	if db, exists := m.dbs[chainID]; exists {
		return db, nil
	}

	// Create new isolated BadgerDB for this chain
	db, err := m.createChainDB(chainID, chainAlias)
	if err != nil {
		return nil, fmt.Errorf("failed to create database for chain %s: %w", chainID, err)
	}

	m.dbs[chainID] = db
	return db, nil
}

// getPrefixedDB returns a prefixed database on the shared DB (legacy mode)
func (m *ChainDBManager) getPrefixedDB(chainID ids.ID) (database.Database, error) {
	if m.sharedDB == nil {
		return nil, fmt.Errorf("shared database not initialized")
	}
	return prefixdb.New(chainID[:], m.sharedDB), nil
}

// createChainDB creates a new isolated BadgerDB for the given chain
func (m *ChainDBManager) createChainDB(chainID ids.ID, chainAlias string) (database.Database, error) {
	// Use chain alias for directory name if available, otherwise use chain ID
	dirName := chainAlias
	if dirName == "" {
		dirName = chainID.String()
	}

	dbPath := filepath.Join(m.baseDir, dirName)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dbPath, err)
	}

	// Create BadgerDB for this chain
	// badgerdb.New(file, configBytes, namespace, metrics)
	db, err := badgerdb.New(
		dbPath,
		nil, // Use default config
		chainAlias, // Use chain alias as namespace for metrics
		nil, // No metrics for now - will be wired up later
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open BadgerDB at %s: %w", dbPath, err)
	}

	m.log.Info("Created isolated database for chain",
		log.Stringer("chainID", chainID),
		log.String("alias", chainAlias),
		log.String("path", dbPath),
	)

	return db, nil
}

// GetVMDatabase returns a VM-prefixed database for the given chain.
// This adds a "vm" prefix to the chain's database for VM-specific data.
func (m *ChainDBManager) GetVMDatabase(chainID ids.ID, chainAlias string) (database.Database, error) {
	chainDB, err := m.GetDatabase(chainID, chainAlias)
	if err != nil {
		return nil, err
	}

	// Add VM prefix to isolate VM data from other chain data
	return prefixdb.New(VMDBPrefix, chainDB), nil
}

// Close closes all chain databases
func (m *ChainDBManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var closeErrors []error
	for chainID, db := range m.dbs {
		if err := db.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("failed to close database for chain %s: %w", chainID, err))
		}
	}

	if len(closeErrors) > 0 {
		return fmt.Errorf("errors closing chain databases: %v", closeErrors)
	}

	return nil
}

// GetAllDatabases returns all open chain databases.
// Useful for G-Chain (GraphQL layer) to query across all chains.
func (m *ChainDBManager) GetAllDatabases() map[ids.ID]database.Database {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[ids.ID]database.Database, len(m.dbs))
	for id, db := range m.dbs {
		result[id] = db
	}
	return result
}

// IsIsolated returns whether isolated per-chain databases are enabled
func (m *ChainDBManager) IsIsolated() bool {
	return m.isolated
}

// GetDatabasePath returns the path for a chain's database
func (m *ChainDBManager) GetDatabasePath(chainID ids.ID, chainAlias string) string {
	dirName := chainAlias
	if dirName == "" {
		dirName = chainID.String()
	}
	return filepath.Join(m.baseDir, dirName)
}
