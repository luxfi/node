package main

import (
	"encoding/binary"
	"fmt"
	"log"
	
	"github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
)

func main() {
	// Open the imported database
	opts := badger.DefaultOptions("/Users/z/.luxd/db-imported-cchain")
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Check specific blocks
	blocksToCheck := []uint64{0, 1, 100, 1000, 10000, 100000, 465602}
	
	for _, height := range blocksToCheck {
		// Construct canonical hash key: "h" + height (as big endian uint64) + "n"
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
			fmt.Printf("Block %d: hash=%s\n", height, hash.Hex())
			return nil
		})
		
		if err != nil {
			fmt.Printf("Block %d: NOT FOUND (%v)\n", height, err)
		}
	}
}
