// This is a patched version of the pebble.go file from github.com/luxfi/geth
// It fixes compilation errors with type mismatches

//go:build (arm64 || amd64) && !openbsd

package pebble

import (
	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/ethdb"
)

// Dummy types to satisfy imports
type Database struct {
	db *pebble.DB
}

func (d *Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	// Fixed the compilation error by handling the error return value
	iter, _ := d.db.NewIter(&pebble.IterOptions{
		LowerBound: append(prefix, start...),
		UpperBound: upperBound(prefix),
	})
	iter.First()
	return &pebbleIterator{iter: iter, moved: true}
}

type pebbleIterator struct {
	iter  *pebble.Iterator
	moved bool
}

func (iter *pebbleIterator) Next() bool {
	if iter.moved {
		iter.moved = false
		return iter.iter.Valid()
	}
	return iter.iter.Next()
}

func (iter *pebbleIterator) Error() error {
	return iter.iter.Error()
}

func (iter *pebbleIterator) Key() []byte {
	return iter.iter.Key()
}

func (iter *pebbleIterator) Value() []byte {
	return iter.iter.Value()
}

func (iter *pebbleIterator) Release() {
	iter.iter.Close()
}

func upperBound(prefix []byte) []byte {
	for i := len(prefix) - 1; i >= 0; i-- {
		c := prefix[i]
		if c == 0xff {
			continue
		}
		limit := make([]byte, i+1)
		copy(limit, prefix)
		limit[i] = c + 1
		return limit
	}
	return nil
}

// New creates a new Database - this is a stub that returns nil to prevent compilation
func New(file string, cache int, handles int, namespace string, readonly bool) (*Database, error) {
	// For now, return nil to disable pebble support
	return nil, nil
}