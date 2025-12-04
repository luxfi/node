// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/luxfi/database"
	"github.com/luxfi/geth/common"
)

var (
	ErrMigrationCancelled = errors.New("migration cancelled")
	ErrInvalidCheckpoint  = errors.New("invalid checkpoint")
)

// MigrationStats contains statistics about a migration
type MigrationStats struct {
	KeysMigrated       uint64
	BlocksMigrated     uint64
	BytesMigrated      uint64
	LastProcessedBlock uint64
	Errors             []error
}

// ProgressCallback is called during migration to report progress
type ProgressCallback func(current, total uint64)

// StateMigrator handles migrating state between databases
type StateMigrator struct {
	srcPath  string
	dstPath  string
	srcDB    database.Database
	dstDB    database.Database

	namespace        []byte
	progressCallback ProgressCallback
	startFrom        uint64
	stopAfter        uint64

	mu sync.Mutex
}

// NewStateMigrator creates a new state migrator
func NewStateMigrator(srcPath, dstPath string) *StateMigrator {
	return &StateMigrator{
		srcPath: srcPath,
		dstPath: dstPath,
	}
}

// NewStateMigratorWithDatabases creates a state migrator with injected databases for testing
func NewStateMigratorWithDatabases(srcDB, dstDB database.Database) *StateMigrator {
	return &StateMigrator{
		srcDB: srcDB,
		dstDB: dstDB,
	}
}

// SetNamespace sets the namespace to strip from source keys
func (m *StateMigrator) SetNamespace(namespace []byte) {
	m.namespace = namespace
}

// SetProgressCallback sets the progress callback
func (m *StateMigrator) SetProgressCallback(cb ProgressCallback) {
	m.progressCallback = cb
}

// SetStartFrom sets the block number to start migration from
func (m *StateMigrator) SetStartFrom(blockNum uint64) {
	m.startFrom = blockNum
}

// SetStopAfter sets the number of blocks to migrate before stopping
func (m *StateMigrator) SetStopAfter(count uint64) {
	m.stopAfter = count
}

// Migrate performs the state migration
func (m *StateMigrator) Migrate(ctx context.Context) (*MigrationStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Open databases
	if m.srcDB == nil {
		m.srcDB = OpenTestDatabase(m.srcPath)
	}
	if m.dstDB == nil {
		m.dstDB = CreateTestDatabase(m.dstPath)
	}

	stats := &MigrationStats{}
	processedBlocks := make(map[uint64]bool)

	iter := m.srcDB.NewIterator()
	defer iter.Release()

	var keyCount uint64
	batch := m.dstDB.NewBatch()
	batchSize := 0

	for iter.Next() {
		select {
		case <-ctx.Done():
			return stats, ErrMigrationCancelled
		default:
		}

		key := iter.Key()
		val := iter.Value()

		// Strip namespace if present
		newKey := m.stripNamespace(key)

		// Write to destination
		if err := batch.Put(newKey, val); err != nil {
			stats.Errors = append(stats.Errors, err)
			continue
		}

		keyCount++
		stats.KeysMigrated++
		stats.BytesMigrated += uint64(len(key) + len(val))
		batchSize++

		// Track blocks
		if isHeader, num, _ := ParseHeaderKey(newKey); isHeader && !processedBlocks[num] {
			if num >= m.startFrom {
				stats.BlocksMigrated++
				stats.LastProcessedBlock = num
				processedBlocks[num] = true

				if m.stopAfter > 0 && stats.BlocksMigrated >= m.stopAfter {
					break
				}
			}
		}

		// Commit batch periodically
		if batchSize >= 1000 {
			if err := batch.Write(); err != nil {
				return stats, err
			}
			batch.Reset()
			batchSize = 0

			if m.progressCallback != nil {
				m.progressCallback(stats.KeysMigrated, keyCount)
			}
		}
	}

	// Final batch commit
	if batchSize > 0 {
		if err := batch.Write(); err != nil {
			return stats, err
		}
	}

	// Final progress callback
	if m.progressCallback != nil {
		m.progressCallback(stats.KeysMigrated, stats.KeysMigrated)
	}

	return stats, nil
}

// stripNamespace removes the namespace prefix from a key
func (m *StateMigrator) stripNamespace(key []byte) []byte {
	if len(m.namespace) == 0 {
		return key
	}

	if len(key) >= len(m.namespace) && hasPrefix(key, m.namespace) {
		return key[len(m.namespace):]
	}
	return key
}

// Close closes the migrator and its databases
func (m *StateMigrator) Close() error {
	var errs []error
	if m.srcDB != nil {
		if err := m.srcDB.Close(); err != nil {
			errs = append(errs, err)
		}
		m.srcDB = nil
	}
	if m.dstDB != nil {
		if err := m.dstDB.Close(); err != nil {
			errs = append(errs, err)
		}
		m.dstDB = nil
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// hasPrefix checks if data starts with prefix
func hasPrefix(data, prefix []byte) bool {
	if len(data) < len(prefix) {
		return false
	}
	for i := range prefix {
		if data[i] != prefix[i] {
			return false
		}
	}
	return true
}

// HeaderKey constructs a header key from block number and hash
func HeaderKey(number uint64, hash common.Hash) []byte {
	key := make([]byte, 41)
	key[0] = 'h'
	binary.BigEndian.PutUint64(key[1:9], number)
	copy(key[9:41], hash[:])
	return key
}

// CanonicalKey constructs a canonical hash key from block number
func CanonicalKey(number uint64) []byte {
	key := make([]byte, 10)
	key[0] = 'h'
	binary.BigEndian.PutUint64(key[1:9], number)
	key[9] = 'n'
	return key
}

// BodyKey constructs a block body key from block number and hash
func BodyKey(number uint64, hash common.Hash) []byte {
	key := make([]byte, 41)
	key[0] = 'b'
	binary.BigEndian.PutUint64(key[1:9], number)
	copy(key[9:41], hash[:])
	return key
}

// ParseHeaderKey parses a header key and returns block number and hash
func ParseHeaderKey(key []byte) (bool, uint64, common.Hash) {
	if len(key) != 41 || key[0] != 'h' {
		return false, 0, common.Hash{}
	}
	num := binary.BigEndian.Uint64(key[1:9])
	hash := common.BytesToHash(key[9:41])
	return true, num, hash
}

// IsHeaderKey checks if a key is a header key
func IsHeaderKey(key []byte) bool {
	isHeader, _, _ := ParseHeaderKey(key)
	return isHeader
}
