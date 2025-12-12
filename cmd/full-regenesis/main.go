// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Full regenesis with direct writes (no batching)
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/ethdb/pebble"
)

func main() {
	home, _ := os.UserHomeDir()
	source := filepath.Join(home, "work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb")
	target := filepath.Join(home, ".lux/db/C-full")

	fmt.Println("FULL REGENESIS - Direct writes (no batching)")
	fmt.Printf("Source: %s\n", source)
	fmt.Printf("Target: %s\n\n", target)

	src, _ := pebble.New(source, 0, 0, "", true)
	defer src.Close()

	os.RemoveAll(target)
	os.MkdirAll(target, 0755)

	tgt, _ := badgerdb.New(target, 0, 0, "", false)
	defer tgt.Close()

	namespace := common.FromHex("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")
	nsLen := len(namespace)

	start := time.Now()
	count := 0
	blocks := 0
	processed := make(map[uint64]bool)

	iter := src.NewIterator(nil, nil)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		val := iter.Value()

		// Strip namespace
		var newKey []byte
		if len(key) >= nsLen && hasPrefix(key, namespace) {
			newKey = key[nsLen:]
		} else {
			newKey = key
		}

		// Direct write (no batching)
		if err := tgt.Put(newKey, val); err != nil {
			fmt.Printf("Error at key %d: %v\n", count, err)
			break
		}

		count++

		if isHdr, num, _ := isHeaderKey(newKey); isHdr && !processed[num] {
			blocks++
			processed[num] = true
			if blocks%10000 == 0 {
				elapsed := time.Since(start)
				fmt.Printf("Block %d (%d keys, %ds, %.0f keys/sec)\n",
					num, count, int(elapsed.Seconds()), float64(count)/elapsed.Seconds())
			}
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("\n✅ Complete: %d keys, %d blocks in %s (%.0f keys/sec)\n",
		count, blocks, elapsed.Round(time.Second), float64(count)/elapsed.Seconds())
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
