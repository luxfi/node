package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/ethdb/pebble"
	"github.com/luxfi/geth/rlp"
)

// encodeBlockNumber encodes a block number as big-endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// headerKey returns the key for block header storage
// Format: 'h' + blockNumber + blockHash
func headerKey(number uint64, hash common.Hash) []byte {
	return append(append([]byte("h"), encodeBlockNumber(number)...), hash.Bytes()...)
}

// bodyKey returns the key for block body storage
// Format: 'b' + blockNumber + blockHash
func bodyKey(number uint64, hash common.Hash) []byte {
	return append(append([]byte("b"), encodeBlockNumber(number)...), hash.Bytes()...)
}

// canonicalHashKey returns the key for the canonical hash mapping
// Format: 'h' + blockNumber + 'n'
func canonicalHashKey(number uint64) []byte {
	return append(append([]byte("h"), encodeBlockNumber(number)...), 'n')
}

// isHeaderKey checks if a key is a header key
func isHeaderKey(key []byte) (bool, uint64, common.Hash) {
	if len(key) != 41 || key[0] != 'h' {
		return false, 0, common.Hash{}
	}
	number := binary.BigEndian.Uint64(key[1:9])
	hash := common.BytesToHash(key[9:41])
	return true, number, hash
}

// ReplayStats tracks replay statistics
type ReplayStats struct {
	BlocksReplayed    uint64
	TransactionsTotal uint64
	GasUsed           uint64
	StartBlock        uint64
	EndBlock          uint64
	StartTime         time.Time
	LastPrintTime     time.Time
}

