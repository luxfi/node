// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log"

	"github.com/cockroachdb/pebble"
)

func main() {
	var dbPath = flag.String("db", "/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb", "PebbleDB path")
	flag.Parse()

	// Open PebbleDB
	db, err := pebble.Open(*dbPath, &pebble.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// SubnetEVM namespace
	namespace, _ := hex.DecodeString("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")

	fmt.Println("=== Looking for Block Data ===")
	
	// Common block storage patterns to look for
	prefixes := []struct{
		name string
		prefix []byte
	}{
		{"Header (h)", append(namespace, 'h')},
		{"Body (b)", append(namespace, 'b')},
		{"Receipt (r)", append(namespace, 'r')},
		{"Hash (H)", append(namespace, 'H')},
		{"Canonical (c)", append(namespace, 'c')},
		{"LastBlock", append(namespace, []byte("LastBlock")...)},
		{"LastAccepted", append(namespace, []byte("LastAccepted")...)},
		{"AcceptorTipKey", append(namespace, []byte("AcceptorTipKey")...)},
		{"AcceptorTipHeightKey", append(namespace, []byte("AcceptorTipHeightKey")...)},
	}

	for _, p := range prefixes {
		fmt.Printf("\nSearching for %s (prefix: %s):\n", p.name, hex.EncodeToString(p.prefix))
		
		iter, err := db.NewIter(&pebble.IterOptions{
			LowerBound: p.prefix,
			UpperBound: append(p.prefix, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF),
		})
		if err != nil {
			fmt.Printf("  Error creating iterator: %v\n", err)
			continue
		}
		
		count := 0
		for iter.First(); iter.Valid() && count < 5; iter.Next() {
			key := iter.Key()
			value := iter.Value()
			count++
			
			fmt.Printf("  Key %d: len=%d\n", count, len(key))
			
			// Show key structure
			if len(key) > len(p.prefix) {
				remainder := key[len(p.prefix):]
				fmt.Printf("    After prefix: %s\n", hex.EncodeToString(remainder))
				
				// Try to interpret as block number + hash
				if len(remainder) >= 8 {
					blockNum := binary.BigEndian.Uint64(remainder[:8])
					fmt.Printf("    Block number: %d\n", blockNum)
				}
				if len(remainder) >= 40 {
					blockHash := remainder[8:40]
					fmt.Printf("    Block hash: %s\n", hex.EncodeToString(blockHash))
				}
			}
			
			// Show value
			if len(value) <= 64 {
				fmt.Printf("    Value: %s\n", hex.EncodeToString(value))
			} else {
				fmt.Printf("    Value: %d bytes (first 32: %s)\n", len(value), hex.EncodeToString(value[:32]))
			}
		}
		iter.Close()
		
		if count == 0 {
			fmt.Printf("  No keys found\n")
		} else {
			// Count total
			iter, _ = db.NewIter(&pebble.IterOptions{
				LowerBound: p.prefix,
				UpperBound: append(p.prefix, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF),
			})
			total := 0
			for iter.First(); iter.Valid(); iter.Next() {
				total++
			}
			iter.Close()
			fmt.Printf("  Total keys: %d\n", total)
		}
	}

	// Also look for canonical chain mappings
	fmt.Println("\n=== Looking for Canonical Chain Mappings ===")
	
	// Try different canonical patterns
	patterns := [][]byte{
		append(namespace, []byte("canonical")...),
		append(namespace, 'n'), // number to hash
		append(namespace, 'l'), // last block
	}
	
	for _, pattern := range patterns {
		fmt.Printf("\nPattern %s:\n", hex.EncodeToString(pattern))
		iter, _ := db.NewIter(&pebble.IterOptions{
			LowerBound: pattern,
			UpperBound: append(pattern, 0xFF, 0xFF, 0xFF, 0xFF),
		})
		
		count := 0
		for iter.First(); iter.Valid() && count < 3; iter.Next() {
			key := iter.Key()
			value := iter.Value()
			count++
			
			fmt.Printf("  Key: %s\n", hex.EncodeToString(key))
			if len(value) <= 64 {
				fmt.Printf("  Value: %s\n", hex.EncodeToString(value))
			} else {
				fmt.Printf("  Value: %d bytes\n", len(value))
			}
		}
		iter.Close()
	}
}