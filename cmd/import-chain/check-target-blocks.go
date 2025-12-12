// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"log"
	
	"github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
)

func main() {
	opts := badger.DefaultOptions("/Users/z/.luxd/db-imported-cchain")
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Check the target blocks from JSONL (last few heights)
	targetBlocks := []uint64{1082773, 1082777, 1082778, 1082779, 1082780}
	
	fmt.Println("=== Checking Final Blocks from Export ===")
	for _, height := range targetBlocks {
		key := make([]byte, 10)
		key[0] = 'h'
		binary.BigEndian.PutUint64(key[1:9], height)
		key[9] = 'n'
		
		err := db.View(func(txn *badger.Txn) error {
			item, err := txn.Get(key)
			if err != nil {
				return err
			}
			
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			
			hash := common.BytesToHash(val)
			fmt.Printf("✅ Block %d: hash=%s\n", height, hash.Hex())
			return nil
		})
		
		if err != nil {
			fmt.Printf("❌ Block %d: NOT FOUND (%v)\n", height, err)
		}
	}
}
