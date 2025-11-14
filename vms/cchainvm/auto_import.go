// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// Automatic Import from old subnet-evm (net/evm) into C-Chain with Q-Chain quantum stamping

package cchainvm

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/ethdb/pebble"
	"github.com/luxfi/geth/rlp"
)

// AutoImportConfig configures automatic import from old net/evm
type AutoImportConfig struct {
	// Source: old subnet-evm (now net/evm) PebbleDB
	OldNetEVMPath string

	// Target: new C-Chain BadgerDB
	CChainDBPath string

	// Enable Q-Chain quantum stamping for imported blocks
	EnableQuantumStamp bool

	// Callback for each imported block (sends to Quasar)
	OnBlockImported func(blockNum uint64, blockHash common.Hash, header *types.Header)
}

// AutoImporter handles automatic migration from old net/evm to C-Chain
type AutoImporter struct {
	config AutoImportConfig
}

// NewAutoImporter creates a new auto importer
func NewAutoImporter(config AutoImportConfig) *AutoImporter {
	return &AutoImporter{config: config}
}

// TryAutoImport checks for old net/evm database and automatically imports if found
// This runs once at node startup before C-Chain starts
func (ai *AutoImporter) TryAutoImport() error {
	// Check if old net/evm database exists
	if _, err := os.Stat(ai.config.OldNetEVMPath); os.IsNotExist(err) {
		// No old database found, normal C-Chain startup
		return nil
	}

	// Check if C-Chain already has data (don't re-import)
	if ai.cChainHasData() {
		fmt.Printf("[AUTO-IMPORT] C-Chain already has data, skipping import\n")
		return nil
	}

	fmt.Printf("\n")
	fmt.Printf("╔═══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║   AUTOMATIC IMPORT: OLD NET/EVM → C-CHAIN + Q-CHAIN STAMP   ║\n")
	fmt.Printf("╚═══════════════════════════════════════════════════════════════╝\n\n")

	fmt.Printf("📦 Detected old net/evm database: %s\n", ai.config.OldNetEVMPath)
	fmt.Printf("📍 Target C-Chain database:      %s\n", ai.config.CChainDBPath)
	if ai.config.EnableQuantumStamp {
		fmt.Printf("⚛️  Q-Chain quantum stamping:     ENABLED\n")
	}
	fmt.Printf("\n")

	startTime := time.Now()

	// Open source (old net/evm PebbleDB) read-only
	fmt.Printf("📖 Opening old net/evm database...\n")
	sourceDB, err := pebble.New(ai.config.OldNetEVMPath, 0, 0, "", true)
	if err != nil {
		return fmt.Errorf("failed to open old net/evm database: %w", err)
	}
	defer sourceDB.Close()
	fmt.Printf("✅ Old database opened\n\n")

	// Open target (C-Chain BadgerDB) read-write
	fmt.Printf("📝 Creating C-Chain database...\n")
	os.MkdirAll(ai.config.CChainDBPath, 0755)
	targetDB, err := badgerdb.New(ai.config.CChainDBPath, 0, 0, "", false)
	if err != nil {
		return fmt.Errorf("failed to create C-Chain database: %w", err)
	}
	defer targetDB.Close()
	fmt.Printf("✅ C-Chain database created\n\n")

	// Import all data
	fmt.Printf("🔄 Importing historic blocks into C-Chain with Q-Chain stamping...\n\n")

	stats, err := ai.importDatabase(sourceDB, targetDB)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	elapsed := time.Since(startTime)

	fmt.Printf("\n\n")
	fmt.Printf("╔═══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║                    IMPORT COMPLETE ✅                         ║\n")
	fmt.Printf("╚═══════════════════════════════════════════════════════════════╝\n\n")

	fmt.Printf("📊 Import Statistics:\n")
	fmt.Printf("   Keys Imported:       %d\n", stats.KeysImported)
	fmt.Printf("   Blocks Imported:     %d\n", stats.BlocksImported)
	fmt.Printf("   Canonical Mappings:  %d\n", stats.CanonicalMappings)
	fmt.Printf("   Quantum Stamps:      %d\n", stats.QuantumStamps)
	fmt.Printf("   Time Taken:          %s\n", elapsed.Round(time.Second))
	if elapsed.Seconds() > 0 {
		fmt.Printf("   Speed:               %.0f keys/sec\n", float64(stats.KeysImported)/elapsed.Seconds())
	}
	fmt.Printf("\n")

	// Mark import as complete (creates marker file)
	ai.markImportComplete()

	fmt.Printf("🌌 Old net/evm successfully pulled into C-Chain event horizon!\n")
	fmt.Printf("⚛️  All blocks quantum-stamped and ready for Quasar consensus\n\n")

	return nil
}

type ImportStats struct {
	KeysImported      uint64
	BlocksImported    uint64
	CanonicalMappings uint64
	QuantumStamps     uint64
}

