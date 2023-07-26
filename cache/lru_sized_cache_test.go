// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"testing"

	"github.com/luxdefi/node/ids"
)

func TestSizedLRU(t *testing.T) {
	cache := NewSizedLRU[ids.ID, int64](TestIntSize, TestIntSizeFunc)

	TestBasic(t, cache)
}

func TestSizedLRUEviction(t *testing.T) {
	cache := NewSizedLRU[ids.ID, int64](2*TestIntSize, TestIntSizeFunc)

	TestEviction(t, cache)
}
