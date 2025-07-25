// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/cache/cachetest"
	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/ids"
)

func TestInterface(t *testing.T) {
	scenarios := []struct {
		name  string
		setup func(size int) cache.Cacher[ids.ID, int64]
	}{
		{
			name: "cache LRU",
			setup: func(size int) cache.Cacher[ids.ID, int64] {
				return lru.NewCache[ids.ID, int64](size)
			},
		},
		{
			name: "sized cache LRU",
			setup: func(size int) cache.Cacher[ids.ID, int64] {
				return lru.NewSizedCache(size*cachetest.IntSize, cachetest.IntSizeFunc)
			},
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			for _, test := range cachetest.Tests {
				baseCache := scenario.setup(test.Size)
				c, err := New("", prometheus.NewRegistry(), baseCache)
				require.NoError(t, err)
				test.Func(t, c)
			}
		})
	}
}
