// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"time"
	

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/cache"
)

var _ cache.Cacher[struct{}, struct{}] = (*Cache[struct{}, struct{}])(nil)

type Cache[K comparable, V any] struct {
	cache.Cacher[K, V]

	metrics *metrics
}

func New[K comparable, V any](
	namespace string,
	registerer prometheus.Registerer,
	cache cache.Cacher[K, V],
) (*Cache[K, V], error) {
	metrics, err := newMetrics(namespace, registerer)
	return &Cache[K, V]{
		Cacher:  cache,
		metrics: metrics,
	}, err
}

func (c *Cache[K, V]) Put(key K, value V) {
	start := time.Now()
	c.Cacher.Put(key, value)
	putDuration := time.Since(start)

	c.metric.putCount.Inc()
	c.metric.putTime.Add(float64(putDuration))
	c.metric.len.Set(float64(c.Cacher.Len()))
	c.metric.portionFilled.Set(c.Cacher.PortionFilled())
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
	start := time.Now()
	value, has := c.Cacher.Get(key)
	getDuration := time.Since(start)

	if has {
		c.metric.getCount.With(hitLabels).Inc()
		c.metric.getTime.With(hitLabels).Add(float64(getDuration))
	} else {
		c.metric.getCount.With(missLabels).Inc()
		c.metric.getTime.With(missLabels).Add(float64(getDuration))
	}

	return value, has
}

func (c *Cache[K, _]) Evict(key K) {
	c.Cacher.Evict(key)

	c.metric.len.Set(float64(c.Cacher.Len()))
	c.metric.portionFilled.Set(c.Cacher.PortionFilled())
}

func (c *Cache[_, _]) Flush() {
	c.Cacher.Flush()

	c.metric.len.Set(float64(c.Cacher.Len()))
	c.metric.portionFilled.Set(c.Cacher.PortionFilled())
}
