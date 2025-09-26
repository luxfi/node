// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package migration

import (
	"bytes"
	"fmt"
	"log"

	"github.com/cockroachdb/pebble"
	"github.com/dgraph-io/badger/v3"
)

// PebbleToBadgerMigrator handles migration from Pebble to Badger database
type PebbleToBadgerMigrator struct {
	sourcePath string
	targetPath string
	logger     *log.Logger
}

// NewPebbleToBadgerMigrator creates a new migrator instance
func NewPebbleToBadgerMigrator(sourcePath, targetPath string, logger *log.Logger) *PebbleToBadgerMigrator {
	return &PebbleToBadgerMigrator{
		sourcePath: sourcePath,
		targetPath: targetPath,
		logger:     logger,
	}
}

// Migrate performs the migration from Pebble to Badger
func (m *PebbleToBadgerMigrator) Migrate() error {
	m.logger.Printf("Starting migration from Pebble (%s) to Badger (%s)\n", m.sourcePath, m.targetPath)

	// Open Pebble database
	pebbleDB, err := pebble.Open(m.sourcePath, &pebble.Options{})
	if err != nil {
		return fmt.Errorf("failed to open Pebble database: %w", err)
	}
	defer pebbleDB.Close()

	// Open Badger database
	badgerOpts := badger.DefaultOptions(m.targetPath)
	badgerOpts.Logger = nil // Disable Badger logs for cleaner output
	badgerDB, err := badger.Open(badgerOpts)
	if err != nil {
		return fmt.Errorf("failed to open Badger database: %w", err)
	}
	defer badgerDB.Close()

	// Count total entries for progress tracking
	totalEntries := 0
	iter, _ := pebbleDB.NewIter(nil)
	for iter.First(); iter.Valid(); iter.Next() {
		totalEntries++
	}
	iter.Close()

	m.logger.Printf("Found %d entries to migrate\n", totalEntries)

	// Perform migration
	migratedCount := 0
	batchSize := 10000
	batch := badgerDB.NewWriteBatch()

	iter, _ = pebbleDB.NewIter(nil)
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := append([]byte{}, iter.Key()...)
		value := append([]byte{}, iter.Value()...)

		err := batch.Set(key, value)
		if err != nil {
			return fmt.Errorf("failed to set key in batch: %w", err)
		}

		migratedCount++

		// Commit batch periodically
		if migratedCount%batchSize == 0 {
			if err := batch.Flush(); err != nil {
				return fmt.Errorf("failed to flush batch: %w", err)
			}
			batch = badgerDB.NewWriteBatch()
			m.logger.Printf("Migrated %d/%d entries (%.1f%%)\n",
				migratedCount, totalEntries,
				float64(migratedCount)*100/float64(totalEntries))
		}
	}

	// Flush remaining entries
	if err := batch.Flush(); err != nil {
		return fmt.Errorf("failed to flush final batch: %w", err)
	}

	m.logger.Printf("Migration completed successfully! Migrated %d entries\n", migratedCount)

	// Verify migration
	if err := m.verify(pebbleDB, badgerDB); err != nil {
		return fmt.Errorf("migration verification failed: %w", err)
	}

	return nil
}

// verify checks that all data was migrated correctly
func (m *PebbleToBadgerMigrator) verify(pebbleDB *pebble.DB, badgerDB *badger.DB) error {
	m.logger.Println("Verifying migration...")

	verifiedCount := 0
	iter, _ := pebbleDB.NewIter(nil)
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		expectedValue := iter.Value()

		err := badgerDB.View(func(txn *badger.Txn) error {
			item, err := txn.Get(key)
			if err != nil {
				return fmt.Errorf("key not found in Badger: %x", key)
			}

			actualValue, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			if !bytes.Equal(expectedValue, actualValue) {
				return fmt.Errorf("value mismatch for key %x", key)
			}

			return nil
		})

		if err != nil {
			return err
		}

		verifiedCount++
		if verifiedCount%10000 == 0 {
			m.logger.Printf("Verified %d entries\n", verifiedCount)
		}
	}

	m.logger.Printf("Verification complete! All %d entries match\n", verifiedCount)
	return nil
}

