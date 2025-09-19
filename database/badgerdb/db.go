// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package badgerdb

import (
	"context"
	"fmt"
	"sync"

	"github.com/dgraph-io/badger/v3"
	"github.com/luxfi/node/database"
)

var _ database.Database = (*Database)(nil)

// Database is a wrapper around Badger database that implements the database.Database interface
type Database struct {
	db     *badger.DB
	closed bool
	lock   sync.RWMutex
}

// New creates a new Badger database instance
func New(path string, config *badger.Options) (*Database, error) {
	if config == nil {
		opts := badger.DefaultOptions(path)
		opts.Logger = nil // Disable verbose logging
		config = &opts
	}

	db, err := badger.Open(*config)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger database: %w", err)
	}

	return &Database{
		db: db,
	}, nil
}

// Has returns true if the key exists in the database
func (db *Database) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return false, database.ErrClosed
	}

	err := db.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		return err
	})

	if err == badger.ErrKeyNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Get retrieves a value by key
func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}

	var value []byte
	err := db.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		value, err = item.ValueCopy(nil)
		return err
	})

	if err == badger.ErrKeyNotFound {
		return nil, database.ErrNotFound
	}
	return value, err
}

// Put stores a key-value pair
func (db *Database) Put(key []byte, value []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}

	return db.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})
}

// Delete removes a key
func (db *Database) Delete(key []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}

	return db.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

// NewBatch creates a new batch
func (db *Database) NewBatch() database.Batch {
	return &batch{
		db:  db,
		ops: make([]batchOp, 0),
	}
}

// NewIterator creates a new iterator
func (db *Database) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

// NewIteratorWithStart creates an iterator with a start key
func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

// NewIteratorWithPrefix creates an iterator with a prefix
func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

// NewIteratorWithStartAndPrefix creates an iterator with both start and prefix
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return &iterator{err: database.ErrClosed}
	}

	txn := db.db.NewTransaction(false)
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix

	iter := txn.NewIterator(opts)
	if start != nil {
		iter.Seek(start)
	} else {
		iter.Rewind()
	}

	return &iterator{
		txn:    txn,
		iter:   iter,
		prefix: prefix,
	}
}

// Stat returns database statistics
func (db *Database) Stat(property string) (string, error) {
	// Badger doesn't have the same stats interface as LevelDB/Pebble
	// Return basic information
	lsm, vlog := db.db.Size()
	return fmt.Sprintf("LSM: %d bytes, ValueLog: %d bytes", lsm, vlog), nil
}

// Compact compacts the database
func (db *Database) Compact(start, limit []byte) error {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}

	// Badger handles compaction automatically, but we can trigger GC
	for {
		err := db.db.RunValueLogGC(0.5)
		if err == badger.ErrNoRewrite {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database
func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	db.closed = true
	return db.db.Close()
}

// HealthCheck returns nil if the database is healthy
func (db *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}

	// Try a simple operation to check health
	err := db.db.View(func(txn *badger.Txn) error {
		return nil
	})

	if err != nil {
		return nil, err
	}

	return map[string]string{"status": "healthy"}, nil
}

// batch implements database.Batch for Badger
type batch struct {
	db   *Database
	ops  []batchOp
	size int
}

type batchOp struct {
	delete bool
	key    []byte
	value  []byte
}

func (b *batch) Put(key, value []byte) error {
	b.ops = append(b.ops, batchOp{
		delete: false,
		key:    append([]byte{}, key...),
		value:  append([]byte{}, value...),
	})
	b.size += len(key) + len(value)
	return nil
}

func (b *batch) Delete(key []byte) error {
	b.ops = append(b.ops, batchOp{
		delete: true,
		key:    append([]byte{}, key...),
	})
	b.size += len(key)
	return nil
}

func (b *batch) Size() int {
	return b.size
}

func (b *batch) Write() error {
	b.db.lock.RLock()
	defer b.db.lock.RUnlock()

	if b.db.closed {
		return database.ErrClosed
	}

	return b.db.db.Update(func(txn *badger.Txn) error {
		for _, op := range b.ops {
			if op.delete {
				if err := txn.Delete(op.key); err != nil {
					return err
				}
			} else {
				if err := txn.Set(op.key, op.value); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (b *batch) Reset() {
	b.ops = b.ops[:0]
	b.size = 0
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, op := range b.ops {
		if op.delete {
			if err := w.Delete(op.key); err != nil {
				return err
			}
		} else {
			if err := w.Put(op.key, op.value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch {
	return b
}

// iterator implements database.Iterator for Badger
type iterator struct {
	txn    *badger.Txn
	iter   *badger.Iterator
	prefix []byte
	err    error
}

func (it *iterator) Next() bool {
	if it.err != nil || it.iter == nil {
		return false
	}
	it.iter.Next()
	return it.iter.Valid()
}

func (it *iterator) Error() error {
	return it.err
}

func (it *iterator) Key() []byte {
	if it.iter == nil || !it.iter.Valid() {
		return nil
	}
	return it.iter.Item().Key()
}

func (it *iterator) Value() []byte {
	if it.iter == nil || !it.iter.Valid() {
		return nil
	}
	val, err := it.iter.Item().ValueCopy(nil)
	if err != nil {
		it.err = err
		return nil
	}
	return val
}

func (it *iterator) Release() {
	if it.iter != nil {
		it.iter.Close()
	}
	if it.txn != nil {
		it.txn.Discard()
	}
}
