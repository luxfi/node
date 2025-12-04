package main

import (
	"fmt"
	"os"
	
	"github.com/cockroachdb/pebble"
)

func main() {
	dbPath := "/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
	
	fmt.Printf("=== Scanning PebbleDB Structure ===\n")
	fmt.Printf("Path: %s\n\n", dbPath)
	
	opts := &pebble.Options{
		ReadOnly: true,
	}
	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	
	// Count different key types by prefix
	prefixCounts := make(map[byte]int)
	var total int
	
	iter, _ := db.NewIter(nil)
	defer iter.Close()
	
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		if len(key) > 0 {
			prefixCounts[key[0]]++
			total++
		}
		
		if total%100000 == 0 {
			fmt.Printf("Scanned: %d entries\n", total)
		}
	}
	
	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Total entries: %d\n\n", total)
	fmt.Printf("By prefix:\n")
	for prefix, count := range prefixCounts {
		fmt.Printf("  0x%02x ('%c'): %d entries\n", prefix, prefix, count)
	}
	
	fmt.Printf("\nℹ️  To export complete state, we need an RPC-based approach\n")
	fmt.Printf("ℹ️  Direct database export only captures blocks, not account state\n")
}
