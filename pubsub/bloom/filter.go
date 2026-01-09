// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"errors"

	"github.com/luxfi/vm/utils/bloom"
)

const bytesPerHash = 8

var (
	_ Filter = (*filter)(nil)

	errMaxBytes = errors.New("too large")
)

type Filter interface {
	// Add adds to filter, assumed thread safe
	Add(...[]byte)

	// Check checks filter, assumed thread safe
	Check([]byte) bool
}

func New(maxN int, p float64, maxBytes int) (Filter, error) {
	numHashes, numEntries := bloom.OptimalParameters(maxN, p)
	if neededBytes := 1 + numHashes*bytesPerHash + numEntries; neededBytes > maxBytes {
		return nil, errMaxBytes
	}
	// Use the bloom filter struct directly
	f, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return nil, err
	}
	return &filter{
		filter: f,
	}, nil
}

type filter struct {
	filter *bloom.Filter
}

func (f *filter) Add(bl ...[]byte) {
	// The bloom.Filter.Add method takes a uint64 hash, not raw bytes
	// We need to hash the bytes first
	for _, b := range bl {
		if len(b) > 0 {
			hash := bloom.Hash(b, nil)
			f.filter.Add(hash)
		}
	}
}

func (f *filter) Check(b []byte) bool {
	// Similarly, Contains takes a uint64 hash
	if len(b) == 0 {
		return false
	}
	hash := bloom.Hash(b, nil)
	return f.filter.Contains(hash)
}
