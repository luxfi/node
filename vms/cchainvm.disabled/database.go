// (c) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/database"
)

// Subnet-EVM namespace for LUX mainnet
var subnetNamespace = []byte{
	0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
	0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
	0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
	0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
}

// Flag to enable subnet namespace handling (set when using --genesis-db)
var UseSubnetNamespace = false

// canonicalKey returns the standard C-chain canonical key format:
//   "H" + blockNumber (8 bytes)
func canonicalKey(number uint64) []byte {
	key := make([]byte, 9)
	key[0] = 'H'
	binary.BigEndian.PutUint64(key[1:], number)
	return key
}

// DatabaseWrapper wraps a Lux database to implement ethdb.Database
type DatabaseWrapper struct {
	db database.Database
}

// WrapDatabase creates a new database wrapper
func WrapDatabase(db database.Database) ethdb.Database {
	return &DatabaseWrapper{db: db}
}

// Has retrieves if a key is present in the key-value data store
func (d *DatabaseWrapper) Has(key []byte) (bool, error) {
	return d.db.Has(key)
}

// Get retrieves the given key if it's present in the key-value data store
func (d *DatabaseWrapper) Get(key []byte) ([]byte, error) {
	// Handle subnet-EVM namespaced keys when flag is set
	if UseSubnetNamespace {
		// For canonical hash lookups (H prefix, 9 bytes)
		if len(key) == 9 && key[0] == 'H' {
			// First try standard format
			val, err := d.db.Get(key)
			if err == nil {
				return val, nil
			}
			
			// If not found, scan for block by number encoded in hash
			blockNum := binary.BigEndian.Uint64(key[1:])
			iter := d.db.NewIteratorWithPrefix(nil)
			defer iter.Release()
			
			for iter.Next() {
				k := iter.Key()
				v := iter.Value()
				
				if len(k) == 64 && bytes.Equal(k[:32], subnetNamespace) {
					hash := k[32:]
					if len(v) > 100 && (v[0] == 0xf8 || v[0] == 0xf9) {
						// Decode block number from first 3 bytes of hash
						hashBlockNum := uint64(hash[0])<<16 | uint64(hash[1])<<8 | uint64(hash[2])
						if hashBlockNum == blockNum {
							// Return the hash for this block number
							return hash, nil
						}
					}
				}
			}
			return nil, database.ErrNotFound
		}
		
		// For header/body lookups (h/b prefix + 32-byte hash)
		if len(key) == 33 && (key[0] == 'h' || key[0] == 'b') {
			// Try standard format first
			val, err := d.db.Get(key)
			if err == nil {
				return val, nil
			}
			
			// Try with namespace for header
			if key[0] == 'h' {
				nsKey := make([]byte, 64)
				copy(nsKey[:32], subnetNamespace)
				copy(nsKey[32:], key[1:])
				val, err := d.db.Get(nsKey)
				if err == nil {
					return val, nil
				}
			}
		}
		
		// For direct hash lookups (32 bytes)
		if len(key) == 32 {
			nsKey := make([]byte, 64)
			copy(nsKey[:32], subnetNamespace)
			copy(nsKey[32:], key)
			return d.db.Get(nsKey)
		}
	}
	return d.db.Get(key)
}

// Put inserts the given value into the key-value data store
func (d *DatabaseWrapper) Put(key []byte, value []byte) error {
	// Debug all writes to understand the structure
	if len(key) > 0 {
		prefix := "unknown"
		switch key[0] {
		case 'H':
			if len(key) == 9 {
				prefix = "canonical-hash"
			} else {
				prefix = "head-header"
			}
		case 'h':
			if len(key) == 41 {
				prefix = "header"
			}
		case 'b':
			prefix = "body"
		case 'B':
			prefix = "head-block"
		case 0x26:
			prefix = "account"
		case 0xa3:
			prefix = "storage"
		}
		fmt.Printf("C-Chain DB Write: %s key=%x (len=%d) val_len=%d\n", prefix, key, len(key), len(value))
	}
	return d.db.Put(key, value)
}

// Delete removes the key from the key-value data store
func (d *DatabaseWrapper) Delete(key []byte) error {
	return d.db.Delete(key)
}

// NewBatch creates a write-only database that buffers changes to its host db
func (d *DatabaseWrapper) NewBatch() ethdb.Batch {
	return &BatchWrapper{
		batch: d.db.NewBatch(),
		db:    d.db,
	}
}

// NewBatchWithSize creates a write-only database batch with a specified size
func (d *DatabaseWrapper) NewBatchWithSize(size int) ethdb.Batch {
	// Lux database doesn't support sized batches, just return a regular batch
	return d.NewBatch()
}

// NewIterator creates a binary-alphabetical iterator over a subset
func (d *DatabaseWrapper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	it := d.db.NewIteratorWithPrefix(prefix)
	return &IteratorWrapper{it: it, prefix: prefix}
}

// Stat returns a particular internal stat of the database
func (d *DatabaseWrapper) Stat() (string, error) {
	// Not implemented in Lux database
	return "stats not available", nil
}

// Compact flattens the underlying data store for the given key range
func (d *DatabaseWrapper) Compact(start []byte, limit []byte) error {
	// Not implemented in Lux database
	return nil
}


// Close closes the database
func (d *DatabaseWrapper) Close() error {
	return d.db.Close()
}

