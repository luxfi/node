package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
)

// BlockEntry is the canonical export format for JSONL
// This format is used by import-chain for direct BadgerDB import
type BlockEntry struct {
	Height   uint64 `json:"height"`
	Hash     string `json:"hash"`
	Header   string `json:"header"`   // RLP hex encoded
	Body     string `json:"body"`     // RLP hex encoded
	Receipts string `json:"receipts"` // RLP hex encoded
}

func main() {
	var (
		srcPath   = flag.String("source", "", "Source PebbleDB path (required)")
		dstPath   = flag.String("output", "", "Output JSONL file path (required)")
		maxHeight = flag.Uint64("max", 0, "Maximum block height (0 = no limit)")
		workers   = flag.Int("workers", 0, "Number of parallel workers (default: NumCPU)")
		bufSize   = flag.Int("buffer", 10000, "Write buffer size in blocks")
	)
	flag.Parse()

	if *srcPath == "" || *dstPath == "" {
		flag.Usage()
		log.Fatal("Both --source and --output are required")
	}

	if *workers == 0 {
		*workers = runtime.NumCPU()
	}

	fmt.Println("=== Canonical Block Export Tool ===")
	fmt.Printf("Source PebbleDB: %s\n", *srcPath)
	fmt.Printf("Output JSONL:    %s\n", *dstPath)
	fmt.Printf("Workers:         %d\n", *workers)
	if *maxHeight > 0 {
		fmt.Printf("Max Height:      %d\n", *maxHeight)
	} else {
		fmt.Printf("Max Height:      unlimited\n")
	}
	fmt.Println()

	startTime := time.Now()

	// Open source PebbleDB
	srcDB, err := pebble.Open(*srcPath, &pebble.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("Failed to open source DB: %v", err)
	}
	defer srcDB.Close()

	// SubnetEVM namespace prefix (from historic chain)
	namespace, _ := hex.DecodeString("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")

	// Step 1: Scan all block hashes and numbers
	fmt.Println("Step 1: Scanning for blocks...")
	blockMap := make(map[uint64][]byte) // number -> hash

	iter, err := srcDB.NewIter(nil)
	if err != nil {
		log.Fatalf("Failed to create iterator: %v", err)
	}

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()

		// Hash to number mapping (namespace + 'H' + hash)
		if len(key) == 65 && bytes.HasPrefix(key, namespace) && key[32] == 'H' {
			hash := make([]byte, 32)
			copy(hash, key[33:65])
			if len(value) == 8 {
				number := binary.BigEndian.Uint64(value)
				if *maxHeight == 0 || number <= *maxHeight {
					blockMap[number] = hash
				}
			}
		}
	}
	iter.Close()

	fmt.Printf("Found %d blocks\n", len(blockMap))

	// Sort block numbers for sequential output
	numbers := make([]uint64, 0, len(blockMap))
	for num := range blockMap {
		numbers = append(numbers, num)
	}
	sort.Slice(numbers, func(i, j int) bool {
		return numbers[i] < numbers[j]
	})

	// Check for gaps
	gaps := findGaps(numbers)
	if len(gaps) > 0 {
		fmt.Printf("WARNING: Found %d gaps in block sequence\n", len(gaps))
		for _, gap := range gaps[:min(5, len(gaps))] {
			fmt.Printf("  Gap: blocks %d to %d missing\n", gap[0], gap[1])
		}
	} else {
		fmt.Println("✓ No gaps - complete chain")
	}

	// Step 2: Extract blocks in parallel
	fmt.Println("\nStep 2: Extracting blocks...")

	// Create output file
	outFile, err := os.Create(*dstPath)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer outFile.Close()

	writer := bufio.NewWriterSize(outFile, 16*1024*1024) // 16MB buffer
	defer writer.Flush()

	// Progress tracking
	var exported int64
	var failed int64
	progressMu := sync.Mutex{}

	// Channel for extracted blocks (maintain order)
	type blockResult struct {
		index int
		entry *BlockEntry
	}
	results := make(chan blockResult, *bufSize)

	// Worker pool for extraction
	jobs := make(chan struct{ index int; number uint64; hash []byte }, *workers*2)

	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				entry := extractBlock(srcDB, namespace, job.number, job.hash)
				if entry != nil {
					results <- blockResult{index: job.index, entry: entry}
				} else {
					progressMu.Lock()
					failed++
					progressMu.Unlock()
				}
			}
		}()
	}

	// Writer goroutine - writes in order
	writerDone := make(chan bool)
	go func() {
		pending := make(map[int]*BlockEntry)
		nextIndex := 0

		for result := range results {
			pending[result.index] = result.entry

			// Write all consecutive blocks we have
			for {
				if entry, ok := pending[nextIndex]; ok {
					data, err := json.Marshal(entry)
					if err == nil {
						writer.Write(data)
						writer.WriteByte('\n')

						progressMu.Lock()
						exported++
						count := exported
						progressMu.Unlock()

						if count%10000 == 0 {
							elapsed := time.Since(startTime).Seconds()
							rate := float64(count) / elapsed
							fmt.Printf("Exported %d blocks (%.0f blocks/sec)\n", count, rate)
						}
					}
					delete(pending, nextIndex)
					nextIndex++
				} else {
					break
				}
			}
		}

		// Flush any remaining
		writer.Flush()
		writerDone <- true
	}()

	// Submit all jobs
	for i, number := range numbers {
		jobs <- struct{ index int; number uint64; hash []byte }{
			index: i, number: number, hash: blockMap[number],
		}
	}
	close(jobs)

	// Wait for workers
	wg.Wait()
	close(results)
	<-writerDone

	elapsed := time.Since(startTime)
	fmt.Printf("\n=== Export Complete ===\n")
	fmt.Printf("Total exported: %d blocks\n", exported)
	fmt.Printf("Failed:         %d blocks\n", failed)
	fmt.Printf("Duration:       %v\n", elapsed.Round(time.Second))
	fmt.Printf("Rate:           %.0f blocks/sec\n", float64(exported)/elapsed.Seconds())
	fmt.Printf("Output:         %s\n", *dstPath)

	// Get file size
	if stat, err := os.Stat(*dstPath); err == nil {
		fmt.Printf("File size:      %.2f GB\n", float64(stat.Size())/(1024*1024*1024))
	}
}