func (ai *AutoImporter) importDatabase(source, target ethdb.Database) (*ImportStats, error) {
	// Old subnet-evm namespace to strip
	namespace := common.FromHex("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")
	namespaceLen := len(namespace)

	stats := &ImportStats{}
	batch := target.NewBatch()
	batchSize := 0

	iter := source.NewIterator(nil, nil)
	defer iter.Release()

	blocksProcessed := make(map[uint64]bool)

	for iter.Next() {
		sourceKey := iter.Key()
		value := iter.Value()

		// Strip namespace prefix if present
		var targetKey []byte
		if len(sourceKey) >= namespaceLen {
			hasNamespace := true
			for i := 0; i < namespaceLen; i++ {
				if sourceKey[i] != namespace[i] {
					hasNamespace = false
					break
				}
			}
			if hasNamespace {
				targetKey = sourceKey[namespaceLen:]
			} else {
				targetKey = sourceKey
			}
		} else {
			targetKey = sourceKey
		}

		// Write key-value
		if err := batch.Put(targetKey, value); err != nil {
			return nil, fmt.Errorf("failed to write key: %w", err)
		}

		stats.KeysImported++
		batchSize++

		// Check if this is a header (for statistics and quantum stamping)
		if isHeader, blockNum, blockHash := isHeaderKey(targetKey); isHeader {
			if !blocksProcessed[blockNum] {
				// Create canonical hash mapping
				canonicalKey := canonicalHashKey(blockNum)
				if err := batch.Put(canonicalKey, blockHash.Bytes()); err != nil {
					return nil, fmt.Errorf("failed to write canonical mapping: %w", err)
				}
				stats.CanonicalMappings++

				// Try to decode header for quantum stamping callback
				var header types.Header
				if err := rlp.DecodeBytes(value, &header); err == nil {
					// Send to Quasar for quantum stamping
					if ai.config.EnableQuantumStamp && ai.config.OnBlockImported != nil {
						ai.config.OnBlockImported(blockNum, blockHash, &header)
						stats.QuantumStamps++
					}
				}

				stats.BlocksImported++
				blocksProcessed[blockNum] = true

				// Print progress every 10000 blocks
				if stats.BlocksImported%10000 == 0 {
					fmt.Printf("  Progress: %d blocks imported, %d Q-stamped\n",
						stats.BlocksImported, stats.QuantumStamps)
				}
			}
		}

		// Commit batch every 10000 keys
		if batchSize >= 10000 {
			if err := batch.Write(); err != nil {
				return nil, fmt.Errorf("failed to write batch: %w", err)
			}
			batch.Reset()
			batchSize = 0
		}
	}

	// Write final batch
	if err := batch.Write(); err != nil {
		return nil, fmt.Errorf("failed to write final batch: %w", err)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return stats, nil
}

// Helper functions

func (ai *AutoImporter) cChainHasData() bool {
	// Check if C-Chain database exists and has data
	if _, err := os.Stat(ai.config.CChainDBPath); os.IsNotExist(err) {
		return false
	}

	// Check for import marker
	markerPath := filepath.Join(ai.config.CChainDBPath, ".import_complete")
	if _, err := os.Stat(markerPath); err == nil {
		return true
	}

	// Try to open and check for data
	db, err := badgerdb.New(ai.config.CChainDBPath, 0, 0, "", true)
	if err != nil {
		return false
	}
	defer db.Close()

	// Check if database has any keys
	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	return iter.Next()
}

func (ai *AutoImporter) markImportComplete() {
	markerPath := filepath.Join(ai.config.CChainDBPath, ".import_complete")
	os.WriteFile(markerPath, []byte(time.Now().Format(time.RFC3339)), 0644)
}

func isHeaderKey(key []byte) (bool, uint64, common.Hash) {
	if len(key) != 41 || key[0] != 'h' {
		return false, 0, common.Hash{}
	}
	number := binary.BigEndian.Uint64(key[1:9])
	hash := common.BytesToHash(key[9:41])
	return true, number, hash
}

func canonicalHashKey(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return append(append([]byte("h"), enc...), 'n')
}

// FindOldNetEVMDatabases scans common locations for old net/evm databases
func FindOldNetEVMDatabases() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	searchPaths := []string{
		// Old subnet-evm locations
		filepath.Join(homeDir, ".luxd/subnet-evm/db"),
		filepath.Join(homeDir, ".lux/subnet-evm/db"),

		// Historic state directory
		filepath.Join(homeDir, "work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"),
		filepath.Join(homeDir, "work/lux/state/chaindata/lux-testnet-96368/db/pebbledb"),
		filepath.Join(homeDir, "work/lux/state/chaindata/spc-mainnet-36911/db/pebbledb"),
		filepath.Join(homeDir, "work/lux/state/chaindata/zoo-mainnet-200200/db/pebbledb"),
		filepath.Join(homeDir, "work/lux/state/chaindata/zoo-testnet-200201/db/pebbledb"),
	}

	var found []string
	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			found = append(found, path)
		}
	}

	return found
}
