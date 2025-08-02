// (c) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"fmt"
	"path/filepath"
	
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/log"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/database"
)

// BadgerDatabaseConfig holds configuration for BadgerDB with ancient store
type BadgerDatabaseConfig struct {
	DataDir         string
	EnableAncient   bool
	AncientDir      string
	ReadOnly        bool
	SharedAncient   bool
	FreezeThreshold uint64 // Blocks to keep in main DB before freezing
}

// NewBadgerDatabase creates a new BadgerDB with optional ancient store support
func NewBadgerDatabase(luxDB database.Database, config BadgerDatabaseConfig) (ethdb.Database, error) {
	// If no BadgerDB config, fall back to wrapped database
	if config.DataDir == "" {
		log.Info("No BadgerDB config, using wrapped Lux database")
		return WrapDatabase(luxDB), nil
	}
	
	// Create BadgerDB with ancient store support
	if config.EnableAncient {
		log.Info("Creating BadgerDB with ancient store",
			"dataDir", config.DataDir,
			"ancientDir", config.AncientDir,
			"readOnly", config.ReadOnly,
			"sharedAncient", config.SharedAncient,
			"freezeThreshold", config.FreezeThreshold)
		
		// Use BadgerDB (ancient store support is handled by the wrapper)
		db, err := badgerdb.New(
			config.DataDir,
			0, // cache
			0, // handles
			"", // namespace
			config.ReadOnly)
		if err != nil {
			return nil, fmt.Errorf("failed to open BadgerDB with ancient: %w", err)
		}
		
		// Ancient store freezing is not yet supported in this BadgerDB implementation
		// TODO: Implement freezer support when needed
		if config.FreezeThreshold > 0 && !config.ReadOnly {
			log.Warn("BadgerDB freezer not yet implemented, freezeThreshold ignored")
		}
		
		return db, nil
	}
	
	// Create regular BadgerDB without ancient store
	badgerPath := filepath.Join(config.DataDir, "chaindata")
	db, err := badgerdb.New(
		badgerPath,
		0, // cache
		0, // handles
		"", // namespace
		config.ReadOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to open BadgerDB: %w", err)
	}
	
	log.Info("Created BadgerDB without ancient store", "path", badgerPath)
	return db, nil
}

// SlurpIntoAncient imports all historical data into the ancient store
func SlurpIntoAncient(sourceDB ethdb.Database, targetPath string, startBlock, endBlock uint64) error {
	log.Info("SLURPING blockchain data into ancient store!",
		"start", startBlock,
		"end", endBlock,
		"target", targetPath)
	
	// Create target database with ancient store
	config := BadgerDatabaseConfig{
		DataDir:         targetPath,
		EnableAncient:   true,
		FreezeThreshold: 90000, // Keep last 90k blocks in main DB
	}
	
	targetDB, err := NewBadgerDatabase(nil, config)
	if err != nil {
		return fmt.Errorf("failed to create target database: %w", err)
	}
	defer targetDB.Close()
	
	// Ancient store freezing is not yet supported in this BadgerDB implementation
	// TODO: Implement freezer support when needed
	
	// Import blocks in batches
	batchSize := uint64(1000)
	totalBlocks := endBlock - startBlock + 1
	
	log.Info("Starting ancient store import", "totalBlocks", totalBlocks)
	
	for start := startBlock; start <= endBlock; start += batchSize {
		end := start + batchSize - 1
		if end > endBlock {
			end = endBlock
		}
		
		// Read blocks from source
		var blocks []*types.Block
		var receipts []types.Receipts
		
		for num := start; num <= end; num++ {
			hash := rawdb.ReadCanonicalHash(sourceDB, num)
			if hash == (common.Hash{}) {
				log.Warn("Missing canonical hash", "block", num)
				continue
			}
			
			block := rawdb.ReadBlock(sourceDB, hash, num)
			if block == nil {
				log.Warn("Missing block", "number", num, "hash", hash)
				continue
			}
			
			// Note: ReadReceipts signature changed, now needs time and config instead of transactions
			// Using block.Time() for time and nil for config as a workaround
			blockReceipts := rawdb.ReadReceipts(sourceDB, hash, num, block.Time(), nil)
			
			blocks = append(blocks, block)
			receipts = append(receipts, blockReceipts)
		}
		
		if len(blocks) == 0 {
			continue
		}
		
		// Encode receipts
		encodedReceipts := make([]rlp.RawValue, len(receipts))
		for i, blockReceipts := range receipts {
			encoded, err := rlp.EncodeToBytes(blockReceipts)
			if err != nil {
				return fmt.Errorf("failed to encode receipts: %w", err)
			}
			encodedReceipts[i] = encoded
		}
		
		// Write to ancient store
		written, err := rawdb.WriteAncientBlocks(targetDB, blocks, encodedReceipts)
		if err != nil {
			return fmt.Errorf("failed to write ancient blocks: %w", err)
		}
		
		log.Info("Imported batch to ancient store",
			"from", start,
			"to", end,
			"blocks", len(blocks),
			"written", written)
	}
	
	log.Info("SLURP COMPLETE! Ancient store is ready!", "blocks", totalBlocks)
	return nil
}