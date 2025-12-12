// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/dgraph-io/badger/v4"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <badgerdb-path>\n", os.Args[0])
		os.Exit(1)
	}

	dbPath := os.Args[1]
	fmt.Printf("Opening BadgerDB at: %s\n", dbPath)

	opts := badger.DefaultOptions(dbPath)
	opts.ReadOnly = true
	opts.Logger = nil // Suppress logs

	db, err := badger.Open(opts)
	if err != nil {
		fmt.Printf("Failed to open BadgerDB: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Println("✅ Successfully opened BadgerDB!")
	fmt.Println("\nScanning first 20 keys...")

	count := 0
	err = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid() && count < 20; it.Next() {
			item := it.Item()
			key := item.Key()

			fmt.Printf("\nKey %d:\n", count)
			fmt.Printf("  Hex: %s\n", hex.EncodeToString(key))
			fmt.Printf("  Len: %d bytes\n", len(key))

			if len(key) > 0 {
				fmt.Printf("  First byte: 0x%02x ('%c')\n", key[0], key[0])
			}

			err := item.Value(func(val []byte) error {
				fmt.Printf("  Value len: %d bytes\n", len(val))
				if len(val) > 32 {
					fmt.Printf("  Value preview: %s...\n", hex.EncodeToString(val[:32]))
				}
				return nil
			})
			if err != nil {
				fmt.Printf("  Error reading value: %v\n", err)
			}

			count++
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error scanning database: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Scan complete! Found %d keys\n", count)
}
