// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"github.com/prometheus/client_golang/prometheus"
	"errors"

	"github.com/luxfi/metric"
)

var _ Metrics = (*metricsImpl)(nil)

type metricsImpl struct {
	numTxs               metrics.Gauge
	bytesAvailableMetric metrics.Gauge
}

func NewMetrics(namespace string, registerer metrics.Registerer) (*metricsImpl, error) {
	m := &metricsImpl{
		numTxs: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: namespace,
			Name:      "count",
			Help:      "Number of transactions in the mempool",
		}),
		bytesAvailableMetric: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: namespace,
			Name:      "bytes_available",
			Help:      "Number of bytes of space currently available in the mempool",
		}),
	}

	err := errors.Join(
		registerer.Register(m.numTxs.(prometheus.Collector)),
		registerer.Register(m.bytesAvailableMetric.(prometheus.Collector)),
	)

	return m, err
}

func (m *metricsImpl) Update(numTxs, bytesAvailable int) {
	m.numTxs.Set(float64(numTxs))
	m.bytesAvailableMetric.Set(float64(bytesAvailable))
}

