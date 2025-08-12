package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	
	"github.com/dgraph-io/badger/v4"
)

func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: convert_db_format <source_db> [dest_db]")
		os.Exit(1)
	}
	
	sourceDB := os.Args[1]
	destDB := sourceDB + "_converted"
	if len(os.Args) > 2 {
		destDB = os.Args[2]
	}
	
	fmt.Printf("Converting database format\n")
	fmt.Printf("Source: %s\n", sourceDB)
	fmt.Printf("Destination: %s\n", destDB)
	
	// Open source database (read-only)
	srcOpts := badger.DefaultOptions(sourceDB)
	srcOpts.ReadOnly = true
	
	srcDB, err := badger.Open(srcOpts)
	if err != nil {
		log.Fatalf("Failed to open source database: %v", err)
	}
	defer srcDB.Close()
	
	// Create destination database
	os.MkdirAll(destDB, 0755)
	destOpts := badger.DefaultOptions(destDB)
	
	dstDB, err := badger.Open(destOpts)
	if err != nil {
		log.Fatalf("Failed to create destination database: %v", err)
	}
	defer dstDB.Close()
	
	fmt.Println("Phase 1: Converting canonical block mappings...")
	
	// First pass: Convert canonical mappings from migrated format to standard format
	canonicalCount := 0
	blockHeaders := make(map[uint64][]byte)
	
	err = srcDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 1000
		
		it := txn.NewIterator(opts)
		defer it.Close()
		
		// Process blocks with 'h' prefix (migrated format)
		prefix := []byte("h")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.Key()
			
			if len(key) == 41 {
				// Extract block number and hash from key
				blockNum := binary.BigEndian.Uint64(key[1:9])
				hash := key[9:41]
				
				// Get the block header data
				val, err := item.ValueCopy(nil)
				if err == nil {
					blockHeaders[blockNum] = val
					
					// Write canonical mapping in standard format: 'H' + blockNum -> hash
					canonicalKey := append([]byte("H"), encodeBlockNumber(blockNum)...)
					
					err = dstDB.Update(func(txn *badger.Txn) error {
						return txn.Set(canonicalKey, hash)
					})
					if err != nil {
						return fmt.Errorf("failed to write canonical mapping for block %d: %w", blockNum, err)
					}
					
					// Also write the block header with standard key format
					// Standard: 'h' + blockNum + hash + 'n' suffix for header
					headerKey := make([]byte, 42)
					headerKey[0] = 'h'
					copy(headerKey[1:9], encodeBlockNumber(blockNum))
					copy(headerKey[9:41], hash)
					headerKey[41] = 'n'
					
					err = dstDB.Update(func(txn *badger.Txn) error {
						return txn.Set(headerKey, val)
					})
					if err != nil {
						return fmt.Errorf("failed to write header for block %d: %w", blockNum, err)
					}
					
					canonicalCount++
					if canonicalCount%10000 == 0 {
						fmt.Printf("  Converted %d blocks...\n", canonicalCount)
					}
				}
			}
		}
		
		return nil
	})
	
	if err != nil {
		log.Fatalf("Error converting canonical mappings: %v", err)
	}
	
	fmt.Printf("Converted %d canonical block mappings\n", canonicalCount)
	
	// Phase 2: Copy other important keys
	fmt.Println("\nPhase 2: Copying other database keys...")
	
	copiedKeys := 0
	err = srcDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		
		it := txn.NewIterator(opts)
		defer it.Close()
		
		// Copy non-block keys (state, receipts, etc.)
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()
			
			// Skip 'h' prefix keys (already processed)
			if len(key) > 0 && key[0] == 'h' {
				continue
			}
			
			// Copy all other keys
			val, err := item.ValueCopy(nil)
			if err == nil {
				err = dstDB.Update(func(txn *badger.Txn) error {
					return txn.Set(key, val)
				})
				if err != nil {
					fmt.Printf("Warning: Failed to copy key %x: %v\n", key, err)
				} else {
					copiedKeys++
					if copiedKeys%10000 == 0 {
						fmt.Printf("  Copied %d additional keys...\n", copiedKeys)
					}
				}
			}
		}
		
		return nil
	})
	
	if err != nil {
		log.Fatalf("Error copying keys: %v", err)
	}
	
	fmt.Printf("Copied %d additional keys\n", copiedKeys)
	
	// Write head pointers
	fmt.Println("\nPhase 3: Setting head pointers...")
	
	// Find the highest block
	var highestBlock uint64
	var highestHash []byte
	
	for blockNum := range blockHeaders {
		if blockNum > highestBlock {
			highestBlock = blockNum
		}
	}
	
	// Get the hash for the highest block
	canonicalKey := append([]byte("H"), encodeBlockNumber(highestBlock)...)
	err = dstDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get(canonicalKey)
		if err == nil {
			highestHash, err = item.ValueCopy(nil)
		}
		return err
	})
	
	if err != nil {
		log.Fatalf("Failed to get hash for highest block %d: %v", highestBlock, err)
	}
	
	// Write head pointers
	headKeys := []string{
		"LastBlock",
		"LastHeader", 
		"LastFast",
	}
	
	for _, keyName := range headKeys {
		err = dstDB.Update(func(txn *badger.Txn) error {
			return txn.Set([]byte(keyName), highestHash)
		})
		if err != nil {
			fmt.Printf("Warning: Failed to write %s: %v\n", keyName, err)
		} else {
			fmt.Printf("  Set %s to block %d (hash %x)\n", keyName, highestBlock, highestHash[:8])
		}
	}
	
	// Write the height
	err = dstDB.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("Height"), encodeBlockNumber(highestBlock))
	})
	if err != nil {
		fmt.Printf("Warning: Failed to write Height: %v\n", err)
	}
	
	fmt.Printf("\n=== Conversion Complete ===\n")
	fmt.Printf("Total blocks: %d\n", canonicalCount)
	fmt.Printf("Highest block: %d\n", highestBlock)
	fmt.Printf("Database saved to: %s\n", destDB)
	
	// Verify the conversion
	fmt.Println("\nVerifying conversion...")
	
	// Check block 0
	block0Key := append([]byte("H"), encodeBlockNumber(0)...)
	err = dstDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get(block0Key)
		if err == nil {
			hash, _ := item.ValueCopy(nil)
			fmt.Printf("  Genesis block (0): %x\n", hash)
		}
		return err
	})
	
	// Check highest block
	err = dstDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get(canonicalKey)
		if err == nil {
			hash, _ := item.ValueCopy(nil)
			fmt.Printf("  Highest block (%d): %x\n", highestBlock, hash)
		}
		return err
	})
	
	fmt.Println("\nConversion successful!")
}