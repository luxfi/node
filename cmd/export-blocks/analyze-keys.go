// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
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

	fmt.Println("=== Analyzing SubnetEVM Database Structure ===")
	fmt.Println("Namespace:", hex.EncodeToString(namespace))
	fmt.Println()

	// Look for keys WITHOUT namespace prefix
	fmt.Println("Looking for keys WITHOUT namespace prefix:")
	iter, _ := db.NewIter(nil)
	nonNamespaceCount := 0
	samples := 0
	
	for iter.First(); iter.Valid() && samples < 1000; iter.Next() {
		key := iter.Key()
		
		if !bytes.HasPrefix(key, namespace) {
			nonNamespaceCount++
			if nonNamespaceCount <= 10 {
				fmt.Printf("Key %d: len=%d, hex=%s", nonNamespaceCount, len(key), hex.EncodeToString(key))
				
				// Try to interpret common patterns
				if len(key) == 41 && key[0] == 'h' {
					// Could be header key: 'h' + 8 bytes number + 32 bytes hash
					number := binary.BigEndian.Uint64(key[1:9])
					hash := hex.EncodeToString(key[9:41])
					fmt.Printf(" -> Header: block=%d, hash=%s", number, hash)
				} else if len(key) == 41 && key[0] == 'b' {
					// Could be body key
					number := binary.BigEndian.Uint64(key[1:9])
					hash := hex.EncodeToString(key[9:41])
					fmt.Printf(" -> Body: block=%d, hash=%s", number, hash)
				} else if len(key) == 41 && key[0] == 'r' {
					// Could be receipt key
					number := binary.BigEndian.Uint64(key[1:9])
					hash := hex.EncodeToString(key[9:41])
					fmt.Printf(" -> Receipt: block=%d, hash=%s", number, hash)
				} else if len(key) == 33 && key[0] == 'H' {
					// Hash to number mapping
					hash := hex.EncodeToString(key[1:33])
					value := iter.Value()
					if len(value) == 8 {
						number := binary.BigEndian.Uint64(value)
						fmt.Printf(" -> Hash->Number: hash=%s, number=%d", hash, number)
					}
				}
				fmt.Println()
			}
		}
		samples++
	}
	iter.Close()
	
	fmt.Printf("\nFound %d keys without namespace prefix in first %d keys\n\n", nonNamespaceCount, samples)

	// Now look at keys WITH namespace prefix
	fmt.Println("Analyzing keys WITH namespace prefix:")
	iter, _ = db.NewIter(&pebble.IterOptions{
		LowerBound: namespace,
		UpperBound: append(namespace, 0xFF),
	})
	
	withNamespaceCount := 0
	for iter.First(); iter.Valid() && withNamespaceCount < 20; iter.Next() {
		key := iter.Key()
		withNamespaceCount++
		
		fmt.Printf("Key %d: len=%d\n", withNamespaceCount, len(key))
		
		// Show structure
		if len(key) > 32 {
			afterNamespace := key[32:]
			fmt.Printf("  After namespace: %s\n", hex.EncodeToString(afterNamespace))
			
			// Try to interpret
			if len(afterNamespace) >= 1 {
				keyType := afterNamespace[0]
				fmt.Printf("  Key type byte: 0x%02x ('%c')\n", keyType, keyType)
				
				if len(afterNamespace) >= 9 {
					// Could be number
					possibleNum := binary.BigEndian.Uint64(afterNamespace[1:9])
					fmt.Printf("  Next 8 bytes as uint64: %d\n", possibleNum)
				}
			}
		}
		
		value := iter.Value()
		if len(value) <= 32 {
			fmt.Printf("  Value: %s\n", hex.EncodeToString(value))
		} else {
			fmt.Printf("  Value length: %d bytes\n", len(value))
		}
		fmt.Println()
	}
	iter.Close()
	
	fmt.Printf("Total found: %d keys with namespace prefix\n", withNamespaceCount)
}