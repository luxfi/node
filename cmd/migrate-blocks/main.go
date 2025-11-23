package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/dgraph-io/badger/v4"
)

// BlockInfo stores metadata about a block
type BlockInfo struct {
	Number      uint64 `json:"number"`
	Hash        string `json:"hash"`
	Parent      string `json:"parent"`
	HasHeader   bool   `json:"has_header"`
	HasBody     bool   `json:"has_body"`
	HasReceipts bool   `json:"has_receipts"`
}

func main() {
	var (
		srcPath  = flag.String("source", "/tmp/cchain-blocks", "Source BadgerDB path with extracted blocks")
		dstPath  = flag.String("dest", "/tmp/cchain-imported", "Destination database path for C-Chain")
		verify   = flag.Bool("verify", false, "Verify migration")
		maxBlock = flag.Uint64("max", 0, "Maximum block to migrate (0 = all)")
	)
	flag.Parse()

	fmt.Println("=== Block Migration Tool ===")
	fmt.Printf("Source: %s\n", *srcPath)
	fmt.Printf("Destination: %s\n", *dstPath)
	fmt.Println()

	// Open source BadgerDB (from extract-all-blocks)
	opts := badger.DefaultOptions(*srcPath)
	opts.Logger = nil
	opts.ReadOnly = true
	srcDB, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open source DB: %v", err)
	}
	defer srcDB.Close()

	// Create destination directory
	os.RemoveAll(*dstPath)
	os.MkdirAll(*dstPath, 0755)

	// Open destination PebbleDB for C-Chain
	dstDB, err := pebble.Open(*dstPath, &pebble.Options{})
	if err != nil {
		log.Fatalf("Failed to open destination DB: %v", err)
	}
	defer dstDB.Close()

	fmt.Println("Starting migration...")
	startTime := time.Now()

	// Count blocks
	blockCount := uint64(0)
	err = srcDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()
			if len(key) == 33 && key[0] == 'H' {
				blockCount++
			}
		}
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to count blocks: %v", err)
	}

	fmt.Printf("Found %d blocks to migrate\n", blockCount)

	// Migrate blocks
	migratedCount := uint64(0)
	batch := dstDB.NewBatch()

	err = srcDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		// Iterate all keys
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			value, err := item.ValueCopy(nil)
			if err != nil {
				return fmt.Errorf("failed to get value: %w", err)
			}

			// Copy key-value to destination
			// For C-Chain, we need to add proper namespace prefix
			var dstKey []byte

			switch key[0] {
			case 'H': // Hash->number mapping
				// C-Chain format: "H" + hash
				dstKey = make([]byte, len(key))
				copy(dstKey, key)

			case 'h': // Header
				// C-Chain format: "h" + number + hash
				dstKey = make([]byte, len(key))
				copy(dstKey, key)

			case 'b': // Body
				// C-Chain format: "b" + number + hash
				dstKey = make([]byte, len(key))
				copy(dstKey, key)

			case 'r': // Receipts
				// C-Chain format: "r" + number + hash
				dstKey = make([]byte, len(key))
				copy(dstKey, key)

			default:
				// Copy other keys as-is (tip keys, etc)
				dstKey = make([]byte, len(key))
				copy(dstKey, key)
			}

			// Add to batch
			if err := batch.Set(dstKey, value, nil); err != nil {
				return fmt.Errorf("failed to set key: %w", err)
			}

			// Track progress for hash->number mappings (one per block)
			if key[0] == 'H' {
				migratedCount++
				if migratedCount%1000 == 0 {
					fmt.Printf("Migrated %d/%d blocks (%.1f%%)...\n",
						migratedCount, blockCount,
						float64(migratedCount)*100/float64(blockCount))

					// Commit batch periodically
					if err := batch.Commit(nil); err != nil {
						return fmt.Errorf("failed to commit batch: %w", err)
					}
					batch = dstDB.NewBatch()
				}

				// Check max block limit
				if *maxBlock > 0 && migratedCount >= *maxBlock {
					break
				}
			}
		}

		// Commit final batch
		if err := batch.Commit(nil); err != nil {
			return fmt.Errorf("failed to commit final batch: %w", err)
		}

		return nil
	})

	if err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("\n=== Migration Complete ===\n")
	fmt.Printf("Blocks migrated: %d\n", migratedCount)
	fmt.Printf("Time elapsed: %s\n", elapsed.Round(time.Second))
	fmt.Printf("Average speed: %.1f blocks/sec\n", float64(migratedCount)/elapsed.Seconds())

	// Verify migration if requested
	if *verify {
		fmt.Println("\nVerifying migration...")
		if err := verifyMigration(srcDB, dstDB); err != nil {
			log.Fatalf("Verification failed: %v", err)
		}
		fmt.Println("✅ Verification passed")
	}

	fmt.Printf("\nDatabase saved to: %s\n", *dstPath)
	fmt.Println("Ready for import into C-Chain")
}

func verifyMigration(srcDB *badger.DB, dstDB *pebble.DB) error {
	// Verify a sample of blocks
	blocksToCheck := []uint64{0, 100, 1000, 10000, 100000}

	for _, blockNum := range blocksToCheck {
		// Check if hash->number mapping exists
		hashKey := make([]byte, 41)
		hashKey[0] = 'h'
		binary.BigEndian.PutUint64(hashKey[1:9], blockNum)

		// Check in source
		var srcExists bool
		err := srcDB.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			opts.Prefix = hashKey[:9]
			it := txn.NewIterator(opts)
			defer it.Close()

			it.Rewind()
			srcExists = it.Valid()
			return nil
		})
		if err != nil {
			return err
		}

		// Check in destination
		iter, err := dstDB.NewIter(&pebble.IterOptions{
			LowerBound: hashKey[:9],
			UpperBound: append(hashKey[:9], 0xff),
		})
		if err != nil {
			return err
		}
		defer iter.Close()

		dstExists := iter.First()

		if srcExists != dstExists {
			return fmt.Errorf("block %d exists in source but not in destination", blockNum)
		}

		if srcExists {
			fmt.Printf("  Block %d: ✓\n", blockNum)
		}
	}

	return nil
}