func extractBlock(db *pebble.DB, namespace []byte, number uint64, hash []byte) *BlockEntry {
	entry := &BlockEntry{
		Height: number,
		Hash:   "0x" + hex.EncodeToString(hash),
	}

	numBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(numBytes, number)

	// Extract header
	headerKey := append([]byte(nil), namespace...)
	headerKey = append(headerKey, 'h')
	headerKey = append(headerKey, numBytes...)
	headerKey = append(headerKey, hash...)

	if headerValue, closer, err := db.Get(headerKey); err == nil {
		entry.Header = "0x" + hex.EncodeToString(headerValue)
		closer.Close()
	} else {
		return nil // Header is required
	}

	// Extract body
	bodyKey := append([]byte(nil), namespace...)
	bodyKey = append(bodyKey, 'b')
	bodyKey = append(bodyKey, numBytes...)
	bodyKey = append(bodyKey, hash...)

	if bodyValue, closer, err := db.Get(bodyKey); err == nil {
		entry.Body = "0x" + hex.EncodeToString(bodyValue)
		closer.Close()
	} else {
		entry.Body = "0x" // Empty body is valid
	}

	// Extract receipts
	receiptKey := append([]byte(nil), namespace...)
	receiptKey = append(receiptKey, 'r')
	receiptKey = append(receiptKey, numBytes...)
	receiptKey = append(receiptKey, hash...)

	if receiptValue, closer, err := db.Get(receiptKey); err == nil {
		entry.Receipts = "0x" + hex.EncodeToString(receiptValue)
		closer.Close()
	} else {
		entry.Receipts = "0x" // Empty receipts is valid
	}

	return entry
}

func findGaps(numbers []uint64) [][2]uint64 {
	if len(numbers) == 0 {
		return nil
	}

	gaps := [][2]uint64{}
	for i := 1; i < len(numbers); i++ {
		if numbers[i] > numbers[i-1]+1 {
			gaps = append(gaps, [2]uint64{numbers[i-1] + 1, numbers[i] - 1})
		}
	}
	return gaps
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
