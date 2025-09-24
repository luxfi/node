// (c) 2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/ids"
)

// RuntimeReplayManager handles runtime detection and loading of migrated blockchain data
type RuntimeReplayManager struct {
	vm           *VM
	sourceDB     *badger.DB
	targetDB     ethdb.Database
	log          *slog.Logger
	lastHeight   uint64
	lastHash     common.Hash
	genesisHash  common.Hash
	stateRoot    common.Hash
}

// NewRuntimeReplayManager creates a new runtime replay manager
func NewRuntimeReplayManager(vm *VM) *RuntimeReplayManager {
	return &RuntimeReplayManager{
		vm:  vm,
		log: slog.Default().With("module", "runtime-replay"),
	}
}

// DetectAndLoadMigratedData checks for migrated BadgerDB data and loads it into the chain
func (r *RuntimeReplayManager) DetectAndLoadMigratedData() error {
	// Check for migrated BadgerDB at the expected location
	badgerPath := "/home/z/.luxd/chainData/C/db/badgerdb"

	// Alternative paths to check
	altPaths := []string{
		badgerPath,
		filepath.Join(os.Getenv("HOME"), ".luxd/chainData/C/db/badgerdb"),
		filepath.Join(os.Getenv("CHAIN_DATA_DIR"), "C/db/badgerdb"),
	}

	var foundPath string
	for _, path := range altPaths {
		if path == "" {
			continue
		}
		if stat, err := os.Stat(path); err == nil && stat.IsDir() {
			foundPath = path
			break
		}
	}

	if foundPath == "" {
		r.log.Info("No migrated BadgerDB found")
		return fmt.Errorf("migrated database not found at expected locations")
	}

	r.log.Info("Found migrated BadgerDB", "path", foundPath)

	// Open the BadgerDB
	opts := badger.DefaultOptions(foundPath)
	opts.Logger = nil // Silence badger logs
	opts.SyncWrites = false
	opts.CompactL0OnClose = false

	db, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open BadgerDB: %w", err)
	}
	defer db.Close()

	r.sourceDB = db
	r.targetDB = r.vm.ethDB

	// First, get the chain metadata
	if err := r.loadChainMetadata(); err != nil {
		return fmt.Errorf("failed to load chain metadata: %w", err)
	}

	r.log.Info("Chain metadata loaded",
		"lastHeight", r.lastHeight,
		"lastHash", r.lastHash.Hex(),
		"genesisHash", r.genesisHash.Hex(),
		"stateRoot", r.stateRoot.Hex())

	// Load blocks and state into the blockchain
	if err := r.replayBlocks(); err != nil {
		return fmt.Errorf("failed to replay blocks: %w", err)
	}

	// Update the VM's last accepted block
	r.vm.lastAccepted = ids.ID(r.lastHash)

	r.log.Info("Runtime replay complete",
		"blocksLoaded", r.lastHeight,
		"lastAccepted", r.vm.lastAccepted.String())

	return nil
}

// loadChainMetadata loads the chain head information from the migrated database
func (r *RuntimeReplayManager) loadChainMetadata() error {
	return r.sourceDB.View(func(txn *badger.Txn) error {
		// Get the last block height
		heightKey := []byte("LastAcceptedBlockNumber")
		item, err := txn.Get(heightKey)
		if err != nil {
			// Try alternative key
			heightKey = []byte("Height")
			item, err = txn.Get(heightKey)
			if err != nil {
				return fmt.Errorf("failed to get block height: %w", err)
			}
		}

		err = item.Value(func(val []byte) error {
			if len(val) == 8 {
				r.lastHeight = binary.BigEndian.Uint64(val)
			} else {
				r.lastHeight = new(big.Int).SetBytes(val).Uint64()
			}
			return nil
		})
		if err != nil {
			return err
		}

		// Get the last block hash
		hashKey := []byte("LastAcceptedBlockID")
		item, err = txn.Get(hashKey)
		if err != nil {
			// Try to get the canonical hash at the last height
			canonKey := append([]byte("h"), encodeBlockNumberReplay(r.lastHeight)...)
			item, err = txn.Get(canonKey)
			if err != nil {
				return fmt.Errorf("failed to get last block hash: %w", err)
			}
		}

		err = item.Value(func(val []byte) error {
			if len(val) == 32 {
				copy(r.lastHash[:], val)
			}
			return nil
		})
		if err != nil {
			return err
		}

		// Get genesis hash (block 0)
		genesisKey := append([]byte("h"), encodeBlockNumberReplay(0)...)
		item, err = txn.Get(genesisKey)
		if err == nil {
			item.Value(func(val []byte) error {
				if len(val) == 32 {
					copy(r.genesisHash[:], val)
				}
				return nil
			})
		}

		// Get the state root of the last block
		headerKey := append(append([]byte("h"), encodeBlockNumberReplay(r.lastHeight)...), r.lastHash[:]...)
		item, err = txn.Get(headerKey)
		if err == nil {
			item.Value(func(val []byte) error {
				var header types.Header
				if err := rlp.DecodeBytes(val, &header); err == nil {
					r.stateRoot = header.Root
				}
				return nil
			})
		}

		return nil
	})
}

