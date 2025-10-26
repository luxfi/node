package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/node/vms/cchainvm"
)

func main() {
	// Open the migrated database
	dbPath := "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
	
	badgerConfig := cchainvm.BadgerDatabaseConfig{
		DataDir:       dbPath,
		EnableAncient: false,
		ReadOnly:      true,
	}
	
	db, err := cchainvm.NewBadgerDatabase(nil, badgerConfig)
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	
	fmt.Printf("Successfully opened database at: %s\n", dbPath)
	
	// Check for genesis (block 0)
	genesisHash := rawdb.ReadCanonicalHash(db, 0)
	fmt.Printf("Genesis hash from rawdb: %s\n", genesisHash.Hex())
	
	// Check with direct key access
	key := make([]byte, 9)
	key[0] = 'H'
	binary.BigEndian.PutUint64(key[1:], 0)
	
	if val, err := db.Get(key); err == nil && len(val) == 32 {
		var hash common.Hash
		copy(hash[:], val)
		fmt.Printf("Genesis hash from direct key: %s\n", hash.Hex())
	} else {
		fmt.Printf("Failed to read genesis with direct key: %v\n", err)
	}
	
	// Check head block
	headHash := rawdb.ReadHeadBlockHash(db)
	fmt.Printf("Head block hash from rawdb: %s\n", headHash.Hex())
	
	// Check custom keys
	if val, err := db.Get([]byte("Height")); err == nil && len(val) == 8 {
		height := binary.BigEndian.Uint64(val)
		fmt.Printf("Height from custom key: %d\n", height)
	}
	
	if val, err := db.Get([]byte("LastBlock")); err == nil && len(val) == 32 {
		var hash common.Hash
		copy(hash[:], val)
		fmt.Printf("LastBlock from custom key: %s\n", hash.Hex())
	}
	
	// Check a few canonical hashes
	fmt.Printf("\nChecking canonical hashes...\n")
	for i := uint64(0); i <= 10; i++ {
		hash := rawdb.ReadCanonicalHash(db, i)
		if hash != (common.Hash{}) {
			fmt.Printf("Block %d: %s\n", i, hash.Hex())
		}
	}
	
	// Check the latest blocks
	if val, err := db.Get([]byte("Height")); err == nil && len(val) == 8 {
		height := binary.BigEndian.Uint64(val)
		fmt.Printf("\nChecking blocks around height %d...\n", height)
		
		for i := height; i > height-5 && i <= height; i-- {
			hash := rawdb.ReadCanonicalHash(db, i)
			if hash != (common.Hash{}) {
				fmt.Printf("Block %d: %s\n", i, hash.Hex())
				
				// Try to read the header
				header := rawdb.ReadHeader(db, hash, i)
				if header != nil {
					fmt.Printf("  Header found: number=%d, hash=%s\n", header.Number.Uint64(), header.Hash().Hex())
				} else {
					fmt.Printf("  Header NOT found\n")
				}
			}
		}
	}
}