// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebbledb

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/luxfi/database"
	log "github.com/luxfi/log"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/units"
)

const (
	Name = "pebbledb"

	// pebbleByteOverHead is the number of bytes of constant overhead that
	// should be added to a batch size per operation.
	pebbleByteOverHead = 8

	defaultCacheSize = 512 * units.MiB
)

var (
	_ database.Database = (*Database)(nil)

	errInvalidOperation = errors.New("invalid operation")

	DefaultConfig = Config{
		CacheSize:                   defaultCacheSize,
		BytesPerSync:                512 * units.KiB,
		WALBytesPerSync:             0, // Default to no background syncing.
		MemTableStopWritesThreshold: 8,
		MemTableSize:                defaultCacheSize / 4,
		MaxOpenFiles:                4096,
		MaxConcurrentCompactions:    1,
		Sync:                        true,
	}
)

type Database struct {
	lock          sync.RWMutex
	pebbleDB      *pebble.DB
	closed        bool
	openIterators set.Set[*iter]
	writeOptions  *pebble.WriteOptions
}

type Config struct {
	CacheSize                   int64  `json:"cacheSize"`
	BytesPerSync                int    `json:"bytesPerSync"`
	WALBytesPerSync             int    `json:"walBytesPerSync"` // 0 means no background syncing
	MemTableStopWritesThreshold int    `json:"memTableStopWritesThreshold"`
	MemTableSize                uint64 `json:"memTableSize"`
	MaxOpenFiles                int    `json:"maxOpenFiles"`
	MaxConcurrentCompactions    int    `json:"maxConcurrentCompactions"`
	Sync                        bool   `json:"sync"`
}

// TODO: Add metrics
func New(file string, configBytes []byte, log log.Logger, _ prometheus.Registerer) (database.Database, error) {
	cfg := DefaultConfig
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			return nil, err
		}
	}

	opts := &pebble.Options{
		Cache:                       pebble.NewCache(cfg.CacheSize),
		BytesPerSync:                cfg.BytesPerSync,
		Comparer:                    pebble.DefaultComparer,
		WALBytesPerSync:             cfg.WALBytesPerSync,
		MemTableStopWritesThreshold: cfg.MemTableStopWritesThreshold,
		MemTableSize:                cfg.MemTableSize,
		MaxOpenFiles:                cfg.MaxOpenFiles,
		MaxConcurrentCompactions:    func() int { return cfg.MaxConcurrentCompactions },
	}
	opts.Experimental.ReadSamplingMultiplier = -1 // Disable seek compaction

	log.Info(
		"opening pebble",
		zap.Reflect("config", cfg),
	)

	db, err := pebble.Open(file, opts)
	return &Database{
		pebbleDB:      db,
		openIterators: set.Set[*iter]{},
		writeOptions:  &pebble.WriteOptions{Sync: cfg.Sync},
	}, err
}

func (d *Database) Close() error {
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.closed {
		return database.ErrClosed
	}

	d.closed = true

	for iter := range d.openIterators {
		iter.lock.Lock()
		iter.release()
		iter.lock.Unlock()
	}
	clear(d.openIterators)

	return updateError(d.pebbleDB.Close())
}

func (d *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	d.lock.RLock()
	defer d.lock.RUnlock()

	if d.closed {
		return nil, database.ErrClosed
	}

	metrics := d.pebbleDB.Metrics()
	return metrics, nil
}

func (d *Database) Has(key []byte) (bool, error) {
	d.lock.RLock()
	defer d.lock.RUnlock()

	if d.closed {
		return false, database.ErrClosed
	}

	_, closer, err := d.pebbleDB.Get(key)
	if closer != nil {
		if closeErr := closer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if errors.Is(err, pebble.ErrNotFound) {
		return false, nil
	}
	return err == nil, updateError(err)
}

func (d *Database) Get(key []byte) ([]byte, error) {
	d.lock.RLock()
	defer d.lock.RUnlock()

	if d.closed {
		return nil, database.ErrClosed
	}

	data, closer, err := d.pebbleDB.Get(key)
	if closer != nil {
		defer closer.Close()
	}
	if err != nil {
		return nil, updateError(err)
	}
	return slices.Clone(data), nil
}

func (d *Database) Put(key []byte, value []byte) error {
	d.lock.RLock()
	defer d.lock.RUnlock()

	if d.closed {
		return database.ErrClosed
	}

	return updateError(d.pebbleDB.Set(key, value, d.writeOptions))
}

func (d *Database) Delete(key []byte) error {
	d.lock.RLock()
	defer d.lock.RUnlock()

	if d.closed {
		return database.ErrClosed
	}

	return updateError(d.pebbleDB.Delete(key, d.writeOptions))
}

func (d *Database) NewBatch() database.Batch {
	return &batch{
		d:     d,
		Batch: d.pebbleDB.NewBatch(),
	}
}

func (d *Database) NewIterator() database.Iterator {
	return d.NewIteratorWithStartAndPrefix(nil, nil)
}

func (d *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return d.NewIteratorWithStartAndPrefix(start, nil)
}

func (d *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return d.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (d *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.closed {
		return &database.IteratorError{Err: database.ErrClosed}
	}

	opts := &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixToUpperBound(prefix),
	}
	pebbleIter, err := d.pebbleDB.NewIter(opts)
	if err != nil {
		return &database.IteratorError{Err: updateError(err)}
	}

	it := &iter{
		db:         d,
		iter:       pebbleIter,
		lowerBound: slices.Clone(opts.LowerBound),
		upperBound: slices.Clone(opts.UpperBound),
	}

	it.release = func() {
		defer d.openIterators.Remove(it)
		if err := it.iter.Close(); err != nil {
			it.err = err
		}
	}

	d.openIterators.Add(it)

	if start != nil {
		it.iter.SeekGE(start)
	} else {
		it.iter.First()
	}

	return it
}

func (d *Database) Compact(start []byte, limit []byte) error {
	d.lock.RLock()
	defer d.lock.RUnlock()

	if d.closed {
		return database.ErrClosed
	}

	return updateError(d.pebbleDB.Compact(start, limit, false))
}

func updateError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pebble.ErrNotFound):
		return database.ErrNotFound
	case errors.Is(err, pebble.ErrClosed):
		return database.ErrClosed
	default:
		return err
	}
}

func prefixToUpperBound(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}
	upperBound := slices.Clone(prefix)
	i := len(upperBound) - 1
	for ; i >= 0; i-- {
		if upperBound[i] < 0xff {
			upperBound[i]++
			break
		} else if i == 0 {
			return nil
		}
	}
	return upperBound[:i+1]
}
