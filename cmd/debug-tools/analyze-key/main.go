// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"log"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
)

func main() {
	// Open BadgerDB
	opts := badger.DefaultOptions("/home/z/.luxd/chainData/C/db/badgerdb/ethdb")
	opts = opts.WithReadOnly(true)
	opts = opts.WithLogger(nil)
	
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Printf("Analyzing key format in BadgerDB\n")

	err = db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		
		// Look at H keys
		prefix := []byte("H")
		count := 0
		
		fmt.Printf("Analyzing 'H' prefixed keys:\n")
		for it.Seek(prefix); it.ValidForPrefix(prefix) && count < 20; it.Next() {
			key := it.Item().Key()
			
			err := it.Item().Value(func(val []byte) error {
				fmt.Printf("\nKey %d:\n", count+1)
				fmt.Printf("  Key: %x (len=%d)\n", key, len(key))
				fmt.Printf("  Value: %x (len=%d)\n", val, len(val))
				
				// Analyze key structure
				if len(key) == 33 && key[0] == 'H' {
					hash := key[1:]
					fmt.Printf("  Hash: %s\n", common.BytesToHash(hash).Hex())
				}
				
				// Analyze value structure
				if len(val) >= 4 {
					// Try interpreting as various formats
					if len(val) == 4 {
						blockNum := binary.BigEndian.Uint32(val)
						fmt.Printf("  As uint32: %d\n", blockNum)
					}
					if len(val) >= 8 {
						blockNum := binary.BigEndian.Uint64(val)
						fmt.Printf("  As uint64: %d\n", blockNum)
						
						// Try last 8 bytes
						if len(val) > 8 {
							blockNum = binary.BigEndian.Uint64(val[len(val)-8:])
							fmt.Printf("  Last 8 bytes as uint64: %d\n", blockNum)
						}
						
						// Try last 4 bytes
						if len(val) >= 4 {
							blockNum32 := binary.BigEndian.Uint32(val[len(val)-4:])
							fmt.Printf("  Last 4 bytes as uint32: %d\n", blockNum32)
						}
					}
				}
				
				return nil
			})
			if err != nil {
				fmt.Printf("Error reading value: %v\n", err)
			}
			count++
		}
		
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
	
	// Now let's check what the geth wrapper expects
	fmt.Printf("\nChecking what rawdb format expects...\n")
	fmt.Printf("Standard geth canonical hash key format:\n")
	fmt.Printf("- Key: 'H' + 8-byte block number = 9 bytes total\n")
	fmt.Printf("- Value: 32-byte hash\n")
	
	// Check if we have any 9-byte H keys
	err = db.View(func(txn *badger.Txn) error {
		// Try some standard block numbers
		for i := uint64(0); i <= 10; i++ {
			key := make([]byte, 9)
			key[0] = 'H'
			binary.BigEndian.PutUint64(key[1:], i)
			
			item, err := txn.Get(key)
			if err == nil {
				err = item.Value(func(val []byte) error {
					fmt.Printf("Block %d canonical hash: %x\n", i, val)
					return nil
				})
				if err != nil {
					fmt.Printf("Error reading block %d: %v\n", i, err)
				}
			} else {
				fmt.Printf("Block %d not found: %v\n", i, err)
			}
		}
		
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
}