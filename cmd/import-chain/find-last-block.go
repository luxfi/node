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

	// Check last few blocks
	testBlocks := []uint64{465600, 465601, 465602, 465603}
	
	for _, height := range testBlocks {
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
			fmt.Printf("❌ Block %d: NOT FOUND\n", height)
		}
	}
}
