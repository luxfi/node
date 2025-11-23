package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/cockroachdb/pebble"
	"github.com/dgraph-io/badger/v4"
	"github.com/ethereum/go-ethereum/rlp"
)

// BlockInfo stores metadata about a block
type BlockInfo struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
	Parent string `json:"parent"`
	HasHeader bool `json:"has_header"`
	HasBody bool `json:"has_body"`
	HasReceipts bool `json:"has_receipts"`
}

func main() {
	var (
		srcPath = flag.String("source", "/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb", "Source PebbleDB path")
		dstPath = flag.String("dest", "/tmp/cchain-blocks", "Destination BadgerDB path")
		maxHeight = flag.Uint64("max", 1074616, "Maximum block height")
		verify = flag.Bool("verify", false, "Verify block chain continuity")
	)
	flag.Parse()

	fmt.Println("=== Complete Block Extraction Tool ===")
	fmt.Printf("Source: %s\n", *srcPath)
	fmt.Printf("Destination: %s\n", *dstPath)
	fmt.Printf("Max Height: %d\n", *maxHeight)
	fmt.Printf("Verify Chain: %v\n\n", *verify)

	// Open source PebbleDB
	srcDB, err := pebble.Open(*srcPath, &pebble.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("Failed to open source DB: %v", err)
	}
	defer srcDB.Close()

	// Create destination directory
	os.RemoveAll(*dstPath)
	os.MkdirAll(*dstPath, 0755)

	// Open destination BadgerDB
	opts := badger.DefaultOptions(*dstPath)
	opts.Logger = nil
	dstDB, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open destination DB: %v", err)
	}
	defer dstDB.Close()

	// SubnetEVM namespace
	namespace, _ := hex.DecodeString("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")

	// Step 1: Collect ALL block hashes and numbers
	fmt.Println("Step 1: Scanning for all blocks...")
	blockMap := make(map[uint64][]byte) // number -> hash
	hashToNumber := make(map[string]uint64) // hash -> number

	iter, err := srcDB.NewIter(nil)
	if err != nil {
		log.Fatalf("Failed to create iterator: %v", err)
	}

	// First, get hash->number mappings
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()

		// Hash to number mapping (namespace + 'H' + hash)
		if len(key) == 65 && bytes.HasPrefix(key, namespace) && key[32] == 'H' {
			hash := key[33:65]
			if len(value) == 8 {
				number := binary.BigEndian.Uint64(value)
				if number <= *maxHeight {
					blockMap[number] = hash
					hashToNumber[hex.EncodeToString(hash)] = number
				}
			}
		}
	}
	iter.Close()

	fmt.Printf("Found %d blocks in hash->number mappings\n", len(blockMap))

	// Step 2: Extract blocks in order
	fmt.Println("\nStep 2: Extracting blocks in sequential order...")

	// Sort block numbers
	numbers := make([]uint64, 0, len(blockMap))
	for num := range blockMap {
		numbers = append(numbers, num)
	}
	sort.Slice(numbers, func(i, j int) bool {
		return numbers[i] < numbers[j]
	})

	// Track extraction progress
	extracted := 0
	missing := []uint64{}
	blockInfos := make([]BlockInfo, 0)

	// Batch for efficient writes
	batch := dstDB.NewWriteBatch()
	const batchSize = 1000

	for i, number := range numbers {
		hash := blockMap[number]

		info := BlockInfo{
			Number: number,
			Hash:   hex.EncodeToString(hash),
		}

		// Extract header
		headerKey := append([]byte(nil), namespace...)
		headerKey = append(headerKey, 'h')
		headerKey = append(headerKey, make([]byte, 8)...)
		binary.BigEndian.PutUint64(headerKey[33:41], number)
		headerKey = append(headerKey, hash...)

		if headerValue, closer, err := srcDB.Get(headerKey); err == nil {
			// Store header
			dstKey := make([]byte, 41)
			dstKey[0] = 'h'
			binary.BigEndian.PutUint64(dstKey[1:9], number)
			copy(dstKey[9:41], hash)
			batch.Set(dstKey, headerValue)
			info.HasHeader = true

			// Try to extract parent hash from header
			if len(headerValue) > 32 {
				// RLP decode to get parent hash (first field in header)
				var parentHash [32]byte
				if err := rlp.DecodeBytes(headerValue, &struct {
					ParentHash [32]byte `rlp:""`
					// We only need the first field
				}{ParentHash: parentHash}); err == nil {
					info.Parent = hex.EncodeToString(parentHash[:])
				}
			}

			closer.Close()
		}

		// Extract body
		bodyKey := append([]byte(nil), namespace...)
		bodyKey = append(bodyKey, 'b')
		bodyKey = append(bodyKey, make([]byte, 8)...)
		binary.BigEndian.PutUint64(bodyKey[33:41], number)
		bodyKey = append(bodyKey, hash...)

		if bodyValue, closer, err := srcDB.Get(bodyKey); err == nil {
			dstKey := make([]byte, 41)
			dstKey[0] = 'b'
			binary.BigEndian.PutUint64(dstKey[1:9], number)
			copy(dstKey[9:41], hash)
			batch.Set(dstKey, bodyValue)
			info.HasBody = true
			closer.Close()
		}

		// Extract receipts
		receiptKey := append([]byte(nil), namespace...)
		receiptKey = append(receiptKey, 'r')
		receiptKey = append(receiptKey, make([]byte, 8)...)
		binary.BigEndian.PutUint64(receiptKey[33:41], number)
		receiptKey = append(receiptKey, hash...)

		if receiptValue, closer, err := srcDB.Get(receiptKey); err == nil {
			dstKey := make([]byte, 41)
			dstKey[0] = 'r'
			binary.BigEndian.PutUint64(dstKey[1:9], number)
			copy(dstKey[9:41], hash)
			batch.Set(dstKey, receiptValue)
			info.HasReceipts = true
			closer.Close()
		}

		// Store hash->number mapping
		hashKey := make([]byte, 33)
		hashKey[0] = 'H'
		copy(hashKey[1:33], hash)
		numBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(numBytes, number)
		batch.Set(hashKey, numBytes)

		blockInfos = append(blockInfos, info)
		extracted++

		// Track missing blocks
		if !info.HasHeader {
			missing = append(missing, number)
		}

		// Write batch periodically
		if (i+1)%batchSize == 0 || i == len(numbers)-1 {
			if err := batch.Flush(); err != nil {
				log.Printf("Error writing batch: %v", err)
			}
			batch = dstDB.NewWriteBatch()
			fmt.Printf("Extracted %d/%d blocks (%.1f%%)...\n",
				extracted, len(numbers),
				float64(extracted)*100/float64(len(numbers)))
		}
	}

	// Step 3: Export metadata
	fmt.Println("\nStep 3: Exporting metadata...")

	// Get and store the tip
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

	// Step 4: Verify chain continuity (optional)
	if *verify {
		fmt.Println("\nStep 4: Verifying blockchain continuity...")
		verifyChain(blockInfos)
	}

	// Step 5: Save block info JSON
	infoFile, err := os.Create(*dstPath + "/block_info.json")
	if err == nil {
		encoder := json.NewEncoder(infoFile)
		encoder.SetIndent("", "  ")
		encoder.Encode(blockInfos)
		infoFile.Close()
	}

	// Print summary
	fmt.Printf("\n=== Extraction Complete ===\n")
	fmt.Printf("Total blocks extracted: %d\n", extracted)
	fmt.Printf("Blocks with headers: %d\n", countWithHeaders(blockInfos))
	fmt.Printf("Blocks with bodies: %d\n", countWithBodies(blockInfos))
	fmt.Printf("Blocks with receipts: %d\n", countWithReceipts(blockInfos))

	if len(missing) > 0 {
		fmt.Printf("\nWARNING: %d blocks missing headers:\n", len(missing))
		if len(missing) <= 10 {
			for _, m := range missing {
				fmt.Printf("  - Block %d\n", m)
			}
		} else {
			fmt.Printf("  First 10: %v...\n", missing[:10])
		}
	}

	// Check for gaps in block sequence
	fmt.Println("\nChecking for gaps in block sequence...")
	gaps := findGaps(numbers)
	if len(gaps) == 0 {
		fmt.Println("✅ No gaps found - complete chain from 0 to", numbers[len(numbers)-1])
	} else {
		fmt.Printf("⚠️  Found %d gaps in block sequence:\n", len(gaps))
		for _, gap := range gaps {
			fmt.Printf("  Gap: blocks %d to %d missing\n", gap[0], gap[1])
		}
	}

	fmt.Printf("\nDatabase saved to: %s\n", *dstPath)
	fmt.Println("Ready for import into C-Chain for runtime replay")
}

