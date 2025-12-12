// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/ethdb/badgerdb"
)

func main() {
	// Open the migrated database using geth's BadgerDB directly
	dbPath := "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
	
	db, err := badgerdb.New(dbPath, 256, 256, "", false)
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	
	fmt.Printf("Successfully opened database at: %s\n", dbPath)
	
	// Try to get an iterator to see what keys exist
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	
	keysByPrefix := make(map[byte]int)
	totalKeys := 0
	
	fmt.Printf("Scanning database keys...\n")
	
	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		totalKeys++
		
		if len(key) > 0 {
			prefix := key[0]
			keysByPrefix[prefix]++
		}
		
		// Sample first few keys
		if totalKeys <= 20 {
			fmt.Printf("Key %d: %x (len=%d), Value len=%d\n", totalKeys, key, len(key), len(value))
			
			// Special check for potential block data
			if len(key) == 9 && key[0] == 'H' {
				blockNum := binary.BigEndian.Uint64(key[1:])
				if len(value) == 32 {
					var hash common.Hash
					copy(hash[:], value)
					fmt.Printf("  -> Canonical hash for block %d: %s\n", blockNum, hash.Hex())
				}
			}
		}
		
		if totalKeys%100000 == 0 {
			fmt.Printf("Scanned %d keys...\n", totalKeys)
		}
		
		// Stop after reasonable amount
		if totalKeys >= 1000000 {
			fmt.Printf("Stopping scan at 1M keys\n")
			break
		}
	}
	
	if err := iter.Error(); err != nil {
		fmt.Printf("Iterator error: %v\n", err)
	}
	
	fmt.Printf("\nTotal keys scanned: %d\n", totalKeys)
	
	// Show key prefixes
	fmt.Printf("\nKey prefix distribution:\n")
	for prefix, count := range keysByPrefix {
		if prefix >= 32 && prefix <= 126 { // printable ASCII
			fmt.Printf("'%c' (0x%02x): %d keys\n", prefix, prefix, count)
		} else {
			fmt.Printf("0x%02x: %d keys\n", prefix, count)
		}
	}
	
	// Check for specific blockchain keys
	fmt.Printf("\nChecking for specific keys...\n")
	
	// Check for height info
	if val, err := db.Get([]byte("Height")); err == nil {
		if len(val) == 8 {
			height := binary.BigEndian.Uint64(val)
			fmt.Printf("Height: %d\n", height)
		} else {
			fmt.Printf("Height key found but wrong size: %d bytes\n", len(val))
		}
	} else {
		fmt.Printf("No Height key found\n")
	}
	
	// Check for LastBlock
	if val, err := db.Get([]byte("LastBlock")); err == nil {
		if len(val) == 32 {
			var hash common.Hash
			copy(hash[:], val)
			fmt.Printf("LastBlock: %s\n", hash.Hex())
		} else {
			fmt.Printf("LastBlock key found but wrong size: %d bytes\n", len(val))
		}
	} else {
		fmt.Printf("No LastBlock key found\n")
	}
	
	// Check canonical hashes for first few blocks
	fmt.Printf("\nChecking canonical hashes...\n")
	foundBlocks := 0
	for i := uint64(0); i < 100 && foundBlocks < 10; i++ {
		key := make([]byte, 9)
		key[0] = 'H'
		binary.BigEndian.PutUint64(key[1:], i)
		
		if val, err := db.Get(key); err == nil && len(val) == 32 {
			var hash common.Hash
			copy(hash[:], val)
			fmt.Printf("Block %d: %s\n", i, hash.Hex())
			foundBlocks++
		}
	}
	
	if foundBlocks == 0 {
		fmt.Printf("No canonical hashes found in first 100 blocks\n")
	}
}