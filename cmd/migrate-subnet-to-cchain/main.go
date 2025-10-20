package main

import (
	"fmt"
	"os"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/ethdb/pebble"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s <source-pebbledb-path> <target-badgerdb-path>\n", os.Args[0])
		fmt.Printf("\nExample:\n")
		fmt.Printf("  %s /path/to/subnet/pebbledb /path/to/cchain/db\n", os.Args[0])
		os.Exit(1)
	}

	sourcePath := os.Args[1]
	targetPath := os.Args[2]

	fmt.Printf("=== SUBNET-EVM TO C-CHAIN MIGRATION ===\n")
	fmt.Printf("Source (SubnetEVM PebbleDB): %s\n", sourcePath)
	fmt.Printf("Target (C-Chain BadgerDB):   %s\n\n", targetPath)

	// Open source PebbleDB
	fmt.Printf("📖 Opening source PebbleDB...\n")
	sourceDB, err := pebble.New(sourcePath, 0, 0, "", true) // read-only
	if err != nil {
		fmt.Printf("❌ Failed to open source database: %v\n", err)
		os.Exit(1)
	}
	defer sourceDB.Close()
	fmt.Printf("✅ Source database opened\n\n")

	// Open target BadgerDB
	fmt.Printf("📝 Creating target BadgerDB...\n")

	// Remove old target if exists
	os.RemoveAll(targetPath)

	targetDB, err := badgerdb.New(targetPath, 0, 0, "", false) // read-write
	if err != nil {
		fmt.Printf("❌ Failed to create target database: %v\n", err)
		os.Exit(1)
	}
	defer targetDB.Close()
	fmt.Printf("✅ Target database created\n\n")

	// SubnetEVM namespace prefix (discovered from earlier scan)
	namespace := common.FromHex("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")
	namespaceLen := len(namespace)

	fmt.Printf("📦 Namespace to strip: %s (%d bytes)\n\n", common.Bytes2Hex(namespace), namespaceLen)

	// Iterate through all keys and copy, stripping namespace
	fmt.Printf("🔄 Starting key-value migration...\n\n")

	iter := sourceDB.NewIterator(nil, nil)
	defer iter.Release()

	batch := targetDB.NewBatch()
	count := 0
	skipped := 0
	startTime := time.Now()

	for iter.Next() {
		sourceKey := iter.Key()
		value := iter.Value()

		// Strip namespace prefix
		if len(sourceKey) < namespaceLen {
			skipped++
			continue
		}

		// Check if key has the expected namespace prefix
		hasNamespace := true
		for i := 0; i < namespaceLen; i++ {
			if sourceKey[i] != namespace[i] {
				hasNamespace = false
				break
			}
		}

		if !hasNamespace {
			skipped++
			continue
		}

		// Strip the namespace prefix
		targetKey := sourceKey[namespaceLen:]

		// Add to batch
		if err := batch.Put(targetKey, value); err != nil {
			fmt.Printf("❌ Failed to add key to batch: %v\n", err)
			os.Exit(1)
		}

		count++

		// Commit batch every 10000 keys
		if count%10000 == 0 {
			if err := batch.Write(); err != nil {
				fmt.Printf("❌ Failed to write batch: %v\n", err)
				os.Exit(1)
			}
			batch.Reset()

			elapsed := time.Since(startTime)
			keysPerSec := float64(count) / elapsed.Seconds()

			fmt.Printf("  Progress: %d keys migrated, %d skipped (%.0f keys/sec)\n",
				count, skipped, keysPerSec)
		}
	}

	// Write final batch
	if err := batch.Write(); err != nil {
		fmt.Printf("❌ Failed to write final batch: %v\n", err)
		os.Exit(1)
	}

	if err := iter.Error(); err != nil {
		fmt.Printf("❌ Iterator error: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("\n✅ Migration complete!\n")
	fmt.Printf("   Keys migrated: %d\n", count)
	fmt.Printf("   Keys skipped:  %d\n", skipped)
	fmt.Printf("   Time taken:    %s\n", elapsed.Round(time.Second))
	fmt.Printf("   Average speed: %.0f keys/sec\n\n", float64(count)/elapsed.Seconds())

	fmt.Printf("📍 Target database ready at: %s\n", targetPath)
	fmt.Printf("\nYou can now start your C-Chain node using this database!\n")
}