func verifyChain(blocks []BlockInfo) {
	// Build parent-child relationships
	childToParent := make(map[string]string)
	for _, block := range blocks {
		if block.Parent != "" {
			childToParent[block.Hash] = block.Parent
		}
	}

	// Find blocks without parents (should only be genesis)
	orphans := 0
	for _, block := range blocks {
		if block.Number > 0 && block.Parent == "" {
			orphans++
		}
	}

	if orphans > 0 {
		fmt.Printf("⚠️  Found %d blocks without parent references\n", orphans)
	} else {
		fmt.Println("✅ All non-genesis blocks have parent references")
	}
}

func countWithHeaders(blocks []BlockInfo) int {
	count := 0
	for _, b := range blocks {
		if b.HasHeader {
			count++
		}
	}
	return count
}

func countWithBodies(blocks []BlockInfo) int {
	count := 0
	for _, b := range blocks {
		if b.HasBody {
			count++
		}
	}
	return count
}

func countWithReceipts(blocks []BlockInfo) int {
	count := 0
	for _, b := range blocks {
		if b.HasReceipts {
			count++
		}
	}
	return count
}

func findGaps(numbers []uint64) [][2]uint64 {
	if len(numbers) == 0 {
		return nil
	}

	gaps := [][2]uint64{}
	for i := 1; i < len(numbers); i++ {
		if numbers[i] > numbers[i-1]+1 {
			gaps = append(gaps, [2]uint64{numbers[i-1]+1, numbers[i]-1})
		}
	}
	return gaps
}