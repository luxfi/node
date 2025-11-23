package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/cockroachdb/pebble"
	"github.com/dgraph-io/badger/v4"
)

func main() {
	var (
		srcPath = flag.String("source", "", "Source PebbleDB path (SubnetEVM)")
		dstPath = flag.String("dest", "", "Destination BadgerDB path")
		maxHeight = flag.Uint64("max", 1074616, "Maximum block height to export")
	)
	flag.Parse()

	if *srcPath == "" || *dstPath == "" {
		flag.Usage()
		log.Fatal("Both --source and --dest are required")
	}

	fmt.Println("=== Block Export Tool ===")
	fmt.Printf("Source: %s\n", *srcPath)
	fmt.Printf("Destination: %s\n", *dstPath)
	fmt.Printf("Max Height: %d\n\n", *maxHeight)

	// Open source PebbleDB
	srcDB, err := pebble.Open(*srcPath, &pebble.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("Failed to open source DB: %v", err)
	}
	defer srcDB.Close()

	// Open destination BadgerDB
	os.MkdirAll(*dstPath, 0755)
	opts := badger.DefaultOptions(*dstPath)
	opts.Logger = nil
	dstDB, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open destination DB: %v", err)
	}
	defer dstDB.Close()

	// SubnetEVM namespace
	namespace, _ := hex.DecodeString("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")

	// Collect all block numbers
	fmt.Println("Scanning for blocks...")
	blockNumbers := make([]uint64, 0)

	iter, err := srcDB.NewIter(nil)
	if err != nil {
		log.Fatalf("Failed to create iterator: %v", err)
	}
	defer iter.Close()

	// Scan for 'H' keys (hash -> number mappings)
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()

		// Check if this is a hash->number mapping
		if len(key) == 65 && bytes.HasPrefix(key, namespace) && key[32] == 'H' {
			value := iter.Value()
			if len(value) == 8 {
				blockNum := binary.BigEndian.Uint64(value)
				if blockNum <= *maxHeight {
					blockNumbers = append(blockNumbers, blockNum)
				}
			}
		}
	}

	// Sort block numbers
	sort.Slice(blockNumbers, func(i, j int) bool {
		return blockNumbers[i] < blockNumbers[j]
	})

	fmt.Printf("Found %d blocks to export\n\n", len(blockNumbers))

	// Export blocks in batches
	const batchSize = 1000
	exported := 0
	batch := dstDB.NewWriteBatch()

	for i, blockNum := range blockNumbers {
		// Find the hash for this block number
		var blockHash []byte

		// Scan for header with this number
		prefix := append([]byte(nil), namespace...)
		prefix = append(prefix, 'h')
		numBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(numBytes, blockNum)
		prefix = append(prefix, numBytes...)

		// Find header key with this prefix
		iter, _ := srcDB.NewIter(&pebble.IterOptions{
			LowerBound: prefix,
			UpperBound: append(prefix[:41], 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF),
		})

		if iter.First() && iter.Valid() {
			key := iter.Key()
			if len(key) == 73 {
				blockHash = key[41:73]

				// Export header
				headerKey := make([]byte, 41)
				headerKey[0] = 'h'
				copy(headerKey[1:9], numBytes)
				copy(headerKey[9:41], blockHash)

				headerValue := iter.Value()
				batch.Set(headerKey, headerValue)

				// Export body
				bodyKeySrc := append([]byte(nil), namespace...)
				bodyKeySrc = append(bodyKeySrc, 'b')
				bodyKeySrc = append(bodyKeySrc, numBytes...)
				bodyKeySrc = append(bodyKeySrc, blockHash...)

				if bodyValue, closer, err := srcDB.Get(bodyKeySrc); err == nil {
					bodyKey := make([]byte, 41)
					bodyKey[0] = 'b'
					copy(bodyKey[1:9], numBytes)
					copy(bodyKey[9:41], blockHash)
					batch.Set(bodyKey, bodyValue)
					closer.Close()
				}

				// Export receipts
				receiptKeySrc := append([]byte(nil), namespace...)
				receiptKeySrc = append(receiptKeySrc, 'r')
				receiptKeySrc = append(receiptKeySrc, numBytes...)
				receiptKeySrc = append(receiptKeySrc, blockHash...)

				if receiptValue, closer, err := srcDB.Get(receiptKeySrc); err == nil {
					receiptKey := make([]byte, 41)
					receiptKey[0] = 'r'
					copy(receiptKey[1:9], numBytes)
					copy(receiptKey[9:41], blockHash)
					batch.Set(receiptKey, receiptValue)
					closer.Close()
				}

				// Export hash->number mapping
				hashKey := make([]byte, 33)
				hashKey[0] = 'H'
				copy(hashKey[1:33], blockHash)
				batch.Set(hashKey, numBytes)

				exported++
			}
		}
		iter.Close()

		// Write batch periodically
		if (i+1)%batchSize == 0 || i == len(blockNumbers)-1 {
			if err := batch.Flush(); err != nil {
				log.Printf("Error writing batch: %v", err)
			}
			batch = dstDB.NewWriteBatch()
			fmt.Printf("Exported %d blocks...\n", exported)
		}
	}

	// Export metadata
	fmt.Println("\nExporting metadata...")

	// Export AcceptorTipKey and AcceptorTipHeightKey
	tipKey := append([]byte(nil), namespace...)
	tipKey = append(tipKey, []byte("AcceptorTipKey")...)
	if tipValue, closer, err := srcDB.Get(tipKey); err == nil {
		batch.Set([]byte("AcceptorTipKey"), tipValue)
		closer.Close()
	}

	heightKey := append([]byte(nil), namespace...)
	heightKey = append(heightKey, []byte("AcceptorTipHeightKey")...)
	if heightValue, closer, err := srcDB.Get(heightKey); err == nil {
		batch.Set([]byte("AcceptorTipHeightKey"), heightValue)
		closer.Close()
	}

	batch.Flush()

	fmt.Printf("\n=== Export Complete ===\n")
	fmt.Printf("Exported %d blocks to %s\n", exported, *dstPath)
}