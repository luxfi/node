// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"
	"sync"

	"github.com/luxfi/metric"
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
	_ merkleDBMetrics = (*metricsImpl)(nil)
	_ merkleDBMetrics = (*mockMetrics)(nil)

	ioLabels     = []string{ioType}
	ioReadLabels = metric.Labels{
		ioType: readType,
	}
	ioWriteLabels = metric.Labels{
		ioType: writeType,
	}

	lookupLabels            = []string{lookupType, lookupResult}
	valueNodeCacheHitLabels = metric.Labels{
		lookupType:   valueNodeCacheType,
		lookupResult: hitResult,
	}
	valueNodeCacheMissLabels = metric.Labels{
		lookupType:   valueNodeCacheType,
		lookupResult: missResult,
	}
	intermediateNodeCacheHitLabels = metric.Labels{
		lookupType:   intermediateNodeCacheType,
		lookupResult: hitResult,
	}
	intermediateNodeCacheMissLabels = metric.Labels{
		lookupType:   intermediateNodeCacheType,
		lookupResult: missResult,
	}
	viewChangesValueHitLabels = metric.Labels{
		lookupType:   viewChangesValueType,
		lookupResult: hitResult,
	}
	viewChangesValueMissLabels = metric.Labels{
		lookupType:   viewChangesValueType,
		lookupResult: missResult,
	}
	viewChangesNodeHitLabels = metric.Labels{
		lookupType:   viewChangesNodeType,
		lookupResult: hitResult,
	}
	viewChangesNodeMissLabels = metric.Labels{
		lookupType:   viewChangesNodeType,
		lookupResult: missResult,
	}
)

type merkleDBMetrics interface {
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

type metricsImpl struct {
	hashes metric.Counter
	io     metric.CounterVec
	lookup metric.CounterVec
}

func newMetrics(prefix string, reg metric.Registerer) (merkleDBMetrics, error) {
	// A nil registerer means metrics are disabled; return a no-op implementation.
	if reg == nil {
		return &mockMetrics{}, nil
	}

	namespace := metric.AppendNamespace(prefix, "merkledb")
	m := metricsImpl{
		hashes: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "hashes",
			Help:      "cumulative number of nodes hashed",
		}),
		io: metric.NewCounterVec(metric.CounterOpts{
			Namespace: namespace,
			Name:      "io",
			Help:      "cumulative number of operations performed to the db",
		}, ioLabels),
		lookup: metric.NewCounterVec(metric.CounterOpts{
			Namespace: namespace,
			Name:      "lookup",
			Help:      "cumulative number of in-memory lookups performed",
		}, lookupLabels),
	}
	err := errors.Join(
		reg.Register(metric.AsCollector(m.hashes)),
		reg.Register(metric.AsCollector(m.io)),
		reg.Register(metric.AsCollector(m.lookup)),
	)
	return &m, err
}

func (m *metricsImpl) HashCalculated() {
	m.hashes.Inc()
}

func (m *metricsImpl) DatabaseNodeRead() {
	m.io.With(ioReadLabels).Inc()
}

func (m *metricsImpl) DatabaseNodeWrite() {
	m.io.With(ioWriteLabels).Inc()
}

func (m *metricsImpl) ValueNodeCacheHit() {
	m.lookup.With(valueNodeCacheHitLabels).Inc()
}

func (m *metricsImpl) ValueNodeCacheMiss() {
	m.lookup.With(valueNodeCacheMissLabels).Inc()
}

func (m *metricsImpl) IntermediateNodeCacheHit() {
	m.lookup.With(intermediateNodeCacheHitLabels).Inc()
}

func (m *metricsImpl) IntermediateNodeCacheMiss() {
	m.lookup.With(intermediateNodeCacheMissLabels).Inc()
}

func (m *metricsImpl) ViewChangesValueHit() {
	m.lookup.With(viewChangesValueHitLabels).Inc()
}

func (m *metricsImpl) ViewChangesValueMiss() {
	m.lookup.With(viewChangesValueMissLabels).Inc()
}

func (m *metricsImpl) ViewChangesNodeHit() {
	m.lookup.With(viewChangesNodeHitLabels).Inc()
}

func (m *metricsImpl) ViewChangesNodeMiss() {
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
