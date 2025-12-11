package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// BlockEntry supports both canonical formats:
// Format 1: height/header/body/receipts
// Format 2: number/header_rlp/body_rlp/receipts_rlp
type BlockEntry struct {
	// Format 1 (canonical import-chain format)
	Height   uint64 `json:"height,omitempty"`
	Header   string `json:"header,omitempty"`
	Body     string `json:"body,omitempty"`
	Receipts string `json:"receipts,omitempty"`

	// Format 2 (export format)
	Number      uint64 `json:"number,omitempty"`
	HeaderRLP   string `json:"header_rlp,omitempty"`
	BodyRLP     string `json:"body_rlp,omitempty"`
	ReceiptsRLP string `json:"receipts_rlp,omitempty"`

	// Common
	Hash string `json:"hash"`
}

// Normalize returns the block number and RLP data regardless of format
func (e *BlockEntry) Normalize() (uint64, string, string, string) {
	number := e.Height
	if number == 0 && e.Number > 0 {
		number = e.Number
	}

	header := e.Header
	if header == "" {
		header = e.HeaderRLP
	}

	body := e.Body
	if body == "" {
		body = e.BodyRLP
	}

	receipts := e.Receipts
	if receipts == "" {
		receipts = e.ReceiptsRLP
	}

	return number, header, body, receipts
}

var (
	totalImported int64
	totalFailed   int64
)

func main() {
	var (
		inputPath = flag.String("input", "", "Input JSONL file (required)")
		dbPath    = flag.String("db", "", "Output BadgerDB path (required)")
		workers   = flag.Int("workers", 0, "Number of parallel workers (default: NumCPU)")
		batchSize = flag.Int("batch", 1000, "Blocks per batch write")
		bufSize   = flag.Int("buffer", 10*1024*1024, "Scanner buffer size in bytes")
	)
	flag.Parse()

	if *inputPath == "" || *dbPath == "" {
		flag.Usage()
		log.Fatal("Both --input and --db are required")
	}

	if *workers == 0 {
		*workers = runtime.NumCPU()
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	fmt.Println("=== Fast Direct BadgerDB Import ===")
	fmt.Printf("Input:    %s\n", *inputPath)
	fmt.Printf("Database: %s\n", *dbPath)
	fmt.Printf("Workers:  %d\n", *workers)
	fmt.Printf("Batch:    %d blocks\n", *batchSize)
	fmt.Println()

	startTime := time.Now()

	// Open or create BadgerDB
	os.MkdirAll(*dbPath, 0755)
	opts := badger.DefaultOptions(*dbPath)
	opts.Logger = nil
	opts.SyncWrites = false             // Faster writes (fsync at end)
	opts.NumMemtables = 8               // More memory tables
	opts.NumLevelZeroTables = 8         // More L0 tables
	opts.NumLevelZeroTablesStall = 16   // More L0 before stall
	opts.ValueLogFileSize = 1 << 30     // 1GB value log files
	opts.NumCompactors = 4              // More compactors

	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer func() {
		fmt.Println("Syncing database...")
		db.Sync()
		db.Close()
	}()

	// Open input file
	file, err := os.Open(*inputPath)
	if err != nil {
		log.Fatalf("Failed to open input file: %v", err)
	}
	defer file.Close()

	// Get file size for progress
	fileInfo, _ := file.Stat()
	fileSize := fileInfo.Size()

	// Create scanner with large buffer
	scanner := bufio.NewScanner(file)
	buf := make([]byte, *bufSize)
	scanner.Buffer(buf, *bufSize)

	// Channel for blocks
	type blockJob struct {
		entries []BlockEntry
	}
	jobs := make(chan blockJob, *workers*2)

	// Worker pool
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for job := range jobs {
				writeBlocks(db, job.entries)
			}
		}(i)
	}

	// Progress reporter
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			imported := atomic.LoadInt64(&totalImported)
			failed := atomic.LoadInt64(&totalFailed)
			elapsed := time.Since(startTime).Seconds()
			rate := float64(imported) / elapsed
			fmt.Printf("Progress: imported=%d, failed=%d, rate=%.0f blocks/sec\n",
				imported, failed, rate)
		}
	}()

	// Read and batch blocks
	var batch []BlockEntry
	linesRead := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry BlockEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			atomic.AddInt64(&totalFailed, 1)
			continue
		}

		batch = append(batch, entry)
		linesRead++

		if len(batch) >= *batchSize {
			jobs <- blockJob{entries: batch}
			batch = make([]BlockEntry, 0, *batchSize)
		}

		// Progress every 100k lines
		if linesRead%100000 == 0 {
			pct := float64(linesRead) / float64(fileSize) * 100 * 500 // rough estimate
			if pct > 100 {
				pct = 99
			}
			fmt.Printf("Read %d lines (est. %.1f%% of file)\n", linesRead, pct)
		}
	}

	// Send remaining
	if len(batch) > 0 {
		jobs <- blockJob{entries: batch}
	}

	close(jobs)
	wg.Wait()

	if err := scanner.Err(); err != nil {
		log.Printf("Scanner error: %v", err)
	}

	// Final sync
	fmt.Println("Final compaction...")
	db.Flatten(4) // Flatten to reduce levels

	elapsed := time.Since(startTime)
	imported := atomic.LoadInt64(&totalImported)
	failed := atomic.LoadInt64(&totalFailed)

	fmt.Printf("\n=== Import Complete ===\n")
	fmt.Printf("Total Imported: %d blocks\n", imported)
	fmt.Printf("Total Failed:   %d blocks\n", failed)
	fmt.Printf("Duration:       %v\n", elapsed.Round(time.Second))
	fmt.Printf("Rate:           %.0f blocks/sec\n", float64(imported)/elapsed.Seconds())
	fmt.Printf("Database:       %s\n", *dbPath)
}

