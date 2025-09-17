// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"github.com/prometheus/client_golang/prometheus"
	"errors"
	"time"

	"github.com/luxfi/metric"
)

type metricsImpl struct {
	txsIssuedCounter      metrics.Counter
	txIssuanceLatency     metrics.Histogram
	txConfirmationLatency metrics.Histogram
	txTotalLatency        metrics.Histogram
}

func newMetrics(namespace string, registry metrics.Registry) (metricsImpl, error) {
	m := metricsImpl{
		txsIssuedCounter: metrics.NewCounter(metrics.CounterOpts{
			Namespace: namespace,
			Name:      "txs_issued",
			Help:      "Number of transactions issued",
		}),
		txIssuanceLatency: metrics.NewHistogram(metrics.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_issuance_latency",
			Help:      "Issuance latency of transactions",
		}),
		txConfirmationLatency: metrics.NewHistogram(metrics.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_confirmation_latency",
			Help:      "Confirmation latency of transactions",
		}),
		txTotalLatency: metrics.NewHistogram(metrics.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_total_latency",
			Help:      "Total latency of transactions",
		}),
	}

	if err := errors.Join(
		registry.Register(m.txsIssuedCounter.(prometheus.Collector)),
		registry.Register(m.txIssuanceLatency.(prometheus.Collector)),
		registry.Register(m.txConfirmationLatency.(prometheus.Collector)),
		registry.Register(m.txTotalLatency.(prometheus.Collector)),
	); err != nil {
		return metricsImpl{}, err
	}

	return m, nil
}

func (m metricsImpl) issue(d time.Duration) {
	m.txsIssuedCounter.Inc()
	m.txIssuanceLatency.Observe(float64(d.Milliseconds()))
}

func (m metricsImpl) accept(confirmationDuration time.Duration, totalDuration time.Duration) {
	m.txConfirmationLatency.Observe(float64(confirmationDuration.Milliseconds()))
	m.txTotalLatency.Observe(float64(totalDuration.Milliseconds()))
}
