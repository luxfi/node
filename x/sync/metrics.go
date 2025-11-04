// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"errors"
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
	m := metricsImpl{
		requestsFailed: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "requests_failed",
			Help:      "cumulative amount of failed proof requests",
		}),
		requestsMade: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "requests_made",
			Help:      "cumulative amount of proof requests made",
		}),
		requestsSucceeded: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "requests_succeeded",
			Help:      "cumulative amount of proof requests that were successful",
		}),
	}
	err := errors.Join(
		reg.Register(m.requestsFailed),
		reg.Register(m.requestsMade),
		reg.Register(m.requestsSucceeded),
	)
	return &m, err
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
