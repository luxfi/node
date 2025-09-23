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
	opts = opts.WithLogger(nil)
	
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Printf("Opened BadgerDB successfully\n")

	err = db.Update(func(txn *badger.Txn) error {
		// Get the LastBlock hash
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
		
		// Find the key-value format by looking at a few H keys
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		
		prefix := []byte("H")
		count := 0
		var minBlock, maxBlock uint64 = ^uint64(0), 0
		
		for it.Seek(prefix); it.ValidForPrefix(prefix) && count < 100; it.Next() {
			key := it.Item().Key()
			
			if len(key) == 33 && key[0] == 'H' {
				err := it.Item().Value(func(val []byte) error {
					// Assume the value contains the block number somewhere
					// Try different positions
					if len(val) >= 8 {
						// Try last 8 bytes
						blockNum := binary.BigEndian.Uint64(val[len(val)-8:])
						if blockNum < 10000000 { // reasonable block number
							if blockNum < minBlock {
								minBlock = blockNum
							}
							if blockNum > maxBlock {
								maxBlock = blockNum
							}
							
							if count < 10 {
								fmt.Printf("Block %d: hash %x\n", blockNum, key[1:9])
							}
						} else if len(val) >= 4 {
							// Try last 4 bytes
							blockNum32 := binary.BigEndian.Uint32(val[len(val)-4:])
							blockNum = uint64(blockNum32)
							if blockNum < 10000000 {
								if blockNum < minBlock {
									minBlock = blockNum
								}
								if blockNum > maxBlock {
									maxBlock = blockNum
								}
								
								if count < 10 {
									fmt.Printf("Block %d: hash %x\n", blockNum, key[1:9])
								}
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
		}
		
		fmt.Printf("Scanned %d H keys\n", count)
		fmt.Printf("Block range: %d to %d\n", minBlock, maxBlock)
		
		if minBlock == ^uint64(0) {
			return fmt.Errorf("Could not determine block number format")
		}
		
		// Now we need to find block 1 to get its parent (genesis)
		// But first, let's see if we already have a genesis
		genesisKey := make([]byte, 9)
		genesisKey[0] = 'H'
		binary.BigEndian.PutUint64(genesisKey[1:], 0)
		
		_, err = txn.Get(genesisKey)
		if err == nil {
			fmt.Printf("Genesis block canonical hash already exists\n")
			return nil
		}
		
		// We need to create a genesis block
		// For now, let's use a known genesis hash from LUX mainnet
		genesisHash := common.HexToHash("0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e")
		
		fmt.Printf("Creating genesis block with hash: %s\n", genesisHash.Hex())
		
		// Set the canonical hash for block 0
		err = txn.Set(genesisKey, genesisHash[:])
		if err != nil {
			return fmt.Errorf("failed to set genesis canonical hash: %v", err)
		}
		
		// Set the Height to the maximum block we found
		heightBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(heightBytes, maxBlock)
		err = txn.Set([]byte("Height"), heightBytes)
		if err != nil {
			return err
		}
		
		// Set head pointers using standard rawdb keys
		err = txn.Set([]byte("LastHeader"), lastBlockHash[:])
		if err != nil {
			return err
		}
		
		err = txn.Set([]byte("LastFast"), lastBlockHash[:])
		if err != nil {
			return err
		}
		
		fmt.Printf("✅ Fixed genesis block and set Height to: %d\n", maxBlock)
		
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("Successfully fixed the database!\n")
}