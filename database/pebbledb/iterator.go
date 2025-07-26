// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebbledb

import (
	"bytes"
	"sync"

	"github.com/cockroachdb/pebble"

	"github.com/luxfi/node/database"
)

var _ database.Iterator = (*iter)(nil)

type iter struct {
	lock       sync.RWMutex
	db         *Database
	iter       *pebble.Iterator
	lowerBound []byte
	upperBound []byte
	release    func()
	err        error
}

func (it *iter) Next() bool {
	it.lock.RLock()
	defer it.lock.RUnlock()

	if it.iter == nil {
		return false
	}

	hasNext := it.iter.Next()
	// If the iterator has been exhausted, we still need to check if the last
	// element is valid.
	if !hasNext || !it.inBounds(it.iter.Key()) {
		return false
	}
	return true
}

func (it *iter) inBounds(key []byte) bool {
	if it.upperBound != nil && bytes.Compare(key, it.upperBound) >= 0 {
		return false
	}
	if it.lowerBound != nil && bytes.Compare(key, it.lowerBound) < 0 {
		return false
	}
	return true
}

func (it *iter) Error() error {
	it.lock.RLock()
	defer it.lock.RUnlock()

	if it.iter == nil {
		return it.err
	}

	return updateError(it.iter.Error())
}

func (it *iter) Key() []byte {
	it.lock.RLock()
	defer it.lock.RUnlock()

	if it.iter == nil {
		return nil
	}

	return it.iter.Key()
}

func (it *iter) Value() []byte {
	it.lock.RLock()
	defer it.lock.RUnlock()

	if it.iter == nil {
		return nil
	}

	return it.iter.Value()
}

func (it *iter) Release() {
	it.lock.Lock()
	defer it.lock.Unlock()

	if it.iter == nil {
		return
	}

	it.release()
	it.iter = nil
}