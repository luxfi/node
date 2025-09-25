package main

import (
	"encoding/binary"
	"fmt"
	"log"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
)

func main() {
	// Open BadgerDB directly
	opts := badger.DefaultOptions("/home/z/.luxd/chainData/C/db/badgerdb/ethdb")
	opts = opts.WithReadOnly(true)
	opts = opts.WithLogger(nil) // Disable logs
	
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Printf("Successfully opened BadgerDB\n")

	// Use a read transaction to scan
	err = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		keyCount := 0
		keysByPrefix := make(map[byte]int)
		
		fmt.Printf("Scanning keys...\n")
		
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()
			keyCount++
			
			if len(key) > 0 {
				keysByPrefix[key[0]]++
			}
			
			// Sample first 50 keys
			if keyCount <= 50 {
				err := item.Value(func(val []byte) error {
					fmt.Printf("Key %d: %x (len=%d), Value len=%d\n", keyCount, key, len(key), len(val))
					
					// Check for potential canonical hash
					if len(key) == 9 && key[0] == 'H' {
						blockNum := binary.BigEndian.Uint64(key[1:])
						if len(val) == 32 {
							var hash common.Hash
							copy(hash[:], val)
							fmt.Printf("  -> Canonical hash for block %d: %s\n", blockNum, hash.Hex())
						}
					}
					
					// Check for Height key
					if string(key) == "Height" && len(val) == 8 {
						height := binary.BigEndian.Uint64(val)
						fmt.Printf("  -> Height: %d\n", height)
					}
					
					// Check for LastBlock key
					if string(key) == "LastBlock" && len(val) == 32 {
						var hash common.Hash
						copy(hash[:], val)
						fmt.Printf("  -> LastBlock: %s\n", hash.Hex())
					}
					
					return nil
				})
				if err != nil {
					fmt.Printf("Error reading value: %v\n", err)
				}
			}
			
			if keyCount%100000 == 0 {
				fmt.Printf("Scanned %d keys...\n", keyCount)
			}
			
			// Limit scan to avoid too much output
			if keyCount >= 1000000 {
				fmt.Printf("Stopping at 1M keys\n")
				break
			}
		}
		
		fmt.Printf("\nTotal keys: %d\n", keyCount)
		fmt.Printf("Key prefixes:\n")
		for prefix, count := range keysByPrefix {
			if prefix >= 32 && prefix <= 126 {
				fmt.Printf("'%c' (0x%02x): %d keys\n", prefix, prefix, count)
			} else {
				fmt.Printf("0x%02x: %d keys\n", prefix, count)
			}
		}
		
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
	
	// Now check for specific keys
	fmt.Printf("\nChecking specific keys...\n")
	
	err = db.View(func(txn *badger.Txn) error {
		// Check Height
		item, err := txn.Get([]byte("Height"))
		if err == nil {
			err = item.Value(func(val []byte) error {
				if len(val) == 8 {
					height := binary.BigEndian.Uint64(val)
					fmt.Printf("Height: %d\n", height)
				}
				return nil
			})
		} else {
			fmt.Printf("Height key not found: %v\n", err)
		}
		
		// Check LastBlock
		item, err = txn.Get([]byte("LastBlock"))
		if err == nil {
			err = item.Value(func(val []byte) error {
				if len(val) == 32 {
					var hash common.Hash
					copy(hash[:], val)
					fmt.Printf("LastBlock: %s\n", hash.Hex())
				}
				return nil
			})
		} else {
			fmt.Printf("LastBlock key not found: %v\n", err)
		}
		
		// Check for genesis block (block 0)
		key := make([]byte, 9)
		key[0] = 'H'
		binary.BigEndian.PutUint64(key[1:], 0)
		
		item, err = txn.Get(key)
		if err == nil {
			err = item.Value(func(val []byte) error {
				if len(val) == 32 {
					var hash common.Hash
					copy(hash[:], val)
					fmt.Printf("Genesis block hash: %s\n", hash.Hex())
				}
				return nil
			})
		} else {
			fmt.Printf("Genesis block not found: %v\n", err)
		}
		
		return nil
	})
	
	if err != nil {
		log.Fatal(err)
	}
}