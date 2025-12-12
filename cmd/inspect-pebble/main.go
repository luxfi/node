// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"

	"github.com/cockroachdb/pebble"
	"github.com/luxfi/geth/common"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: inspect-pebble <pebbledb_path>")
		os.Exit(1)
	}

	dbPath := os.Args[1]
	fmt.Printf("Opening PebbleDB at: %s\n", dbPath)

	opts := &pebble.Options{
		ReadOnly: true,
	}

	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		log.Fatalf("Failed to open PebbleDB: %v", err)
	}
	defer db.Close()

	fmt.Println("✓ Successfully opened PebbleDB")
	fmt.Println()

	// Stats
	keyCount := 0
	blockCount := 0
	headerCount := 0
	stateCount := 0
	receiptCount := 0
	txCount := 0
	
	// Look for latest block info
	var lastBlockNum uint64
	var lastHash common.Hash

	// Create iterator
	iter, err := db.NewIter(nil)
	if err != nil {
		log.Fatalf("Failed to create iterator: %v", err)
	}
	defer iter.Close()

	fmt.Println("Analyzing database contents...")
	fmt.Println()

	// Iterate through all keys
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		keyCount++
		
		if len(key) > 0 {
			prefix := string(key[0:1])
			
			switch prefix {
			case "b", "B": // Block data
				blockCount++
				if blockCount <= 3 {
					if len(key) >= 9 {
						// Try to extract block number
						num := binary.BigEndian.Uint64(key[1:9])
						if num > lastBlockNum {
							lastBlockNum = num
						}
						fmt.Printf("  Block key found: prefix=%s, number=%d\n", prefix, num)
					}
				}
			case "h", "H": // Headers
				headerCount++
				if headerCount <= 3 && len(key) >= 33 {
					// Extract hash from key
					hash := common.BytesToHash(key[len(key)-32:])
					fmt.Printf("  Header key found: hash=%s\n", hash.Hex()[:10]+"...")
				}
			case "r", "R": // Receipts
				receiptCount++
			case "t": // Transactions
				txCount++
			case "n", "c", "s": // State data (accounts, contracts, storage)
				stateCount++
			}
		}

		// Stop after scanning enough keys to get statistics
		if keyCount >= 100000 {
			// Continue counting but stop printing
			for iter.Next() {
				key := iter.Key()
				keyCount++
				
				if len(key) > 0 {
					prefix := string(key[0:1])
					switch prefix {
					case "b", "B":
						blockCount++
						if len(key) >= 9 {
							num := binary.BigEndian.Uint64(key[1:9])
							if num > lastBlockNum {
								lastBlockNum = num
							}
						}
					case "h", "H":
						headerCount++
					case "r", "R":
						receiptCount++
					case "t":
						txCount++
					case "n", "c", "s":
						stateCount++
					}
				}
			}
			break
		}
	}

	// Check for specific metadata keys
	fmt.Println("\nChecking metadata keys:")
	metaKeys := []string{
		"LastBlock",
		"LastHeader",
		"LastFast",
		"eth-block-height-",
		"LastPivot",
	}

	for _, k := range metaKeys {
		value, closer, err := db.Get([]byte(k))
		if err == nil {
			if k == "LastBlock" && len(value) == 32 {
				lastHash = common.BytesToHash(value)
				fmt.Printf("  %s: %s\n", k, lastHash.Hex())
			} else if len(value) == 8 {
				height := binary.BigEndian.Uint64(value)
				fmt.Printf("  %s: %d\n", k, height)
			} else {
				fmt.Printf("  %s: found (%d bytes)\n", k, len(value))
			}
			closer.Close()
		}
	}

	// Try to find highest block by checking canonical chain
	fmt.Println("\nScanning for highest block...")
	for i := lastBlockNum; i < lastBlockNum+1000 && i < 2000000; i++ {
		// Check canonical hash key: "H" + block number
		canonicalKey := append([]byte("H"), make([]byte, 8)...)
		binary.BigEndian.PutUint64(canonicalKey[1:], i)
		
		if _, closer, err := db.Get(canonicalKey); err == nil {
			lastBlockNum = i
			closer.Close()
		} else {
			// No more blocks found
			break
		}
	}

	fmt.Println("\n" + "============================================================")
	fmt.Println("DATABASE SUMMARY")
	fmt.Println("============================================================")
	fmt.Printf("Total keys scanned: %d\n", keyCount)
	fmt.Printf("Blocks:             %d\n", blockCount)
	fmt.Printf("Headers:            %d\n", headerCount)
	fmt.Printf("Receipts:           %d\n", receiptCount)
	fmt.Printf("Transactions:       %d\n", txCount)
	fmt.Printf("State entries:      %d\n", stateCount)
	fmt.Printf("\nHighest block:      %d\n", lastBlockNum)
	
	if lastHash != (common.Hash{}) {
		fmt.Printf("Last block hash:    %s\n", lastHash.Hex())
	}

	expectedBlocks := uint64(1074616)
	fmt.Printf("\nExpected blocks:    %d\n", expectedBlocks)
	
	if lastBlockNum > 0 {
		if lastBlockNum >= expectedBlocks {
			fmt.Printf("✅ Database contains all expected blocks!\n")
		} else {
			fmt.Printf("⚠️  Database has %d blocks, missing %d blocks\n", lastBlockNum, expectedBlocks-lastBlockNum)
		}
	} else {
		fmt.Println("⚠️  Could not determine block count from database")
	}
}