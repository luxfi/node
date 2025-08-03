// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package metercacher

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/metrics"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/cache"
	"github.com/luxfi/ids"
)

func TestInterface(t *testing.T) {
	type scenario struct {
		description string
		setup       func(size int) cache.Cacher[ids.ID, int64]
	}

	scenarios := []scenario{
		{
			description: "cache LRU",
			setup: func(size int) cache.Cacher[ids.ID, int64] {
				return &cache.LRU[ids.ID, int64]{Size: size}
			},
		},
		{
			description: "sized cache LRU",
			setup: func(size int) cache.Cacher[ids.ID, int64] {
				return cache.NewSizedLRU[ids.ID, int64](size*cache.TestIntSize, cache.TestIntSizeFunc)
			},
		},
	}

	for _, scenario := range scenarios {
		for _, test := range cache.CacherTests {
			baseCache := scenario.setup(test.Size)
			c, err := New("", metrics.NewNoOpMetrics("test").Registry(), baseCache)
			require.NoError(t, err)
			test.Func(t, c)
		}
	}
}
