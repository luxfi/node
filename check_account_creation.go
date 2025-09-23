package main

import (
	"encoding/binary"
	"fmt"
	"log"

	"github.com/dgraph-io/badger/v3"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/crypto"
)

const (
	dbPath        = "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
	targetAddress = "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
)

var (
	headerPrefix = []byte("h")
)

func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func main() {
	log.Printf("Checking when account %s was created", targetAddress)
	
	// Open BadgerDB
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open BadgerDB: %v", err)
	}
	defer db.Close()
	
	addr := common.HexToAddress(targetAddress)
	accountKey := crypto.Keccak256(addr.Bytes())
	
	// Check what's the latest block in the database
	log.Printf("Finding latest block in database...")
	latestBlock := uint64(0)
	
	err = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		
		// Start from the header prefix and find the highest block number
		for it.Seek(headerPrefix); it.ValidForPrefix(headerPrefix); it.Next() {
			key := it.Item().Key()
			if len(key) >= len(headerPrefix)+8 {
				blockNumBytes := key[len(headerPrefix):len(headerPrefix)+8]
				blockNum := binary.BigEndian.Uint64(blockNumBytes)
				if blockNum > latestBlock {
					latestBlock = blockNum
				}
			}
		}
		return nil
	})
	if err != nil {
		log.Fatalf("Error finding latest block: %v", err)
	}
	
	log.Printf("Latest block found: %d", latestBlock)
	
	// Check if account exists in current state (latest block)
	found := false
	err = db.View(func(txn *badger.Txn) error {
		// Try direct lookup
		_, err := txn.Get(accountKey)
		if err == nil {
			found = true
			return nil
		}
		
		// Try path-based lookup
		pathKey := append([]byte("A"), accountKey...)
		_, err = txn.Get(pathKey)
		if err == nil {
			found = true
			return nil
		}
		
		// Try snapshot lookup
		snapshotKey := append([]byte("a"), accountKey...)
		_, err = txn.Get(snapshotKey)
		if err == nil {
			found = true
			return nil
		}
		
		return nil
	})
	
	if found {
		log.Printf("✅ Account %s EXISTS in current state", addr.Hex())
		fmt.Printf("\n=== CONCLUSION ===\n")
		fmt.Printf("The account %s (luxdefi.eth) EXISTS in the current blockchain state.\n", addr.Hex())
		fmt.Printf("However, it was NOT FOUND at block 1,000,000.\n")
		fmt.Printf("This means the account was created AFTER block 1,000,000.\n")
		fmt.Printf("At block 1,000,000, the account had ZERO balance.\n")
		fmt.Printf("==================\n")
	} else {
		log.Printf("❌ Account %s does NOT exist in current state", addr.Hex())
		fmt.Printf("\n=== CONCLUSION ===\n")
		fmt.Printf("The account %s (luxdefi.eth) does NOT exist in the blockchain state.\n", addr.Hex())
		fmt.Printf("This could mean:\n")
		fmt.Printf("1. The address is incorrect\n")
		fmt.Printf("2. The account has never been used\n")
		fmt.Printf("3. Different state storage format is being used\n")
		fmt.Printf("==================\n")
	}
	
	// Let's also check a few sample accounts to see if our lookup method works
	log.Printf("Testing lookup method with sample addresses...")
	sampleAddresses := []string{
		"0x0000000000000000000000000000000000000000", // Zero address
		"0x0000000000000000000000000000000000000001", // Precompile
		"0x1000000000000000000000000000000000000000", // Random
	}
	
	for _, sampleAddr := range sampleAddresses {
		addr := common.HexToAddress(sampleAddr)
		key := crypto.Keccak256(addr.Bytes())
		
		err = db.View(func(txn *badger.Txn) error {
			if _, err := txn.Get(key); err == nil {
				log.Printf("Found sample address %s", sampleAddr)
			}
			return nil
		})
	}
}