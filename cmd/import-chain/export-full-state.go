package main

import (
	"encoding/json"
	"fmt"
	"os"
	
	"github.com/cockroachdb/pebble"
)

type FullExport struct {
	Blocks   []BlockData   `json:"blocks"`
	Accounts []AccountData `json:"accounts"`
	Storage  []StorageData `json:"storage"`
	Code     []CodeData    `json:"code"`
}

type BlockData struct {
	Height   uint64 `json:"height"`
	Hash     string `json:"hash"`
	Header   string `json:"header"`
	Body     string `json:"body"`
	Receipts string `json:"receipts"`
}

type AccountData struct {
	Address  string `json:"address"`
	Nonce    uint64 `json:"nonce"`
	Balance  string `json:"balance"`
	CodeHash string `json:"codeHash"`
	Root     string `json:"root"`
}

type StorageData struct {
	Address string `json:"address"`
	Key     string `json:"key"`
	Value   string `json:"value"`
}

type CodeData struct {
	CodeHash string `json:"codeHash"`
	Code     string `json:"code"`
}

func main() {
	dbPath := "/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
	outputPath := "/tmp/lux-mainnet-96369-full-state.json"
	
	fmt.Printf("=== Full State Export ===\n")
	fmt.Printf("Source: %s\n", dbPath)
	fmt.Printf("Output: %s\n\n", outputPath)
	
	opts := &pebble.Options{
		ReadOnly: true,
	}
	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	
	export := &FullExport{
		Blocks:   make([]BlockData, 0),
		Accounts: make([]AccountData, 0),
		Storage:  make([]StorageData, 0),
		Code:     make([]CodeData, 0),
	}
	
	// Scan all keys to categorize data
	iter := db.NewIter(nil)
	defer iter.Close()
	
	var (
		blockCount   int
		accountCount int
		storageCount int
		codeCount    int
	)
	
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		
		// Categorize by key prefix
		if len(key) == 0 {
			continue
		}
		
		prefix := key[0]
		switch prefix {
		case 'h', 'H', 'b', 'B', 'r', 'R', 'n', 'N':
			// Block data (header, body, receipts, number)
			blockCount++
		case 's', 'S':
			// Account state
			accountCount++
		case 'c', 'C':
			// Contract code
			codeCount++
		default:
			// Storage or other
			storageCount++
		}
		
		if (blockCount+accountCount+storageCount+codeCount)%10000 == 0 {
			fmt.Printf("Scanned: blocks=%d accounts=%d storage=%d code=%d\n", 
				blockCount, accountCount, storageCount, codeCount)
		}
	}
	
	fmt.Printf("\n=== Export Summary ===\n")
	fmt.Printf("Block entries: %d\n", blockCount)
	fmt.Printf("Account entries: %d\n", accountCount)
	fmt.Printf("Storage entries: %d\n", storageCount)
	fmt.Printf("Code entries: %d\n", codeCount)
	fmt.Printf("Total entries: %d\n", blockCount+accountCount+storageCount+codeCount)
	
	fmt.Printf("\nℹ️  Full state export requires traversing the state trie.\n")
	fmt.Printf("ℹ️  This is a complex operation that should be done via RPC or state dump.\n")
}
