// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"log"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/rlp"
)

func main() {
	// Open BadgerDB
	opts := badger.DefaultOptions("/home/z/.luxd/chainData/C/db/badgerdb/ethdb")
	opts = opts.WithLogger(nil)
	
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Printf("Opened BadgerDB successfully\n")

	err = db.Update(func(txn *badger.Txn) error {
		// First, get the LastBlock hash
		item, err := txn.Get([]byte("LastBlock"))
		if err != nil {
			return fmt.Errorf("LastBlock not found: %v", err)
		}
		
		var lastBlockHash common.Hash
		err = item.Value(func(val []byte) error {
			copy(lastBlockHash[:], val)
			return nil
		})
		if err != nil {
			return err
		}
		
		fmt.Printf("LastBlock hash: %s\n", lastBlockHash.Hex())
		
		// Look for this hash in the H keys to find the block number
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		
		var highestBlock uint64 = 0
		var highestHash common.Hash
		blocksFound := 0
		
		// Scan H keys to find the highest block number
		prefix := []byte("H")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := it.Item().Key()
			
			// H keys should be 33 bytes: 'H' + 32-byte hash
			if len(key) == 33 && key[0] == 'H' {
				var blockHash common.Hash
				copy(blockHash[:], key[1:])
				
				err := it.Item().Value(func(val []byte) error {
					if len(val) >= 8 {
						// The value should contain the block number
						blockNum := binary.BigEndian.Uint64(val[len(val)-8:])
						if blockNum > highestBlock {
							highestBlock = blockNum
							highestHash = blockHash
						}
						blocksFound++
						
						if blocksFound <= 10 || blocksFound%100000 == 0 {
							fmt.Printf("Block %d: %s\n", blockNum, blockHash.Hex())
						}
					}
					return nil
				})
				if err != nil {
					fmt.Printf("Error reading value: %v\n", err)
				}
			}
		}
		
		fmt.Printf("Found %d blocks, highest: %d\n", blocksFound, highestBlock)
		fmt.Printf("Highest block hash: %s\n", highestHash.Hex())
		
		// Check if we can find the header for block 1 to extract parent hash for genesis
		var block1Hash common.Hash
		found := false
		
		for it.Seek(prefix); it.ValidForPrefix(prefix) && !found; it.Next() {
			key := it.Item().Key()
			if len(key) == 33 && key[0] == 'H' {
				err := it.Item().Value(func(val []byte) error {
					if len(val) >= 8 {
						blockNum := binary.BigEndian.Uint64(val[len(val)-8:])
						if blockNum == 1 {
							copy(block1Hash[:], key[1:])
							found = true
							fmt.Printf("Block 1 hash: %s\n", block1Hash.Hex())
						}
					}
					return nil
				})
				if err != nil {
					return err
				}
			}
		}
		
		if !found {
			return fmt.Errorf("Block 1 not found")
		}
		
		// Try to read header for block 1 to get parent hash
		headerKey := append([]byte("h"), block1Hash[:]...)
		headerKey = append(headerKey, make([]byte, 8)...)
		binary.BigEndian.PutUint64(headerKey[33:], 1)
		
		item, err = txn.Get(headerKey)
		if err != nil {
			fmt.Printf("Header for block 1 not found with key format 1: %v\n", err)
			// Try different key format
			headerKey = append([]byte("h"), block1Hash[:]...)
			item, err = txn.Get(headerKey)
			if err != nil {
				fmt.Printf("Header for block 1 not found with key format 2: %v\n", err)
				return err
			}
		}
		
		var header types.Header
		err = item.Value(func(val []byte) error {
			return rlp.DecodeBytes(val, &header)
		})
		if err != nil {
			return fmt.Errorf("failed to decode header: %v", err)
		}
		
		fmt.Printf("Block 1 parent hash: %s\n", header.ParentHash.Hex())
		
		// Now create genesis block canonical hash mapping
		// The genesis hash should be the parent of block 1
		genesisHash := header.ParentHash
		
		// Create canonical hash mapping for block 0 using standard format
		// Key format: 'H' + 8-byte block number
		genesisKey := make([]byte, 9)
		genesisKey[0] = 'H'
		binary.BigEndian.PutUint64(genesisKey[1:], 0)
		err = txn.Set(genesisKey, genesisHash[:])
		if err != nil {
			return fmt.Errorf("failed to set genesis canonical hash: %v", err)
		}
		
		fmt.Printf("Set genesis (block 0) canonical hash: %s\n", genesisHash.Hex())
		
		// Also set head pointers using rawdb keys
		err = txn.Set([]byte("LastHeader"), lastBlockHash[:])
		if err != nil {
			return err
		}
		
		err = txn.Set([]byte("LastFast"), lastBlockHash[:])
		if err != nil {
			return err
		}
		
		// Convert the highest block number to bytes for Height
		heightBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(heightBytes, highestBlock)
		err = txn.Set([]byte("Height"), heightBytes)
		if err != nil {
			return err
		}
		
		fmt.Printf("Set Height to: %d\n", highestBlock)
		fmt.Printf("Set head pointers to: %s\n", lastBlockHash.Hex())
		
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("Successfully fixed genesis block and head pointers!\n")
}