// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"os"

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
	
	// Check custom keys first
	if val, err := db.Get([]byte("Height")); err == nil && len(val) == 8 {
		height := binary.BigEndian.Uint64(val)
		fmt.Printf("Height from custom key: %d\n", height)
		
		// Check if we can read the canonical hash at this height
		key := make([]byte, 9)
		key[0] = 'H'
		binary.BigEndian.PutUint64(key[1:], height)
		
		if hashBytes, err := db.Get(key); err == nil && len(hashBytes) == 32 {
			var hash common.Hash
			copy(hash[:], hashBytes)
			fmt.Printf("Block %d hash: %s\n", height, hash.Hex())
		} else {
			fmt.Printf("Failed to read canonical hash at height %d: %v\n", height, err)
		}
	} else {
		fmt.Printf("No Height key found: %v\n", err)
	}
	
	if val, err := db.Get([]byte("LastBlock")); err == nil && len(val) == 32 {
		var hash common.Hash
		copy(hash[:], val)
		fmt.Printf("LastBlock from custom key: %s\n", hash.Hex())
	} else {
		fmt.Printf("No LastBlock key found: %v\n", err)
	}
	
	// Check genesis (block 0) with direct key access
	key := make([]byte, 9)
	key[0] = 'H'
	binary.BigEndian.PutUint64(key[1:], 0)
	
	if val, err := db.Get(key); err == nil && len(val) == 32 {
		var hash common.Hash
		copy(hash[:], val)
		fmt.Printf("Genesis hash (block 0): %s\n", hash.Hex())
	} else {
		fmt.Printf("Failed to read genesis with direct key: %v\n", err)
	}
	
	// Check a few more blocks
	fmt.Printf("\nChecking first 10 blocks...\n")
	for i := uint64(0); i <= 10; i++ {
		key := make([]byte, 9)
		key[0] = 'H'
		binary.BigEndian.PutUint64(key[1:], i)
		
		if val, err := db.Get(key); err == nil && len(val) == 32 {
			var hash common.Hash
			copy(hash[:], val)
			fmt.Printf("Block %d: %s\n", i, hash.Hex())
		}
	}
	
	// Check if we have any rawdb head pointers
	fmt.Printf("\nChecking rawdb head pointers...\n")
	
	// Try to read head block hash directly
	headKeys := [][]byte{
		[]byte("LastHeader"),
		[]byte("LastBlock"),
		[]byte("LastFast"),
	}
	
	for _, key := range headKeys {
		if val, err := db.Get(key); err == nil {
			if len(val) == 32 {
				var hash common.Hash
				copy(hash[:], val)
				fmt.Printf("%s: %s\n", string(key), hash.Hex())
			} else {
				fmt.Printf("%s: %x (len=%d)\n", string(key), val, len(val))
			}
		}
	}
}