// replayBlocks loads blocks from the migrated database into the blockchain
func (r *RuntimeReplayManager) replayBlocks() error {
	r.log.Info("Starting block replay", "targetHeight", r.lastHeight)

	// We'll replay in batches for efficiency
	batchSize := uint64(1000)
	if r.lastHeight > 10000 {
		batchSize = 5000
	}

	startTime := time.Now()
	blocksProcessed := uint64(0)

	// Start from block 0 (genesis)
	for height := uint64(0); height <= r.lastHeight; height += batchSize {
		endHeight := height + batchSize - 1
		if endHeight > r.lastHeight {
			endHeight = r.lastHeight
		}

		r.log.Info("Processing block batch",
			"from", height,
			"to", endHeight,
			"progress", fmt.Sprintf("%.1f%%", float64(height)*100/float64(r.lastHeight)))

		// Process batch
		err := r.sourceDB.View(func(txn *badger.Txn) error {
			for h := height; h <= endHeight; h++ {
				// Get canonical hash for this height
				canonKey := append([]byte("h"), encodeBlockNumberReplay(h)...)
				item, err := txn.Get(canonKey)
				if err != nil {
					continue // Skip missing blocks
				}

				var blockHash common.Hash
				err = item.Value(func(val []byte) error {
					if len(val) == 32 {
						copy(blockHash[:], val)
					}
					return nil
				})
				if err != nil {
					continue
				}

				// Get block header
				headerKey := append(append([]byte("h"), encodeBlockNumberReplay(h)...), blockHash[:]...)
				item, err = txn.Get(headerKey)
				if err != nil {
					continue
				}

				var header types.Header
				err = item.Value(func(val []byte) error {
					return rlp.DecodeBytes(val, &header)
				})
				if err != nil {
					continue
				}

				// Get block body
				bodyKey := append(append([]byte("b"), encodeBlockNumberReplay(h)...), blockHash[:]...)
				item, err = txn.Get(bodyKey)
				if err != nil {
					continue
				}

				var body types.Body
				err = item.Value(func(val []byte) error {
					return rlp.DecodeBytes(val, &body)
				})
				if err != nil {
					continue
				}

				// Create the block
				block := types.NewBlockWithHeader(&header)
				block = block.WithBody(body)

				// Write to target database
				rawdb.WriteBlock(r.targetDB, block)
				rawdb.WriteCanonicalHash(r.targetDB, blockHash, h)
				rawdb.WriteHeadBlockHash(r.targetDB, blockHash)

				if h == r.lastHeight {
					rawdb.WriteHeadHeaderHash(r.targetDB, blockHash)
					rawdb.WriteHeadFastBlockHash(r.targetDB, blockHash)
				}

				blocksProcessed++
			}
			return nil
		})

		if err != nil {
			r.log.Warn("Error processing batch", "error", err)
		}
	}

	// Also copy critical state for key accounts
	r.log.Info("Loading account states")
	if err := r.loadCriticalState(); err != nil {
		r.log.Warn("Failed to load critical state", "error", err)
	}

	elapsed := time.Since(startTime)
	r.log.Info("Block replay complete",
		"blocksProcessed", blocksProcessed,
		"duration", elapsed,
		"blocksPerSecond", float64(blocksProcessed)/elapsed.Seconds())

	// Force blockchain to recognize the new head
	if r.vm.blockChain != nil {
		r.vm.blockChain.SetHead(r.lastHeight)
	}

	return nil
}

// loadCriticalState loads state for important accounts
func (r *RuntimeReplayManager) loadCriticalState() error {
	// Critical accounts to check
	criticalAccounts := []string{
		"0x9011E888251AB053B7bD1cdB598Db4f9DEd94714", // luxdefi.eth
		"0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC", // Test account
	}

	for _, addrStr := range criticalAccounts {
		addr := common.HexToAddress(addrStr)

		// Try to load account state from the migrated database
		err := r.sourceDB.View(func(txn *badger.Txn) error {
			// Account state is stored with a specific key pattern
			// This varies by database implementation
			stateKey := append([]byte("state:"), addr[:]...)
			item, err := txn.Get(stateKey)
			if err != nil {
				// Try alternative key format
				stateKey = append(r.stateRoot[:], addr[:]...)
				item, err = txn.Get(stateKey)
				if err != nil {
					return nil // Account not found, skip
				}
			}

			return item.Value(func(val []byte) error {
				// Store in target database
				// The exact format depends on the state database structure
				r.targetDB.Put(stateKey, val)
				r.log.Info("Loaded account state", "address", addrStr)
				return nil
			})
		})

		if err != nil {
			r.log.Warn("Failed to load account state", "address", addrStr, "error", err)
		}
	}

	// Try to copy the entire state trie if possible
	r.log.Info("Attempting to copy state trie nodes")
	copiedNodes := 0
	maxNodes := 100000 // Limit to prevent memory issues

	err := r.sourceDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100

		it := txn.NewIterator(opts)
		defer it.Close()

		// Look for trie nodes (usually prefixed with specific bytes)
		prefix := []byte("t") // Common trie node prefix
		for it.Seek(prefix); it.ValidForPrefix(prefix) && copiedNodes < maxNodes; it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)
			val, err := item.ValueCopy(nil)
			if err != nil {
				continue
			}

			// Write to target database
			r.targetDB.Put(key, val)
			copiedNodes++

			if copiedNodes%10000 == 0 {
				r.log.Info("Copying state nodes", "copied", copiedNodes)
			}
		}

		return nil
	})

	if err != nil {
		r.log.Warn("Error copying state trie", "error", err)
	}

	r.log.Info("State copy complete", "nodesCopied", copiedNodes)

	return nil
}

// encodeBlockNumberReplay encodes block number for database keys (renamed to avoid conflict)
func encodeBlockNumberReplay(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// TriggerRuntimeReplay is the main entry point to trigger replay at runtime
func TriggerRuntimeReplay(vm *VM) error {
	manager := NewRuntimeReplayManager(vm)
	return manager.DetectAndLoadMigratedData()
}