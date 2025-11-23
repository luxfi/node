package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"log"

	"github.com/cockroachdb/pebble"
)

func main() {
	var dbPath = flag.String("db", "/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb", "PebbleDB path")
	flag.Parse()

	// Open PebbleDB
	db, err := pebble.Open(*dbPath, &pebble.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// SubnetEVM namespace
	namespace, _ := hex.DecodeString("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")

	fmt.Println("Inspecting database keys...")
	fmt.Println("Namespace:", hex.EncodeToString(namespace))

	// Count different key types
	hashToNum := 0
	headers := 0
	bodies := 0
	receipts := 0
	other := 0

	iter, err := db.NewIter(nil)
	if err != nil {
		log.Fatalf("Failed to create iterator: %v", err)
	}
	defer iter.Close()

	// Sample some keys
	samples := 0
	for iter.First(); iter.Valid() && samples < 100; iter.Next() {
		key := iter.Key()
		
		if bytes.HasPrefix(key, namespace) {
			keyLen := len(key)
			if keyLen >= 33 {
				keyType := key[32]
				fmt.Printf("Key %d: len=%d, type='%c' (0x%02x), ", samples, keyLen, keyType, keyType)
				
				// Show more detail
				if keyLen >= 41 {
					fmt.Printf("next 8 bytes: %s, ", hex.EncodeToString(key[33:41]))
				}
				if keyLen >= 65 {
					fmt.Printf("hash: %s", hex.EncodeToString(key[33:65]))
				}
				fmt.Println()

				switch keyType {
				case 'H':
					hashToNum++
				case 'h':
					headers++
				case 'b':
					bodies++
				case 'r':
					receipts++
				default:
					other++
				}
			}
		}
		samples++
	}

	// Now count all keys
	fmt.Println("\nCounting all keys with namespace prefix...")
	totalWithNamespace := 0
	iter, _ = db.NewIter(nil)
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		if bytes.HasPrefix(key, namespace) {
			totalWithNamespace++
			
			if totalWithNamespace <= 10 {
				fmt.Printf("Sample key: %s\n", hex.EncodeToString(key))
			}
		}
	}
	iter.Close()

	fmt.Printf("\nTotal keys with namespace: %d\n", totalWithNamespace)
	fmt.Printf("Hash->Number mappings: %d\n", hashToNum)
	fmt.Printf("Headers: %d\n", headers)
	fmt.Printf("Bodies: %d\n", bodies)
	fmt.Printf("Receipts: %d\n", receipts)
	fmt.Printf("Other: %d\n", other)
}