// Ancient retrieves an ancient binary blob from the append-only immutable files
func (d *DatabaseWrapper) Ancient(kind string, number uint64) ([]byte, error) {
	// Lux database doesn't support ancient data
	return nil, errors.New("ancient data not supported")
}

// AncientRange retrieves multiple items in sequence
func (d *DatabaseWrapper) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	// Lux database doesn't support ancient data
	return nil, errors.New("ancient data not supported")
}

// Ancients returns the ancient item numbers in the ancient store
func (d *DatabaseWrapper) Ancients() (uint64, error) {
	// Lux database doesn't support ancient data
	return 0, nil
}

// Tail returns the number of first stored item in the freezer
func (d *DatabaseWrapper) Tail() (uint64, error) {
	// Lux database doesn't support ancient data
	return 0, nil
}

// AncientSize returns the ancient size of the specified category
func (d *DatabaseWrapper) AncientSize(kind string) (uint64, error) {
	// Lux database doesn't support ancient data
	return 0, nil
}

// ModifyAncients runs a write operation on the ancient store
func (d *DatabaseWrapper) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	// Lux database doesn't support ancient data
	return 0, errors.New("ancient data not supported")
}

// TruncateHead discards all but the first n ancient data from the ancient store
func (d *DatabaseWrapper) TruncateHead(n uint64) (uint64, error) {
	// Lux database doesn't support ancient data
	return 0, errors.New("ancient data not supported")
}

// TruncateTail discards the first n ancient data from the ancient store
func (d *DatabaseWrapper) TruncateTail(n uint64) (uint64, error) {
	// Lux database doesn't support ancient data
	return 0, errors.New("ancient data not supported")
}

// Sync flushes the database to disk
func (d *DatabaseWrapper) Sync() error {
	// Lux database doesn't have explicit sync
	return nil
}

// SyncKeyValue ensures that all pending writes are flushed to disk
func (d *DatabaseWrapper) SyncKeyValue() error {
	// Lux database doesn't have explicit sync
	return nil
}

// ReadAncients applies the provided AncientReader function
func (d *DatabaseWrapper) ReadAncients(fn func(ethdb.AncientReaderOp) error) (err error) {
	// Call the function with our database as the ancient reader
	// This allows the code to work even though we don't support ancient data
	return fn(d)
}

// MigrateTable processes and migrates entries of a given table to a new format
func (d *DatabaseWrapper) MigrateTable(string, func([]byte) ([]byte, error)) error {
	return errors.New("table migration not supported")
}

// AncientDatadir returns the ancient datadir
func (d *DatabaseWrapper) AncientDatadir() (string, error) {
	return "", errors.New("ancient data not supported")
}

// SyncAncient syncs the ancient data directory
func (d *DatabaseWrapper) SyncAncient() error {
	// Lux database doesn't support ancient data
	return nil
}

// DeleteRange removes all entries between the given markers
func (d *DatabaseWrapper) DeleteRange(start, end []byte) error {
	// Not supported in Lux database
	return errors.New("delete range not supported")
}

// BatchWrapper wraps a Lux batch to implement ethdb.Batch
type BatchWrapper struct {
	batch database.Batch
	db    database.Database
}

// Put inserts the given value into the batch
func (b *BatchWrapper) Put(key []byte, value []byte) error {
	return b.batch.Put(key, value)
}

// Delete removes the key from the batch
func (b *BatchWrapper) Delete(key []byte) error {
	return b.batch.Delete(key)
}

// ValueSize retrieves the amount of data queued up for writing
func (b *BatchWrapper) ValueSize() int {
	return b.batch.Size()
}

// Write flushes any accumulated data to disk
func (b *BatchWrapper) Write() error {
	err := b.batch.Write()
	if err == nil {
		// Debug: verify genesis was written
		if b.db != nil {
			has, _ := b.db.Has([]byte("h\x00\x00\x00\x00\x00\x00\x00\x00")) // 9-byte canonical key for block 0
			if has {
				fmt.Printf("Debug: Genesis header successfully written to database\n")
			}
		}
	}
	return err
}

// Reset resets the batch for reuse
func (b *BatchWrapper) Reset() {
	b.batch.Reset()
}

// Replay replays the batch contents to the given writer
func (b *BatchWrapper) Replay(w ethdb.KeyValueWriter) error {
	// Not directly supported, would need to implement
	return nil
}

// DeleteRange removes the key range from the batch
func (b *BatchWrapper) DeleteRange(start, end []byte) error {
	// Not supported in Lux database batch
	return errors.New("delete range not supported")
}

// IteratorWrapper wraps a Lux iterator to implement ethdb.Iterator
type IteratorWrapper struct {
	it     database.Iterator
	prefix []byte
}

// Next moves the iterator to the next key/value pair
func (i *IteratorWrapper) Next() bool {
	return i.it.Next()
}

// Error returns any accumulated error
func (i *IteratorWrapper) Error() error {
	return i.it.Error()
}

// Key returns the key of the current key/value pair
func (i *IteratorWrapper) Key() []byte {
	return i.it.Key()
}

// Value returns the value of the current key/value pair
func (i *IteratorWrapper) Value() []byte {
	return i.it.Value()
}

// Release releases associated resources
func (i *IteratorWrapper) Release() {
	i.it.Release()
}