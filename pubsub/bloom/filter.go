// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"errors"

	"github.com/luxfi/node/utils/bloom"
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
	f, err := bloom.New(uint64(maxN), p, uint64(maxBytes))
	if err != nil {
		return nil, err
	}
	return &filter{
		filter: f,
	}, nil
}

type filter struct {
	filter bloom.Filter
}

func (f *filter) Add(bl ...[]byte) {
	f.filter.Add(bl...)
}

func (f *filter) Check(b []byte) bool {
	return f.filter.Check(b)
}
