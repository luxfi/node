// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

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
	"strings"

	"github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/ethdb"
)

type BlockEntry struct {
	Height   uint64 `json:"height"`
	Hash     string `json:"hash"`
	Header   string `json:"header"`
	Body     string `json:"body"`
	Receipts string `json:"receipts"`
}

// badgerDB implements ethdb.Database using BadgerDB
type badgerDB struct {
	db *badger.DB
}

func newBadgerDB(path string) (*badgerDB, error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	return &badgerDB{db: db}, nil
}

func (b *badgerDB) Has(key []byte) (bool, error) {
	var exists bool
	err := b.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			exists = false
			return nil
		}
		exists = true
		return err
	})
	return exists, err
}

func (b *badgerDB) Get(key []byte) ([]byte, error) {
	var val []byte
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		val, err = item.ValueCopy(nil)
		return err
	})
	return val, err
}

func (b *badgerDB) Put(key []byte, value []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})
}

func (b *badgerDB) Delete(key []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

func (b *badgerDB) NewBatch() ethdb.Batch {
	return &badgerBatch{db: b.db, wb: b.db.NewWriteBatch()}
}

func (b *badgerDB) Close() error {
	return b.db.Close()
}

func (b *badgerDB) DeleteRange(start []byte, end []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(start); it.Valid(); it.Next() {
			key := it.Item().Key()
			if len(end) > 0 && string(key) >= string(end) {
				break
			}
			if err := txn.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b *badgerDB) Stat() (string, error) {
	return "badgerdb stats not implemented", nil
}

func (b *badgerDB) SyncKeyValue() error {
	return b.db.Sync()
}

func (b *badgerDB) Compact(start []byte, limit []byte) error {
	// BadgerDB handles compaction automatically
	return nil
}

func (b *badgerDB) NewBatchWithSize(size int) ethdb.Batch {
	// BadgerDB doesn't support pre-sized batches, ignore size parameter
	return &badgerBatch{db: b.db, wb: b.db.NewWriteBatch()}
}

func (b *badgerDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	txn := b.db.NewTransaction(false) // read-only transaction
	it := txn.NewIterator(opts)

	// Combine prefix and start for the seek key
	seekKey := append(prefix, start...)
	it.Seek(seekKey)

	return &badgerIterator{
		txn: txn,
		it:  it,
	}
}

type badgerIterator struct {
	txn *badger.Txn
	it  *badger.Iterator
	err error
}

func (i *badgerIterator) Next() bool {
	if i.err != nil {
		return false
	}
	if !i.it.Valid() {
		return false
	}
	i.it.Next()
	return i.it.Valid()
}

func (i *badgerIterator) Error() error {
	return i.err
}

func (i *badgerIterator) Key() []byte {
	if !i.it.Valid() {
		return nil
	}
	return i.it.Item().KeyCopy(nil)
}

func (i *badgerIterator) Value() []byte {
	if !i.it.Valid() {
		return nil
	}
	val, err := i.it.Item().ValueCopy(nil)
	if err != nil {
		i.err = err
		return nil
	}
	return val
}

func (i *badgerIterator) Release() {
	i.it.Close()
	i.txn.Discard()
}

type badgerBatch struct {
	db *badger.DB
	wb *badger.WriteBatch
}

func (b *badgerBatch) Put(key []byte, value []byte) error {
	return b.wb.Set(key, value)
}

func (b *badgerBatch) Delete(key []byte) error {
	return b.wb.Delete(key)
}

func (b *badgerBatch) Write() error {
	return b.wb.Flush()
}

func (b *badgerBatch) ValueSize() int {
	return 0
}

func (b *badgerBatch) Reset() {
	b.wb.Cancel()
	b.wb = b.db.NewWriteBatch()
}

func (b *badgerBatch) DeleteRange(start []byte, end []byte) error {
	// Note: BadgerDB WriteBatch doesn't support range operations
	// This is a no-op as batch DeleteRange is rarely used
	return nil
}

func (b *badgerBatch) Replay(w ethdb.KeyValueWriter) error {
	// BadgerDB WriteBatch doesn't support replay
	// This would require storing operations in memory which defeats the purpose of batching
	return nil
}

// Helper functions for constructing database keys (copied from geth/core/rawdb/schema.go)
var (
	headerPrefix = []byte("h") // headerPrefix + num (uint64 big endian) + hash -> header
)

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// headerKey = headerPrefix + num (uint64 big endian) + hash
func headerKey(number uint64, hash common.Hash) []byte {
	return append(append(headerPrefix, encodeBlockNumber(number)...), hash.Bytes()...)
}

func main() {
	var (
		jsonlPath = flag.String("input", "blocks.jsonl", "Input JSONL file")
		dbPath    = flag.String("db", "", "Database path for C-Chain")
	)
	flag.Parse()

	if *dbPath == "" {
		flag.Usage()
		log.Fatal("--db is required")
	}

	fmt.Println("=== Block Import from JSONL ===")
	fmt.Printf("Input: %s\n", *jsonlPath)
	fmt.Printf("Database: %s\n\n", *dbPath)

	// Open C-Chain database (BadgerDB)
	db, err := newBadgerDB(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Wrap database for geth
	ethDB := rawdb.NewDatabase(db)

	// Open JSONL file
	file, err := os.Open(*jsonlPath)
	if err != nil {
		log.Fatalf("Failed to open input file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	imported := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry BlockEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			log.Printf("Failed to parse JSON: %v", err)
			continue
		}

		// Decode hex strings to bytes
		headerBytes, err := hex.DecodeString(strings.TrimPrefix(entry.Header, "0x"))
		if err != nil {
			log.Printf("Failed to decode header hex at height %d: %v", entry.Height, err)
			continue
		}

		bodyBytes, err := hex.DecodeString(strings.TrimPrefix(entry.Body, "0x"))
		if err != nil {
			log.Printf("Failed to decode body hex at height %d: %v", entry.Height, err)
			continue
		}

		receiptsBytes, err := hex.DecodeString(strings.TrimPrefix(entry.Receipts, "0x"))
		if err != nil {
			log.Printf("Failed to decode receipts hex at height %d: %v", entry.Height, err)
			continue
		}

		// Decode block hash
		blockHash, err := hex.DecodeString(strings.TrimPrefix(entry.Hash, "0x"))
		if err != nil {
			log.Printf("Failed to decode hash at height %d: %v", entry.Height, err)
			continue
		}

		// Write raw RLP directly to database keys (bypass types.Header decoding)
		// This avoids WithdrawalsHash compatibility issues with pre-Shanghai blocks
		hash := common.BytesToHash(blockHash)
		number := entry.Height

		// Write header RLP manually (rawdb.WriteHeaderRLP doesn't exist)
		// First write the hash -> number mapping
		rawdb.WriteHeaderNumber(ethDB, hash, number)
		// Then write the header RLP data
		key := headerKey(number, hash)
		if err := ethDB.Put(key, headerBytes); err != nil {
			log.Printf("Failed to write header at height %d: %v", entry.Height, err)
			continue
		}

		// Write body RLP
		rawdb.WriteBodyRLP(ethDB, hash, number, bodyBytes)

		// Write receipts RLP
		if len(receiptsBytes) > 0 {
			rawdb.WriteRawReceipts(ethDB, hash, number, receiptsBytes)
		}

		// Write canonical hash
		rawdb.WriteCanonicalHash(ethDB, hash, number)

		// Update head pointers
		rawdb.WriteHeadBlockHash(ethDB, hash)
		rawdb.WriteHeadHeaderHash(ethDB, hash)

		imported++
		if imported%1000 == 0 {
			fmt.Printf("Imported %d blocks...\n", imported)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading file: %v", err)
	}

	fmt.Printf("\n=== Import Complete ===\n")
	fmt.Printf("Imported %d blocks to %s\n", imported, *dbPath)
}
