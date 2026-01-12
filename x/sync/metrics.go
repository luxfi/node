// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"sync"

	"github.com/luxfi/metric"
)

var (
	_ SyncMetrics = (*mockMetrics)(nil)
	_ SyncMetrics = (*metricsImpl)(nil)
)

type SyncMetrics interface {
	RequestFailed()
	RequestMade()
	RequestSucceeded()
}

type mockMetrics struct {
	lock              sync.Mutex
	requestsFailed    int
	requestsMade      int
	requestsSucceeded int
}

func (m *mockMetrics) RequestFailed() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.requestsFailed++
}

func (m *mockMetrics) RequestMade() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.requestsMade++
}

func (m *mockMetrics) RequestSucceeded() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.requestsSucceeded++
}

type metricsImpl struct {
	requestsFailed    metric.Counter
	requestsMade      metric.Counter
	requestsSucceeded metric.Counter
}

func NewMetrics(namespace string, reg metric.Registerer) (SyncMetrics, error) {
	if reg == nil {
		reg = metric.NewNoOpRegistry()
	}
	m := metricsImpl{
		requestsFailed: reg.NewCounter(
			metric.AppendNamespace(namespace, "requests_failed"),
			"cumulative amount of failed proof requests",
		),
		requestsMade: reg.NewCounter(
			metric.AppendNamespace(namespace, "requests_made"),
			"cumulative amount of proof requests made",
		),
		requestsSucceeded: reg.NewCounter(
			metric.AppendNamespace(namespace, "requests_succeeded"),
			"cumulative amount of proof requests that were successful",
		),
	}
	return &m, nil
}

func (m *metricsImpl) RequestFailed() {
	m.requestsFailed.Inc()
}

func (m *metricsImpl) RequestMade() {
	m.requestsMade.Inc()
}

func (m *metricsImpl) RequestSucceeded() {
	m.requestsSucceeded.Inc()
}
