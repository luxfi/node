// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package attestationvm

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/logging"
)

const (
	// Database prefixes for oracle registry
	oraclePrefix     = 0x10
	oracleCountKey   = "oracle_count"
	oracleIndexKey   = "oracle_index"
)

// OracleRegistry manages registered oracles
type OracleRegistry struct {
	db     database.Database
	log    logging.Logger
	
	// Caches
	oracles      map[string]*OracleInfo  // ID -> OracleInfo
	oraclesByFeed map[string][]string     // Feed type -> Oracle IDs
	oracleCount  int
	
	mu sync.RWMutex
}

// NewOracleRegistry creates a new oracle registry
func NewOracleRegistry(db database.Database, log logging.Logger) (*OracleRegistry, error) {
	or := &OracleRegistry{
		db:            db,
		log:           log,
		oracles:       make(map[string]*OracleInfo),
		oraclesByFeed: make(map[string][]string),
	}
	
	// Load oracle count
	countBytes, err := db.Get([]byte(oracleCountKey))
	if err == database.ErrNotFound {
		or.oracleCount = 0
	} else if err != nil {
		return nil, err
	} else {
		or.oracleCount = int(binary.BigEndian.Uint64(countBytes))
	}
	
	// Load oracles from DB
	if err := or.loadOracles(); err != nil {
		return nil, err
	}
	
	return or, nil
}

// RegisterOracle registers a new oracle
func (or *OracleRegistry) RegisterOracle(oracle *OracleInfo) error {
	or.mu.Lock()
	defer or.mu.Unlock()
	
	// Validate oracle
	if oracle.ID == "" {
		return errors.New("oracle ID required")
	}
	
	if len(oracle.PublicKey) == 0 {
		return errors.New("oracle public key required")
	}
	
	// Check if already exists
	if _, exists := or.oracles[oracle.ID]; exists {
		return errors.New("oracle already registered")
	}
	
	// Set registration time
	if oracle.JoinedAt == 0 {
		oracle.JoinedAt = time.Now().Unix()
	}
	
	// Serialize oracle
	oracleBytes, err := utils.Codec.Marshal(codecVersion, oracle)
	if err != nil {
		return err
	}
	
	// Store in database
	key := make([]byte, 1+len(oracle.ID))
	key[0] = oraclePrefix
	copy(key[1:], []byte(oracle.ID))
	
	if err := or.db.Put(key, oracleBytes); err != nil {
		return err
	}
	
	// Update caches
	or.oracles[oracle.ID] = oracle
	
	// Update feed index
	for _, feed := range oracle.Feeds {
		or.oraclesByFeed[feed] = append(or.oraclesByFeed[feed], oracle.ID)
	}
	
	// Update count
	or.oracleCount++
	countBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(countBytes, uint64(or.oracleCount))
	if err := or.db.Put([]byte(oracleCountKey), countBytes); err != nil {
		return err
	}
	
	or.log.Info("Oracle registered",
		"id", oracle.ID,
		"name", oracle.Name,
		"feeds", oracle.Feeds,
	)
	
	return nil
}

// GetOracle retrieves an oracle by ID
func (or *OracleRegistry) GetOracle(oracleID string) (*OracleInfo, error) {
	or.mu.RLock()
	defer or.mu.RUnlock()
	
	oracle, exists := or.oracles[oracleID]
	if !exists {
		return nil, errors.New("oracle not found")
	}
	
	return oracle, nil
}

// GetOraclesByFeed returns oracles that provide a specific feed type
func (or *OracleRegistry) GetOraclesByFeed(feedType string) ([]*OracleInfo, error) {
	or.mu.RLock()
	defer or.mu.RUnlock()
	
	oracleIDs, exists := or.oraclesByFeed[feedType]
	if !exists {
		return nil, nil
	}
	
	oracles := make([]*OracleInfo, 0, len(oracleIDs))
	for _, id := range oracleIDs {
		if oracle, exists := or.oracles[id]; exists {
			oracles = append(oracles, oracle)
		}
	}
	
	return oracles, nil
}

// GetAllOracles returns all registered oracles
func (or *OracleRegistry) GetAllOracles() []*OracleInfo {
	or.mu.RLock()
	defer or.mu.RUnlock()
	
	oracles := make([]*OracleInfo, 0, len(or.oracles))
	for _, oracle := range or.oracles {
		oracles = append(oracles, oracle)
	}
	
	return oracles
}

// UpdateOracleReputation updates an oracle's reputation score
func (or *OracleRegistry) UpdateOracleReputation(oracleID string, delta int64) error {
	or.mu.Lock()
	defer or.mu.Unlock()
	
	oracle, exists := or.oracles[oracleID]
	if !exists {
		return errors.New("oracle not found")
	}
	
	// Update reputation (with bounds checking)
	newRep := int64(oracle.Reputation) + delta
	if newRep < 0 {
		newRep = 0
	}
	oracle.Reputation = uint64(newRep)
	
	// Serialize and save
	oracleBytes, err := utils.Codec.Marshal(codecVersion, oracle)
	if err != nil {
		return err
	}
	
	key := make([]byte, 1+len(oracle.ID))
	key[0] = oraclePrefix
	copy(key[1:], []byte(oracle.ID))
	
	if err := or.db.Put(key, oracleBytes); err != nil {
		return err
	}
	
	or.log.Debug("Oracle reputation updated",
		"id", oracleID,
		"reputation", oracle.Reputation,
		"delta", delta,
	)
	
	return nil
}

// RemoveOracle removes an oracle from the registry
func (or *OracleRegistry) RemoveOracle(oracleID string) error {
	or.mu.Lock()
	defer or.mu.Unlock()
	
	oracle, exists := or.oracles[oracleID]
	if !exists {
		return errors.New("oracle not found")
	}
	
	// Remove from database
	key := make([]byte, 1+len(oracleID))
	key[0] = oraclePrefix
	copy(key[1:], []byte(oracleID))
	
	if err := or.db.Delete(key); err != nil {
		return err
	}
	
	// Remove from caches
	delete(or.oracles, oracleID)
	
	// Remove from feed index
	for _, feed := range oracle.Feeds {
		feedOracles := or.oraclesByFeed[feed]
		for i, id := range feedOracles {
			if id == oracleID {
				or.oraclesByFeed[feed] = append(feedOracles[:i], feedOracles[i+1:]...)
				break
			}
		}
	}
	
	// Update count
	or.oracleCount--
	countBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(countBytes, uint64(or.oracleCount))
	if err := or.db.Put([]byte(oracleCountKey), countBytes); err != nil {
		return err
	}
	
	or.log.Info("Oracle removed",
		"id", oracleID,
		"name", oracle.Name,
	)
	
	return nil
}

// GetOracleCount returns the total number of registered oracles
func (or *OracleRegistry) GetOracleCount() int {
	or.mu.RLock()
	defer or.mu.RUnlock()
	return or.oracleCount
}

// loadOracles loads oracles from DB to cache
func (or *OracleRegistry) loadOracles() error {
	// In production, we would iterate through the DB
	// For now, we start with empty registry
	return nil
}

// Close closes the oracle registry
func (or *OracleRegistry) Close() {
	or.mu.Lock()
	defer or.mu.Unlock()
	
	or.oracles = nil
	or.oraclesByFeed = nil
}