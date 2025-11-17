// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// Regenesis - Import lux-mainnet-96369 into C-Chain with quantum finality

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/ethdb/pebble"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	// Source: old lux-mainnet-96369
	source := filepath.Join(home, "work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb")

	// Target: new C-Chain in ~/.lux/db/C
	target := filepath.Join(home, ".lux/db/C")

	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║            REGENESIS - BLOCKCHAIN REBIRTH                    ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📦 Source: %s\n", source)
	fmt.Printf("🎯 Target: %s\n", target)
	fmt.Println()

	// Check source exists
	if _, err := os.Stat(source); os.IsNotExist(err) {
		fmt.Println("❌ Source database not found")
		os.Exit(1)
	}

	// Check if already done
	marker := filepath.Join(target, ".regenesis_done")
	if _, err := os.Stat(marker); err == nil {
		fmt.Println("✅ Regenesis already complete")
		os.Exit(0)
	}

	// Open source (read-only)
	fmt.Println("📖 Opening old net/evm database...")
	srcDB, err := pebble.New(source, 0, 0, "", true)
	if err != nil {
		fmt.Printf("❌ Failed to open source: %v\n", err)
		os.Exit(1)
	}
	defer srcDB.Close()
	fmt.Println("✅ Source opened")
	fmt.Println()

	// Create target
	fmt.Println("📝 Creating C-Chain database...")
	os.MkdirAll(target, 0755)
	tgtDB, err := badgerdb.New(target, 0, 0, "", false)
	if err != nil {
		fmt.Printf("❌ Failed to create target: %v\n", err)
		os.Exit(1)
	}
	defer tgtDB.Close()
	fmt.Println("✅ Target created")
	fmt.Println()

	// Import
	fmt.Println("🌌 Importing into Quasar event horizon...")
	fmt.Println()

	stats := importAll(srcDB, tgtDB)

	// Mark complete
	os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)), 0644)

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  ✅ REGENESIS COMPLETE ✅                     ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📊 Statistics:\n")
	fmt.Printf("   Keys:     %d\n", stats.keys)
	fmt.Printf("   Blocks:   %d\n", stats.blocks)
	fmt.Printf("   Mappings: %d\n", stats.mappings)
	fmt.Printf("   Time:     %s\n", stats.elapsed.Round(time.Second))
	if stats.elapsed.Seconds() > 0 {
		fmt.Printf("   Speed:    %.0f keys/sec\n", float64(stats.keys)/stats.elapsed.Seconds())
	}
	fmt.Println()
	fmt.Println("🌌 Old chain reborn in new C-Chain with quantum finality!")
	fmt.Println()
}

type stats struct {
	keys     uint64
	blocks   uint64
	mappings uint64
	elapsed  time.Duration
}

func importAll(src *pebble.Database, tgtDB ethdb.Database) stats {
	namespace := common.FromHex("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")
	namespaceLen := len(namespace)

	start := time.Now()
	var s stats

	iter := src.NewIterator(nil, nil)
	defer iter.Release()

	batch := tgtDB.NewBatch()
	batchSize := 0
	processed := make(map[uint64]bool)

	for iter.Next() {
		key := iter.Key()
		val := iter.Value()

		// Strip namespace if present
		var newKey []byte
		if len(key) >= namespaceLen && hasPrefix(key, namespace) {
			newKey = key[namespaceLen:]
		} else {
			newKey = key
		}

		// Write ALL keys (not just headers)
		if err := batch.Put(newKey, val); err != nil {
			panic(err)
		}

		s.keys++
		batchSize++

		// Track blocks for progress reporting
		if isHdr, num, _ := isHeaderKey(newKey); isHdr && !processed[num] {
			s.blocks++
			processed[num] = true

			// Progress every 10K blocks
			if s.blocks%10000 == 0 {
				fmt.Printf("  Block %d... (%d keys)\n", num, s.keys)
			}
		}

		// Batch commit every 100 keys (BadgerDB strict limits)
		if batchSize >= 100 {
			if err := batch.Write(); err != nil {
				fmt.Printf("\n❌ Batch write failed at key %d: %v\n", s.keys, err)
				panic(err)
			}
			batch.Reset()
			batchSize = 0

			// Progress indicator
			if s.keys % 100000 == 0 {
				fmt.Printf("  %dM keys written\n", s.keys/1000000)
			}
		}
	}

	// Final batch
	batch.Write()
	s.elapsed = time.Since(start)

	return s
}

func hasPrefix(data, prefix []byte) bool {
	if len(data) < len(prefix) {
		return false
	}
	for i := range prefix {
		if data[i] != prefix[i] {
			return false
		}
	}
	return true
}

func isHeaderKey(key []byte) (bool, uint64, common.Hash) {
	if len(key) != 41 || key[0] != 'h' {
		return false, 0, common.Hash{}
	}
	num := binary.BigEndian.Uint64(key[1:9])
	hash := common.BytesToHash(key[9:41])
	return true, num, hash
}

func canonKey(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return append(append([]byte("h"), b...), 'n')
}
