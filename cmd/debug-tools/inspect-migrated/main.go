// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/node/vms/cchainvm"
)

func main() {
	// Open the migrated database
	dbPath := "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
	
	badgerConfig := cchainvm.BadgerDatabaseConfig{
		DataDir:       dbPath,
		EnableAncient: false,
		ReadOnly:      true,
	}
	
	db, err := cchainvm.NewBadgerDatabase(nil, badgerConfig)
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	
	fmt.Printf("Successfully opened database at: %s\n", dbPath)
	
	// First, let's try to get an iterator to see what keys exist
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	
	keyMap := make(map[string]int)
	keysByPrefix := make(map[byte][]string)
	totalKeys := 0
	
	fmt.Printf("Scanning database keys...\n")
	
	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		totalKeys++
		
		if len(key) > 0 {
			prefix := key[0]
			keysByPrefix[prefix] = append(keysByPrefix[prefix], fmt.Sprintf("%x", key))
			
			// Track key patterns
			if len(key) <= 20 {
				keyMap[fmt.Sprintf("%x", key)]++
			}
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
		
		// Don't scan too many keys to avoid crashes
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
	fmt.Printf("\nKey prefixes found:\n")
	prefixes := make([]byte, 0, len(keysByPrefix))
	for prefix := range keysByPrefix {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool { return prefixes[i] < prefixes[j] })
	
	for _, prefix := range prefixes {
		keys := keysByPrefix[prefix]
		fmt.Printf("Prefix '%c' (0x%02x): %d keys\n", prefix, prefix, len(keys))
		
		// Show a few examples
		if len(keys) > 0 {
			examples := keys
			if len(examples) > 5 {
				examples = examples[:5]
			}
			for _, example := range examples {
				fmt.Printf("  Example: %s\n", example)
			}
		}
	}
	
	// Check for specific patterns that might be blockchain data
	fmt.Printf("\nLooking for blockchain-specific patterns...\n")
	
	// Check for rawdb keys
	rawdbKeys := []string{
		"H",           // Canonical hash prefix
		"h",           // Header prefix
		"b",           // Body prefix
		"r",           // Receipt prefix
		"LastHeader",  // Head header
		"LastBlock",   // Head block
		"LastFast",    // Head fast block
		"Height",      // Custom height key
		"LastBlock",   // Custom last block key
	}
	
	for _, keyStr := range rawdbKeys {
		key := []byte(keyStr)
		if value, err := db.Get(key); err == nil {
			fmt.Printf("Found key '%s': %x (len=%d)\n", keyStr, value, len(value))
		}
	}
	
	// Try to find canonical hashes for first few blocks
	fmt.Printf("\nChecking for canonical hashes (first 10 blocks)...\n")
	for i := uint64(0); i < 10; i++ {
		// Try standard rawdb format: 'H' + 8-byte block number
		key := make([]byte, 9)
		key[0] = 'H'
		binary.BigEndian.PutUint64(key[1:], i)
		
		if value, err := db.Get(key); err == nil && len(value) == 32 {
			var hash common.Hash
			copy(hash[:], value)
			fmt.Printf("Block %d: %s\n", i, hash.Hex())
		}
	}
	
	// Try to find headers by looking for 'h' prefix
	fmt.Printf("\nChecking for headers...\n")
	headerCount := 0
	iter2 := db.NewIterator([]byte("h"), nil)
	defer iter2.Release()
	
	for iter2.Next() && headerCount < 10 {
		key := iter2.Key()
		value := iter2.Value()
		headerCount++
		
		fmt.Printf("Header key %d: %x (len=%d), Value len=%d\n", headerCount, key, len(key), len(value))
		
		// If it's a standard header key format: 'h' + hash (32 bytes) + block number (8 bytes)
		if len(key) == 41 && key[0] == 'h' {
			hash := key[1:33]
			blockNum := binary.BigEndian.Uint64(key[33:])
			fmt.Printf("  -> Header for block %d, hash: %x\n", blockNum, hash)
		}
	}
	
	fmt.Printf("Found %d header keys\n", headerCount)
}