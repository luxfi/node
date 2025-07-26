// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebbledb

import (
	"github.com/cockroachdb/pebble"

	"github.com/luxfi/node/database"
)

var _ database.Batch = (*batch)(nil)

type batch struct {
	d *Database
	*pebble.Batch
}

func (b *batch) Put(key, value []byte) error {
	return updateError(b.Set(key, value, b.d.writeOptions))
}

func (b *batch) Delete(key []byte) error {
	return updateError(b.Batch.Delete(key, b.d.writeOptions))
}

func (b *batch) Size() int {
	// TODO: Implement a more accurate size calculation, this only returns the
	// number of operations not the size of the data. The Pebble batch doesn't
	// expose the size of the data like the goleveldb batch does.
	return int(b.Count())
}

func (b *batch) Write() error {
	b.d.lock.RLock()
	defer b.d.lock.RUnlock()

	if b.d.closed {
		return database.ErrClosed
	}

	return updateError(b.Commit(b.d.writeOptions))
}

func (b *batch) Reset() {
	b.Batch.Reset()
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	reader := b.Reader()
	for {
		kind, key, value, ok, err := reader.Next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}

		switch kind {
		case pebble.InternalKeyKindSet, pebble.InternalKeyKindSetWithDelete:
			if err := w.Put(key, value); err != nil {
				return err
			}
		case pebble.InternalKeyKindDelete:
			if err := w.Delete(key); err != nil {
				return err
			}
		case pebble.InternalKeyKindSingleDelete:
			if err := w.Delete(key); err != nil {
				return err
			}
		case pebble.InternalKeyKindRangeDelete:
			// RangeDelete is not supported in the replay
			return errInvalidOperation
		case pebble.InternalKeyKindLogData:
			// LogData is ignored
		default:
			return errInvalidOperation
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch {
	return b
}