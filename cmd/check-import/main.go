// Check what was imported and what's missing
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/ethdb/pebble"
)

func main() {
	home, _ := os.UserHomeDir()

	source := filepath.Join(home, "work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb")
	target := filepath.Join(home, ".lux/db/C")

	fmt.Println("Analyzing import...")

	// Open databases
	src, _ := pebble.New(source, 0, 0, "", true)
	defer src.Close()

	tgt, _ := badgerdb.New(target, 0, 0, "", true)
	defer tgt.Close()

	// Count key prefixes in source
	srcPrefixes := make(map[byte]int)
	iter := src.NewIterator(nil, nil)
	for iter.Next() {
		key := iter.Key()
		if len(key) > 32 {
			// Skip namespace
			key = key[32:]
		}
		if len(key) > 0 {
			srcPrefixes[key[0]]++
		}
	}
	iter.Release()

	// Count in target
	tgtPrefixes := make(map[byte]int)
	iter2 := tgt.NewIterator(nil, nil)
	for iter2.Next() {
		key := iter2.Key()
		if len(key) > 0 {
			tgtPrefixes[key[0]]++
		}
	}
	iter2.Release()

	fmt.Println("\nKey Prefix Analysis:")
	fmt.Println("Prefix | Source | Target | Status")
	fmt.Println("-------|--------|--------|-------")

	for prefix := byte('a'); prefix <= 'z'; prefix++ {
		src := srcPrefixes[prefix]
		tgt := tgtPrefixes[prefix]
		status := "✅"
		if src > 0 && tgt == 0 {
			status = "❌ MISSING"
		}
		if src > 0 || tgt > 0 {
			fmt.Printf("  '%c'  | %6d | %6d | %s\n", prefix, src, tgt, status)
		}
	}
}
