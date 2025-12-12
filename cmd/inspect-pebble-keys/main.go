// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/cockroachdb/pebble"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: inspect-pebble-keys <pebbledb_path>")
		os.Exit(1)
	}

	dbPath := os.Args[1]
	fmt.Printf("Opening PebbleDB at: %s\n", dbPath)

	opts := &pebble.Options{
		ReadOnly: true,
	}

	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		log.Fatalf("Failed to open PebbleDB: %v", err)
	}
	defer db.Close()

	fmt.Println("✓ Successfully opened PebbleDB")
	fmt.Println()
	fmt.Println("First 50 keys in database:")
	fmt.Println("============================================================")

	// Create iterator
	iter, err := db.NewIter(nil)
	if err != nil {
		log.Fatalf("Failed to create iterator: %v", err)
	}
	defer iter.Close()

	count := 0
	// Map to track key prefixes
	prefixMap := make(map[string]int)
	
	// Iterate through first 50 keys
	for iter.First(); iter.Valid() && count < 50; iter.Next() {
		key := iter.Key()
		val := iter.Value()
		
		// Show key in hex
		keyHex := hex.EncodeToString(key)
		keyLen := len(key)
		valLen := len(val)
		
		// Track prefix (first 4 bytes or less)
		prefixLen := 4
		if len(key) < 4 {
			prefixLen = len(key)
		}
		prefix := hex.EncodeToString(key[:prefixLen])
		prefixMap[prefix]++
		
		// For readability, show ASCII if printable
		keyStr := ""
		isPrintable := true
		for _, b := range key {
			if b >= 32 && b <= 126 {
				keyStr += string(b)
			} else {
				isPrintable = false
				break
			}
		}
		
		if isPrintable && keyStr != "" {
			fmt.Printf("%3d. Key[%d]: %s (ASCII: %s), Value[%d bytes]\n", count+1, keyLen, keyHex[:min(32, len(keyHex))], keyStr[:min(20, len(keyStr))], valLen)
		} else {
			fmt.Printf("%3d. Key[%d]: %s..., Value[%d bytes]\n", count+1, keyLen, keyHex[:min(32, len(keyHex))], valLen)
		}
		
		count++
	}

	// Continue scanning to gather statistics
	fmt.Println("\nScanning rest of database for statistics...")
	for iter.Next() {
		key := iter.Key()
		
		// Track prefix
		prefixLen := 4
		if len(key) < 4 {
			prefixLen = len(key)
		}
		prefix := hex.EncodeToString(key[:prefixLen])
		prefixMap[prefix]++
		
		count++
		if count >= 100000 {
			break // Sample 100k keys
		}
	}

	fmt.Println("\nTop key prefixes (first 4 bytes):")
	fmt.Println("============================================================")
	
	// Find top prefixes
	type prefixCount struct {
		prefix string
		count  int
	}
	
	var prefixes []prefixCount
	for p, c := range prefixMap {
		prefixes = append(prefixes, prefixCount{p, c})
	}
	
	// Sort by count
	for i := 0; i < len(prefixes)-1; i++ {
		for j := i + 1; j < len(prefixes); j++ {
			if prefixes[j].count > prefixes[i].count {
				prefixes[i], prefixes[j] = prefixes[j], prefixes[i]
			}
		}
	}
	
	// Show top 20 prefixes
	for i := 0; i < min(20, len(prefixes)); i++ {
		p := prefixes[i]
		fmt.Printf("  %s: %d occurrences\n", p.prefix, p.count)
	}
	
	fmt.Printf("\nTotal unique prefixes: %d\n", len(prefixMap))
	fmt.Printf("Total keys sampled: %d\n", count)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}