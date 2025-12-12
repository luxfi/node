// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/cockroachdb/pebble"
)

func main() {
	var (
		srcPath   = flag.String("source", "", "Source PebbleDB path (old subnet)")
		maxHeight = flag.Uint64("max", 1082780, "Maximum block height to import")
		jsonlOut  = flag.String("output", "blocks.jsonl", "Output JSONL file")
	)
	flag.Parse()

	if *srcPath == "" {
		flag.Usage()
		log.Fatal("--source is required")
	}

	fmt.Println("=== Block Export to JSONL ===")
	fmt.Printf("Source: %s\n", *srcPath)
	fmt.Printf("Output: %s\n", *jsonlOut)
	fmt.Printf("Max Height: %d\n\n", *maxHeight)

	// Open source PebbleDB
	srcDB, err := pebble.Open(*srcPath, &pebble.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("Failed to open source DB: %v", err)
	}
	defer srcDB.Close()

	// Create output file
	outFile, err := os.Create(*jsonlOut)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer outFile.Close()

	// SubnetEVM namespace (the 32-byte blockchain ID prefix)
	namespace, _ := hex.DecodeString("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")

	fmt.Println("Scanning for blocks...")
	exported := 0

	// Iterate through blocks by height
	for blockNum := uint64(0); blockNum <= *maxHeight; blockNum++ {
		// Find header for this block number
		prefix := append([]byte(nil), namespace...)
		prefix = append(prefix, 'h')
		numBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(numBytes, blockNum)
		prefix = append(prefix, numBytes...)

		// Scan for header with this block number
		iter, _ := srcDB.NewIter(&pebble.IterOptions{
			LowerBound: prefix,
			UpperBound: append(prefix[:41], 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF),
		})

		if !iter.First() || !iter.Valid() {
			iter.Close()
			continue
		}

		key := iter.Key()
		if len(key) != 73 {
			iter.Close()
			continue
		}

		blockHash := key[41:73]
		headerRLP := make([]byte, len(iter.Value()))
		copy(headerRLP, iter.Value())
		iter.Close()

		// Read body
		bodyKey := append([]byte(nil), namespace...)
		bodyKey = append(bodyKey, 'b')
		bodyKey = append(bodyKey, numBytes...)
		bodyKey = append(bodyKey, blockHash...)

		var bodyRLP []byte
		if bodyValue, closer, err := srcDB.Get(bodyKey); err == nil {
			bodyRLP = make([]byte, len(bodyValue))
			copy(bodyRLP, bodyValue)
			closer.Close()
		}

		// Read receipts
		receiptsKey := append([]byte(nil), namespace...)
		receiptsKey = append(receiptsKey, 'r')
		receiptsKey = append(receiptsKey, numBytes...)
		receiptsKey = append(receiptsKey, blockHash...)

		var receiptsRLP []byte
		if receiptsValue, closer, err := srcDB.Get(receiptsKey); err == nil {
			receiptsRLP = make([]byte, len(receiptsValue))
			copy(receiptsRLP, receiptsValue)
			closer.Close()
		}

		// Write JSONL entry with raw RLP bytes
		fmt.Fprintf(outFile, "{\"height\":%d,\"hash\":\"0x%x\",\"header\":\"0x%x\",\"body\":\"0x%x\",\"receipts\":\"0x%x\"}\n",
			blockNum, blockHash, headerRLP, bodyRLP, receiptsRLP)

		exported++
		if exported%1000 == 0 {
			fmt.Printf("Exported %d blocks...\n", exported)
		}
	}

	fmt.Printf("\n=== Export Complete ===\n")
	fmt.Printf("Exported %d blocks to %s\n", exported, *jsonlOut)
}
