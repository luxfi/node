// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"errors"

	"github.com/luxfi/metric"
)

type Metrics struct {
	txsIssuedCounter    metric.Counter
	txsConfirmedCounter metric.Counter
	txsFailedCounter    metric.Counter
	txLatency           metric.Histogram
}

func NewMetrics(registry *metric.Registry) (*Metrics, error) {
	m := &Metrics{
		txsIssuedCounter: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "txs_issued",
			Help:      "Number of transactions issued",
		}),
		txsConfirmedCounter: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "txs_confirmed",
			Help:      "Number of transactions confirmed",
		}),
		txsFailedCounter: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "txs_failed",
			Help:      "Number of transactions failed",
		}),
		txLatency: metric.NewHistogram(metric.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_latency",
			Help:      "Latency of transactions",
		}),
	}

	if err := errors.Join(
		registry.Register(m.txsIssuedCounter),
		registry.Register(m.txsConfirmedCounter),
		registry.Register(m.txsFailedCounter),
		registry.Register(m.txLatency),
	); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Metrics) IncIssuedTx() {
	m.txsIssuedCounter.Inc()
}

func (m *Metrics) RecordConfirmedTx(latencyMS float64) {
	m.txsConfirmedCounter.Inc()
	m.txLatency.Observe(latencyMS)
}

func (m *Metrics) RecordFailedTx(latencyMS float64) {
	m.txsFailedCounter.Inc()
	m.txLatency.Observe(latencyMS)
}
