// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"errors"

	db "github.com/luxfi/database"
)

const (
	intervalPrefixByte byte = iota
	blockPrefixByte

	prefixLen = 1
)

var (
	intervalPrefix = []byte{intervalPrefixByte}
	blockPrefix    = []byte{blockPrefixByte}

	errInvalidKeyLength = errors.New("invalid key length")
)

func GetIntervals(dbIteratee db.Iteratee) ([]*Interval, error) {
	it := dbIteratee.NewIteratorWithPrefix(intervalPrefix)
	defer it.Release()

	var intervals []*Interval
	for it.Next() {
		dbKey := it.Key()
		if len(dbKey) < prefixLen {
			return nil, errInvalidKeyLength
		}

		intervalKey := dbKey[prefixLen:]
		upperBound, err := db.ParseUInt64(intervalKey)
		if err != nil {
			return nil, err
		}

		value := it.Value()
		lowerBound, err := db.ParseUInt64(value)
		if err != nil {
			return nil, err
		}

		intervals = append(intervals, &Interval{
			LowerBound: lowerBound,
			UpperBound: upperBound,
		})
	}
	return intervals, it.Error()
}

func PutInterval(dbWriter db.KeyValueWriter, upperBound uint64, lowerBound uint64) error {
	return db.PutUInt64(dbWriter, makeIntervalKey(upperBound), lowerBound)
}

func DeleteInterval(dbDeleter db.KeyValueDeleter, upperBound uint64) error {
	return dbDeleter.Delete(makeIntervalKey(upperBound))
}

// makeIntervalKey uses the upperBound rather than the lowerBound because blocks
// are fetched from tip towards genesis. This means that it is more common for
// the lowerBound to change than the upperBound. Modifying the lowerBound only
// requires a single write rather than a write and a delete when modifying the
// upperBound.
func makeIntervalKey(upperBound uint64) []byte {
	intervalKey := db.PackUInt64(upperBound)
	return append(intervalPrefix, intervalKey...)
}

// GetBlockIterator returns a block iterator that will produce values
// corresponding to persisted blocks in order of increasing height.
func GetBlockIterator(dbIteratee db.Iteratee) db.Iterator {
	return dbIteratee.NewIteratorWithPrefix(blockPrefix)
}

// GetBlockIteratorWithStart returns a block iterator that will produce values
// corresponding to persisted blocks in order of increasing height starting at
// [height].
func GetBlockIteratorWithStart(dbIteratee db.Iteratee, height uint64) db.Iterator {
	return dbIteratee.NewIteratorWithStartAndPrefix(
		makeBlockKey(height),
		blockPrefix,
	)
}

func GetBlock(dbReader db.KeyValueReader, height uint64) ([]byte, error) {
	return dbReader.Get(makeBlockKey(height))
}

func PutBlock(dbWriter db.KeyValueWriter, height uint64, bytes []byte) error {
	return dbWriter.Put(makeBlockKey(height), bytes)
}

func DeleteBlock(dbDeleter db.KeyValueDeleter, height uint64) error {
	return dbDeleter.Delete(makeBlockKey(height))
}

// makeBlockKey ensures that the returned key maintains the same sorted order as
// the height. This ensures that database iteration of block keys will iterate
// from lower height to higher height.
func makeBlockKey(height uint64) []byte {
	blockKey := db.PackUInt64(height)
	return append(blockPrefix, blockKey...)
}