func (s *ReplayStats) Print(current uint64) {
	elapsed := time.Since(s.StartTime)
	if elapsed.Seconds() > 0 {
		blocksPerSec := float64(s.BlocksReplayed) / elapsed.Seconds()
		fmt.Printf("\r  [Block %d] Replayed: %d blocks, %d txs, %.0f blocks/sec",
			current, s.BlocksReplayed, s.TransactionsTotal, blocksPerSec)
	}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s <source-pebbledb-path> <target-badgerdb-path> [start-block] [end-block]\n", os.Args[0])
		fmt.Printf("\nReplay historic blockchain data from old subnet EVM into new C-Chain\n")
		fmt.Printf("\nExamples:\n")
		fmt.Printf("  # Replay all blocks:\n")
		fmt.Printf("  %s ~/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb ~/.luxd/db/C\n", os.Args[0])
		fmt.Printf("\n  # Replay specific range:\n")
		fmt.Printf("  %s ~/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb ~/.luxd/db/C 0 10000\n", os.Args[0])
		fmt.Printf("\nAvailable historic chains:\n")

		// List available chains
		stateDir := filepath.Join(os.Getenv("HOME"), "work/lux/state/chaindata")
		if entries, err := os.ReadDir(stateDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					dbPath := filepath.Join(stateDir, entry.Name(), "db/pebbledb")
					if _, err := os.Stat(dbPath); err == nil {
						fmt.Printf("  - %s\n", entry.Name())
					}
				}
			}
		}
		os.Exit(1)
	}

	sourcePath := os.Args[1]
	targetPath := os.Args[2]

	var startBlock, endBlock uint64 = 0, ^uint64(0) // Max uint64 = replay all
	if len(os.Args) >= 4 {
		fmt.Sscanf(os.Args[3], "%d", &startBlock)
	}
	if len(os.Args) >= 5 {
		fmt.Sscanf(os.Args[4], "%d", &endBlock)
	}

	fmt.Printf("╔═══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║     HISTORIC BLOCKCHAIN REPLAY - QUASAR EVENT HORIZON        ║\n")
	fmt.Printf("╚═══════════════════════════════════════════════════════════════╝\n\n")

	fmt.Printf("📖 Source (Old Subnet EVM PebbleDB): %s\n", sourcePath)
	fmt.Printf("📝 Target (New C-Chain BadgerDB):    %s\n", targetPath)
	fmt.Printf("🔢 Block Range: %d → %d\n\n", startBlock, endBlock)

	// Open source PebbleDB (read-only)
	fmt.Printf("📖 Opening historic database (PebbleDB)...\n")
	sourceDB, err := pebble.New(sourcePath, 0, 0, "", true)
	if err != nil {
		fmt.Printf("❌ Failed to open source database: %v\n", err)
		os.Exit(1)
	}
	defer sourceDB.Close()
	fmt.Printf("✅ Historic database opened\n\n")

	// Open target BadgerDB (read-write)
	fmt.Printf("📝 Opening target C-Chain database (BadgerDB)...\n")

	// Create target directory if doesn't exist
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		fmt.Printf("❌ Failed to create target directory: %v\n", err)
		os.Exit(1)
	}

	targetDB, err := badgerdb.New(targetPath, 0, 0, "", false)
	if err != nil {
		fmt.Printf("❌ Failed to open target database: %v\n", err)
		os.Exit(1)
	}
	defer targetDB.Close()
	fmt.Printf("✅ Target database opened\n\n")

	// EVM namespace prefix (from subnet-evm)
	namespace := common.FromHex("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")
	namespaceLen := len(namespace)

	fmt.Printf("🔄 Starting historic blockchain replay into event horizon...\n\n")

	stats := &ReplayStats{
		StartBlock:    startBlock,
		EndBlock:      endBlock,
		StartTime:     time.Now(),
		LastPrintTime: time.Now(),
	}

	// Iterate through source database
	iter := sourceDB.NewIterator(nil, nil)
	defer iter.Release()

	batch := targetDB.NewBatch()
	batchSize := 0
	keysCopied := 0
	blocksProcessed := make(map[uint64]bool)

	for iter.Next() {
		sourceKey := iter.Key()
		value := iter.Value()

		// Strip namespace if present
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

		// Check if this is a header key in our range
		if isHeader, blockNum, blockHash := isHeaderKey(targetKey); isHeader {
			if blockNum < startBlock || blockNum > endBlock {
				continue // Skip blocks outside range
			}

			// Try to decode header to get transaction count
			var header types.Header
			if err := rlp.DecodeBytes(value, &header); err == nil {
				if !blocksProcessed[blockNum] {
					stats.BlocksReplayed++
					stats.GasUsed += header.GasUsed
					blocksProcessed[blockNum] = true

					// Estimate tx count from gas (avg 21000 gas per tx)
					if header.GasUsed > 0 {
						stats.TransactionsTotal += header.GasUsed / 21000
					}

					// Print progress every second
					if time.Since(stats.LastPrintTime) >= time.Second {
						stats.Print(blockNum)
						stats.LastPrintTime = time.Now()
					}
				}
			}

			// Create canonical hash mapping
			canonicalKey := canonicalHashKey(blockNum)
			if err := batch.Put(canonicalKey, blockHash.Bytes()); err != nil {
				fmt.Printf("\n❌ Failed to add canonical mapping: %v\n", err)
				os.Exit(1)
			}
		}

		// Add key-value to batch
		if err := batch.Put(targetKey, value); err != nil {
			fmt.Printf("\n❌ Failed to add key to batch: %v\n", err)
			os.Exit(1)
		}

		keysCopied++
		batchSize++

		// Commit batch every 10000 keys
		if batchSize >= 10000 {
			if err := batch.Write(); err != nil {
				fmt.Printf("\n❌ Failed to write batch: %v\n", err)
				os.Exit(1)
			}
			batch.Reset()
			batchSize = 0
		}
	}

	// Write final batch
	if err := batch.Write(); err != nil {
		fmt.Printf("\n❌ Failed to write final batch: %v\n", err)
		os.Exit(1)
	}

	if err := iter.Error(); err != nil {
		fmt.Printf("\n❌ Iterator error: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(stats.StartTime)
	fmt.Printf("\n\n")
	fmt.Printf("╔═══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║                     REPLAY COMPLETE ✅                        ║\n")
	fmt.Printf("╚═══════════════════════════════════════════════════════════════╝\n\n")

	fmt.Printf("📊 Replay Statistics:\n")
	fmt.Printf("   Blocks Replayed:     %d\n", stats.BlocksReplayed)
	fmt.Printf("   Transactions (est):  %d\n", stats.TransactionsTotal)
	fmt.Printf("   Total Gas Used:      %d\n", stats.GasUsed)
	fmt.Printf("   Database Keys:       %d\n", keysCopied)
	fmt.Printf("   Time Taken:          %s\n", elapsed.Round(time.Second))

	if elapsed.Seconds() > 0 {
		fmt.Printf("   Average Speed:       %.0f blocks/sec\n", float64(stats.BlocksReplayed)/elapsed.Seconds())
	}

	fmt.Printf("\n🌌 Historic chain pulled into Quasar event horizon!\n")
	fmt.Printf("📍 Target database: %s\n", targetPath)
	fmt.Printf("\n✅ C-Chain ready to start with historic state\n")
	fmt.Printf("   Run: luxd --network-id=96369 --data-dir=~/.luxd\n\n")
}