// MigrateGenesis migrates genesis data specifically
func (m *PebbleToBadgerMigrator) MigrateGenesis() error {
	// Genesis-specific migration logic
	genesisKeys := []string{
		"genesis",
		"validators",
		"chains",
		"subnets",
		"utxos",
		"stakes",
		"rewards",
		"delegations",
	}

	m.logger.Println("Migrating genesis data...")

	// Open source database
	pebbleDB, err := pebble.Open(m.sourcePath, &pebble.Options{})
	if err != nil {
		return fmt.Errorf("failed to open source database: %w", err)
	}
	defer pebbleDB.Close()

	// Open target database
	badgerOpts := badger.DefaultOptions(m.targetPath)
	badgerOpts.Logger = nil
	badgerDB, err := badger.Open(badgerOpts)
	if err != nil {
		return fmt.Errorf("failed to open target database: %w", err)
	}
	defer badgerDB.Close()

	// Migrate genesis-specific keys
	for _, prefix := range genesisKeys {
		m.logger.Printf("Migrating %s data...\n", prefix)

		prefixBytes := []byte(prefix)
		iter, _ := pebbleDB.NewIter(&pebble.IterOptions{
			LowerBound: prefixBytes,
			UpperBound: append(prefixBytes, 0xFF),
		})
		defer iter.Close()

		count := 0
		batch := badgerDB.NewWriteBatch()

		for iter.First(); iter.Valid(); iter.Next() {
			key := append([]byte{}, iter.Key()...)
			value := append([]byte{}, iter.Value()...)

			if err := batch.Set(key, value); err != nil {
				return fmt.Errorf("failed to set %s key: %w", prefix, err)
			}

			count++
			if count%1000 == 0 {
				if err := batch.Flush(); err != nil {
					return fmt.Errorf("failed to flush %s batch: %w", prefix, err)
				}
				batch = badgerDB.NewWriteBatch()
			}
		}

		if err := batch.Flush(); err != nil {
			return fmt.Errorf("failed to flush final %s batch: %w", prefix, err)
		}

		m.logger.Printf("Migrated %d %s entries\n", count, prefix)
	}

	m.logger.Println("Genesis migration completed successfully!")
	return nil
}

// GetStats returns statistics about the databases
func (m *PebbleToBadgerMigrator) GetStats() (*MigrationStats, error) {
	stats := &MigrationStats{}

	// Get Pebble stats
	pebbleDB, err := pebble.Open(m.sourcePath, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open Pebble database: %w", err)
	}
	defer pebbleDB.Close()

	iter, _ := pebbleDB.NewIter(nil)
	for iter.First(); iter.Valid(); iter.Next() {
		stats.SourceEntries++
		stats.SourceBytes += int64(len(iter.Key()) + len(iter.Value()))
	}
	iter.Close()

	// Get Badger stats if it exists
	badgerOpts := badger.DefaultOptions(m.targetPath)
	badgerOpts.Logger = nil
	badgerDB, err := badger.Open(badgerOpts)
	if err == nil {
		defer badgerDB.Close()

		err = badgerDB.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			iter := txn.NewIterator(opts)
			defer iter.Close()

			for iter.Rewind(); iter.Valid(); iter.Next() {
				item := iter.Item()
				stats.TargetEntries++
				stats.TargetBytes += item.KeySize() + int64(item.ValueSize())
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get Badger stats: %w", err)
		}
	}

	return stats, nil
}

// MigrationStats holds statistics about the migration
type MigrationStats struct {
	SourceEntries int64
	SourceBytes   int64
	TargetEntries int64
	TargetBytes   int64
}

// String returns a string representation of the stats
func (s *MigrationStats) String() string {
	return fmt.Sprintf(
		"Source: %d entries (%.2f MB), Target: %d entries (%.2f MB)",
		s.SourceEntries, float64(s.SourceBytes)/(1024*1024),
		s.TargetEntries, float64(s.TargetBytes)/(1024*1024),
	)
}
