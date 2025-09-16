// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"
	"sync"

	metrics "github.com/luxfi/metric"
)

const (
	ioType    = "type"
	readType  = "read"
	writeType = "write"

	lookupType                = "type"
	valueNodeCacheType        = "valueNodeCache"
	intermediateNodeCacheType = "intermediateNodeCache"
	viewChangesValueType      = "viewChangesValue"
	viewChangesNodeType       = "viewChangesNode"

	lookupResult = "result"
	hitResult    = "hit"
	missResult   = "miss"
)

var (
	_ metrics = (*prometheusMetrics)(nil)
	_ metrics = (*mockMetrics)(nil)

	ioLabels     = []string{ioType}
	ioReadLabels = metrics.Labels{
		ioType: readType,
	}
	ioWriteLabels = metrics.Labels{
		ioType: writeType,
	}

	lookupLabels            = []string{lookupType, lookupResult}
	valueNodeCacheHitLabels = metrics.Labels{
		lookupType:   valueNodeCacheType,
		lookupResult: hitResult,
	}
	valueNodeCacheMissLabels = metrics.Labels{
		lookupType:   valueNodeCacheType,
		lookupResult: missResult,
	}
	intermediateNodeCacheHitLabels = metrics.Labels{
		lookupType:   intermediateNodeCacheType,
		lookupResult: hitResult,
	}
	intermediateNodeCacheMissLabels = metrics.Labels{
		lookupType:   intermediateNodeCacheType,
		lookupResult: missResult,
	}
	viewChangesValueHitLabels = metrics.Labels{
		lookupType:   viewChangesValueType,
		lookupResult: hitResult,
	}
	viewChangesValueMissLabels = metrics.Labels{
		lookupType:   viewChangesValueType,
		lookupResult: missResult,
	}
	viewChangesNodeHitLabels = metrics.Labels{
		lookupType:   viewChangesNodeType,
		lookupResult: hitResult,
	}
	viewChangesNodeMissLabels = metrics.Labels{
		lookupType:   viewChangesNodeType,
		lookupResult: missResult,
	}
)

type metrics interface {
	HashCalculated()
	DatabaseNodeRead()
	DatabaseNodeWrite()
	ValueNodeCacheHit()
	ValueNodeCacheMiss()
	IntermediateNodeCacheHit()
	IntermediateNodeCacheMiss()
	ViewChangesValueHit()
	ViewChangesValueMiss()
	ViewChangesNodeHit()
	ViewChangesNodeMiss()
}

type prometheusMetrics struct {
	hashes metrics.Counter
	io     metrics.CounterVec
	lookup metrics.CounterVec
}

func newMetrics(namespace string, reg metrics.Registerer) (metrics, error) {
	if reg == nil {
		return &mockMetrics{}, nil
	}
	m := prometheusMetrics{
		hashes: metrics.NewCounter(metrics.CounterOpts{
			Namespace: namespace,
			Name:      "hashes",
			Help:      "cumulative number of nodes hashed",
		}),
		io: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: namespace,
			Name:      "io",
			Help:      "cumulative number of operations performed to the db",
		}, ioLabels),
		lookup: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: namespace,
			Name:      "lookup",
			Help:      "cumulative number of in-memory lookups performed",
		}, lookupLabels),
	}
	err := errors.Join(
		reg.Register(m.hashes),
		reg.Register(m.io),
		reg.Register(m.lookup),
	)
	return &m, err
}

func (m *prometheusMetrics) HashCalculated() {
	m.hashes.Inc()
}

func (m *prometheusMetrics) DatabaseNodeRead() {
	m.io.With(ioReadLabels).Inc()
}

func (m *prometheusMetrics) DatabaseNodeWrite() {
	m.io.With(ioWriteLabels).Inc()
}

func (m *prometheusMetrics) ValueNodeCacheHit() {
	m.lookup.With(valueNodeCacheHitLabels).Inc()
}

func (m *prometheusMetrics) ValueNodeCacheMiss() {
	m.lookup.With(valueNodeCacheMissLabels).Inc()
}

func (m *prometheusMetrics) IntermediateNodeCacheHit() {
	m.lookup.With(intermediateNodeCacheHitLabels).Inc()
}

func (m *prometheusMetrics) IntermediateNodeCacheMiss() {
	m.lookup.With(intermediateNodeCacheMissLabels).Inc()
}

func (m *prometheusMetrics) ViewChangesValueHit() {
	m.lookup.With(viewChangesValueHitLabels).Inc()
}

func (m *prometheusMetrics) ViewChangesValueMiss() {
	m.lookup.With(viewChangesValueMissLabels).Inc()
}

func (m *prometheusMetrics) ViewChangesNodeHit() {
	m.lookup.With(viewChangesNodeHitLabels).Inc()
}

func (m *prometheusMetrics) ViewChangesNodeMiss() {
	m.lookup.With(viewChangesNodeMissLabels).Inc()
}

type mockMetrics struct {
	lock                      sync.Mutex
	hashCount                 int64
	nodeReadCount             int64
	nodeWriteCount            int64
	valueNodeCacheHit         int64
	valueNodeCacheMiss        int64
	intermediateNodeCacheHit  int64
	intermediateNodeCacheMiss int64
	viewChangesValueHit       int64
	viewChangesValueMiss      int64
	viewChangesNodeHit        int64
	viewChangesNodeMiss       int64
}

func (m *mockMetrics) HashCalculated() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.hashCount++
}

func (m *mockMetrics) DatabaseNodeRead() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.nodeReadCount++
}

func (m *mockMetrics) DatabaseNodeWrite() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.nodeWriteCount++
}

func (m *mockMetrics) ValueNodeCacheHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.valueNodeCacheHit++
}

func (m *mockMetrics) ValueNodeCacheMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.valueNodeCacheMiss++
}

func (m *mockMetrics) IntermediateNodeCacheHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.intermediateNodeCacheHit++
}

func (m *mockMetrics) IntermediateNodeCacheMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.intermediateNodeCacheMiss++
}

func (m *mockMetrics) ViewChangesValueHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewChangesValueHit++
}

func (m *mockMetrics) ViewChangesValueMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewChangesValueMiss++
}

func (m *mockMetrics) ViewChangesNodeHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewChangesNodeHit++
}

func (m *mockMetrics) ViewChangesNodeMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewChangesNodeMiss++
}
