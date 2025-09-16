// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"errors"
	"sync"

	metrics "github.com/luxfi/metric"
)

var (
	_ SyncMetrics = (*mockMetrics)(nil)
	_ SyncMetrics = (*metrics)(nil)
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

type metrics struct {
	requestsFailed    metrics.Counter
	requestsMade      metrics.Counter
	requestsSucceeded metrics.Counter
}

func NewMetrics(namespace string, reg metrics.Registerer) (SyncMetrics, error) {
	m := metrics{
		requestsFailed: metrics.NewCounter(metrics.CounterOpts{
			Namespace: namespace,
			Name:      "requests_failed",
			Help:      "cumulative amount of failed proof requests",
		}),
		requestsMade: metrics.NewCounter(metrics.CounterOpts{
			Namespace: namespace,
			Name:      "requests_made",
			Help:      "cumulative amount of proof requests made",
		}),
		requestsSucceeded: metrics.NewCounter(metrics.CounterOpts{
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

func (m *metrics) RequestFailed() {
	m.requestsFailed.Inc()
}

func (m *metrics) RequestMade() {
	m.requestsMade.Inc()
}

func (m *metrics) RequestSucceeded() {
	m.requestsSucceeded.Inc()
}
