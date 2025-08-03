// Helper to verify and debug chaindata reading
package cchainvm

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/luxfi/database/pebbledb"
)

// VerifyChaindata checks if we can read the chaindata properly
func VerifyChaindata(path string) error {
	fmt.Printf("=== Verifying SubnetEVM chaindata at: %s ===\n", path)
	
	// Open the database
	db, err := pebbledb.New(path, 256, 256, "verify", true)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()
	
	// 1. Check for hash-to-number mappings (H prefix = 0x48)
	fmt.Println("\n1. Checking hash-to-number mappings (0x48 prefix)...")
	hashToNumCount := 0
	highestBlock := uint64(0)
	var sampleHash common.Hash
	
	iter := db.NewIteratorWithStartAndPrefix([]byte{0x48}, nil)
	for iter.Next() && hashToNumCount < 10 {
		key := iter.Key()
		val := iter.Value()
		
		if len(key) == 33 && len(val) == 8 {
			copy(sampleHash[:], key[1:])
			blockNum := binary.BigEndian.Uint64(val)
			fmt.Printf("   Hash %s -> Block %d\n", hex.EncodeToString(key[1:9]), blockNum)
			
			if blockNum > highestBlock {
				highestBlock = blockNum
			}
			hashToNumCount++
		}
	}
	iter.Release()
	fmt.Printf("   Total found: %d+, Highest block: %d\n", hashToNumCount, highestBlock)
	
	// 2. Check for headers (h prefix = 0x68)
	fmt.Println("\n2. Checking headers (0x68 prefix)...")
	headerCount := 0
	
	// Try to read a header using the sample hash
	if highestBlock > 0 && sampleHash != (common.Hash{}) {
		// Try block 1 first
		for blockNum := uint64(1); blockNum <= 5 && blockNum <= highestBlock; blockNum++ {
			// Need to find the hash for this block number first
			// Scan hash-to-number to find it
			iter := db.NewIteratorWithStartAndPrefix([]byte{0x48}, nil)
			var blockHash common.Hash
			found := false
			
			for iter.Next() {
				key := iter.Key()
				val := iter.Value()
				if len(val) == 8 {
					num := binary.BigEndian.Uint64(val)
					if num == blockNum {
						copy(blockHash[:], key[1:])
						found = true
						break
					}
				}
			}
			iter.Release()
			
			if found {
				// Try to read the header
				headerKey := make([]byte, 41)
				headerKey[0] = 0x68 // 'h'
				binary.BigEndian.PutUint64(headerKey[1:9], blockNum)
				copy(headerKey[9:], blockHash[:])
				
				headerData, err := db.Get(headerKey)
				if err == nil {
					fmt.Printf("   Block %d: Header found (%d bytes)\n", blockNum, len(headerData))
					headerCount++
				} else {
					fmt.Printf("   Block %d: No header (key: %x)\n", blockNum, headerKey[:16])
				}
			}
		}
	}
	
	// 3. Check for canonical chain keys (LastHeader, LastBlock)
	fmt.Println("\n3. Checking canonical chain markers...")
	if lastHeader, err := db.Get([]byte("LastHeader")); err == nil {
		fmt.Printf("   LastHeader: %x\n", lastHeader)
	}
	if lastBlock, err := db.Get([]byte("LastBlock")); err == nil {
		fmt.Printf("   LastBlock: %x\n", lastBlock)
	}
	
	// 4. Scan for other key patterns
	fmt.Println("\n4. Scanning key patterns (first 20 keys)...")
	iter = db.NewIterator()
	count := 0
	keyPatterns := make(map[byte]int)
	
	for iter.Next() && count < 20 {
		key := iter.Key()
		if len(key) > 0 {
			keyPatterns[key[0]]++
			
			keyStr := hex.EncodeToString(key)
			if len(keyStr) > 40 {
				keyStr = keyStr[:40] + "..."
			}
			fmt.Printf("   Key[%d]: %s (prefix: 0x%02x)\n", count, keyStr, key[0])
		}
		count++
	}
	iter.Release()
	
	// 5. Summary of key patterns
	fmt.Println("\n5. Key prefix summary:")
	for prefix, count := range keyPatterns {
		fmt.Printf("   0x%02x: %d keys\n", prefix, count)
	}
	
	return nil
}