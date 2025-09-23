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

// headerHashKey = headerPrefix + num (uint64 big endian) + headerHashSuffix
func headerHashKey(number uint64) []byte {
	return append(append(headerPrefix, encodeBlockNumber(number)...), headerHashSuffix...)
}

// headerKey = headerPrefix + num (uint64 big endian) + hash
func headerKey(number uint64, hash common.Hash) []byte {
	return append(append(headerPrefix, encodeBlockNumber(number)...), hash.Bytes()...)
}

func main() {
	log.Printf("Debugging database keys for block %d", targetBlock)
	
	// Open BadgerDB
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open BadgerDB: %v", err)
	}
	defer db.Close()
	
	// Read canonical hash for block 1,000,000
	hashKey := headerHashKey(targetBlock)
	log.Printf("Looking for hash key: %x", hashKey)
	
	var canonicalHashBytes []byte
	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(hashKey)
		if err != nil {
			return err
		}
		canonicalHashBytes, err = item.ValueCopy(nil)
		return err
	})
	if err != nil {
		log.Fatalf("No canonical hash found for block %d: %v", targetBlock, err)
	}
	
	canonicalHash := common.BytesToHash(canonicalHashBytes)
	log.Printf("Canonical hash for block %d: %s", targetBlock, canonicalHash.Hex())
	
	// Try to find header with different key patterns
	headerKey1 := headerKey(targetBlock, canonicalHash)
	log.Printf("Looking for header key 1: %x", headerKey1)
	
	// Check if header exists
	err = db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(headerKey1)
		return err
	})
	if err != nil {
		log.Printf("Header key 1 not found: %v", err)
	} else {
		log.Printf("Header key 1 found!")
	}
	
	// Let's list all keys that start with "h" + block number
	blockPrefix := append(headerPrefix, encodeBlockNumber(targetBlock)...)
	log.Printf("Looking for keys with prefix: %x", blockPrefix)
	
	count := 0
	err = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		
		for it.Seek(blockPrefix); it.ValidForPrefix(blockPrefix); it.Next() {
			item := it.Item()
			key := item.Key()
			log.Printf("Found key: %x", key)
			count++
			if count > 10 {
				log.Printf("... truncated (found more than 10 keys)")
				break
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("Error iterating: %v", err)
	}
	
	if count == 0 {
		log.Printf("No keys found with prefix %x", blockPrefix)
		
		// Let's check some nearby blocks
		for i := targetBlock - 2; i <= targetBlock + 2; i++ {
			nearbyPrefix := append(headerPrefix, encodeBlockNumber(uint64(i))...)
			log.Printf("Checking block %d with prefix: %x", i, nearbyPrefix)
			
			err = db.View(func(txn *badger.Txn) error {
				opts := badger.DefaultIteratorOptions
				opts.PrefetchValues = false
				it := txn.NewIterator(opts)
				defer it.Close()
				
				if it.Seek(nearbyPrefix); it.ValidForPrefix(nearbyPrefix) {
					key := it.Item().Key()
					log.Printf("Found nearby key for block %d: %x", i, key)
				}
				return nil
			})
		}
	}
}