func writeBlocks(db *badger.DB, entries []BlockEntry) {
	wb := db.NewWriteBatch()
	defer wb.Cancel()

	for _, entry := range entries {
		number, headerHex, bodyHex, receiptsHex := entry.Normalize()

		// Decode hash
		hashBytes, err := hex.DecodeString(strings.TrimPrefix(entry.Hash, "0x"))
		if err != nil {
			atomic.AddInt64(&totalFailed, 1)
			continue
		}

		// Decode header
		headerBytes, err := hex.DecodeString(strings.TrimPrefix(headerHex, "0x"))
		if err != nil {
			atomic.AddInt64(&totalFailed, 1)
			continue
		}

		// Decode body
		bodyBytes, err := hex.DecodeString(strings.TrimPrefix(bodyHex, "0x"))
		if err != nil {
			bodyBytes = []byte{} // Empty body is valid
		}

		// Decode receipts
		receiptsBytes, err := hex.DecodeString(strings.TrimPrefix(receiptsHex, "0x"))
		if err != nil {
			receiptsBytes = []byte{} // Empty receipts is valid
		}

		numBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(numBytes, number)

		// Write header: 'h' + number + hash -> headerRLP
		headerKey := make([]byte, 1+8+32)
		headerKey[0] = 'h'
		copy(headerKey[1:9], numBytes)
		copy(headerKey[9:41], hashBytes)
		if err := wb.Set(headerKey, headerBytes); err != nil {
			log.Printf("Failed to write header for block %d: %v", number, err)
		}

		// Write body: 'b' + number + hash -> bodyRLP
		bodyKey := make([]byte, 1+8+32)
		bodyKey[0] = 'b'
		copy(bodyKey[1:9], numBytes)
		copy(bodyKey[9:41], hashBytes)
		if err := wb.Set(bodyKey, bodyBytes); err != nil {
			log.Printf("Failed to write body for block %d: %v", number, err)
		}

		// Write receipts: 'r' + number + hash -> receiptsRLP
		if len(receiptsBytes) > 0 {
			receiptsKey := make([]byte, 1+8+32)
			receiptsKey[0] = 'r'
			copy(receiptsKey[1:9], numBytes)
			copy(receiptsKey[9:41], hashBytes)
			if err := wb.Set(receiptsKey, receiptsBytes); err != nil {
				log.Printf("Failed to write receipts for block %d: %v", number, err)
			}
		}

		// Write hash->number mapping: 'H' + hash -> number
		hashKey := make([]byte, 1+32)
		hashKey[0] = 'H'
		copy(hashKey[1:33], hashBytes)
		if err := wb.Set(hashKey, numBytes); err != nil {
			log.Printf("Failed to write hash mapping for block %d: %v", number, err)
		}

		// Write canonical hash: 'n' + number -> hash
		canonicalKey := make([]byte, 1+8)
		canonicalKey[0] = 'n'
		copy(canonicalKey[1:9], numBytes)
		if err := wb.Set(canonicalKey, hashBytes); err != nil {
			log.Printf("Failed to write canonical hash for block %d: %v", number, err)
		}

		atomic.AddInt64(&totalImported, 1)
	}

	if err := wb.Flush(); err != nil {
		log.Printf("Failed to flush batch: %v", err)
	}
}
