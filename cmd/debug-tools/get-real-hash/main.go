// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"log"

	"github.com/dgraph-io/badger/v3"
	"github.com/luxfi/geth/common"
)

const (
	targetBlock = 1000000 // 0xF4240
	dbPath      = "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
)

// Database key prefixes from geth rawdb schema
var (
	headerPrefix       = []byte("h") // headerPrefix + num (uint64 big endian) + hash -> header
	headerHashSuffix   = []byte("n") // headerPrefix + num (uint64 big endian) + headerHashSuffix -> hash
)

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func main() {
	log.Printf("Finding actual header hash for block %d", targetBlock)
	
	// Open BadgerDB
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open BadgerDB: %v", err)
	}
	defer db.Close()
	
	// Look for all keys with the block prefix
	blockPrefix := append(headerPrefix, encodeBlockNumber(targetBlock)...)
	log.Printf("Looking for keys with prefix: %x", blockPrefix)
	
	err = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		
		for it.Seek(blockPrefix); it.ValidForPrefix(blockPrefix); it.Next() {
			item := it.Item()
			key := item.Key()
			log.Printf("Found key: %x", key)
			
			// Extract the hash from the key
			if len(key) == len(blockPrefix) + 32 { // hash is 32 bytes
				hashBytes := key[len(blockPrefix):]
				hash := common.BytesToHash(hashBytes)
				log.Printf("Extracted hash: %s", hash.Hex())
				
				// This must be the actual hash for the block
				// Let's verify by reading the header
				var headerData []byte
				headerItem, err := txn.Get(key)
				if err != nil {
					log.Printf("Could not read header data: %v", err)
					continue
				}
				headerData, err = headerItem.ValueCopy(nil)
				if err != nil {
					log.Printf("Could not copy header data: %v", err)
					continue
				}
				
				log.Printf("Found header data with %d bytes for hash %s", len(headerData), hash.Hex())
				log.Printf("Use this hash: %s", hash.Hex())
				return nil
			} else if len(key) == len(blockPrefix) + 1 && key[len(key)-1] == headerHashSuffix[0] {
				// This is the canonical hash key
				log.Printf("This is the canonical hash key")
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("Error iterating: %v", err)
	}
}