// Initialize C-Chain blockchain from migrated snapshot
package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/ethdb/badgerdb"
)

func main() {
	dbPath := os.ExpandEnv("$HOME/.lux/db/C")

	fmt.Println("Initializing C-Chain from snapshot...")
	fmt.Printf("Database: %s\n\n", dbPath)

	db, err := badgerdb.New(dbPath, 0, 0, "", false)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Find highest block
	highest := uint64(0)
	var highestHash common.Hash

	iter := db.NewIterator(nil, nil)
	for iter.Next() {
		key := iter.Key()
		// Header keys: 'h' + 8 bytes + 32 bytes = 41
		if len(key) == 41 && key[0] == 'h' {
			num := binary.BigEndian.Uint64(key[1:9])
			if num > highest && num < 10000000 { // Sanity check
				highest = num
				highestHash = common.BytesToHash(key[9:41])
			}
		}
	}
	iter.Release()

	if highest == 0 {
		fmt.Println("❌ No blocks found in database")
		return
	}

	fmt.Printf("Found highest block: %d\n", highest)
	fmt.Printf("Block hash: %s\n\n", highestHash.Hex())

	// Write head block pointers
	fmt.Println("Writing head block metadata...")
	rawdb.WriteHeadBlockHash(db, highestHash)
	rawdb.WriteHeadHeaderHash(db, highestHash)
	rawdb.WriteHeadFastBlockHash(db, highestHash)

	// Write canonical hash
	canonKey := make([]byte, 10)
	canonKey[0] = 'h'
	binary.BigEndian.PutUint64(canonKey[1:9], highest)
	canonKey[9] = 'n'
	db.Put(canonKey, highestHash.Bytes())

	fmt.Println("✅ Head block metadata written")
	fmt.Printf("\nDatabase initialized at block %d\n", highest)
	fmt.Println("Ready to start node!")
}
