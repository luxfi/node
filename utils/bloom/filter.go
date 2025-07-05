// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/bits"
	"sync"
)

// constants and errors are defined in optimal.go

type Filter struct {
	// numBits is always equal to [bitsPerByte * len(entries)]
	numBits uint64

	lock      sync.RWMutex
	hashSeeds []uint64
	entries   []byte
	count     int
}

// New creates a new Filter with the specified number of hashes and bytes for
// entries. The returned bloom filter is safe for concurrent usage.
func New(numHashes, numEntries int) (*Filter, error) {
	if numEntries < minEntries {
		return nil, errTooFewEntries
	}

	hashSeeds, err := newHashSeeds(numHashes)
	if err != nil {
		return nil, err
	}

	return &Filter{
		numBits:   uint64(numEntries * bitsPerByte),
		hashSeeds: hashSeeds,
		entries:   make([]byte, numEntries),
		count:     0,
	}, nil
}

func (f *Filter) Add(hash uint64) {
	f.lock.Lock()
	defer f.lock.Unlock()

	_ = 1 % f.numBits // hint to the compiler that numBits is not 0
	for _, seed := range f.hashSeeds {
		hash = bits.RotateLeft64(hash, hashRotation) ^ seed
		index := hash % f.numBits
		byteIndex := index / bitsPerByte
		bitIndex := index % bitsPerByte
		f.entries[byteIndex] |= 1 << bitIndex
	}
	f.count++
}

// Count returns the number of elements that have been added to the bloom
// filter.
func (f *Filter) Count() int {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.count
}

func (f *Filter) Contains(hash uint64) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return containsCommon(f.hashSeeds, f.entries, hash)
}

func (f *Filter) Marshal() []byte {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return marshalCommon(f.hashSeeds, f.entries)
}

func newHashSeeds(count int) ([]uint64, error) {
	switch {
	case count < minHashes:
		return nil, fmt.Errorf("%w: %d < %d", errTooFewHashes, count, minHashes)
	case count > maxHashes:
		return nil, fmt.Errorf("%w: %d > %d", errTooManyHashes, count, maxHashes)
	}

	bytes := make([]byte, count*bytesPerUint64)
	if _, err := rand.Reader.Read(bytes); err != nil {
		return nil, err
	}

	seeds := make([]uint64, count)
	for i := range seeds {
		seeds[i] = binary.BigEndian.Uint64(bytes[i*bytesPerUint64:])
	}
	return seeds, nil
}

