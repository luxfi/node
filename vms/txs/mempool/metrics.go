// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"

	"github.com/luxfi/metric"
)

var _ Metrics = (*metricsImpl)(nil)

type metricsImpl struct {
	numTxs               metric.Gauge
	bytesAvailableMetric metric.Gauge
}

func NewMetrics(namespace string, registerer metric.Registerer) (*metricsImpl, error) {
	m := &metricsImpl{
		numTxs: metric.NewGauge(metric.GaugeOpts{
			Namespace: namespace,
			Name:      "count",
			Help:      "Number of transactions in the mempool",
		}),
		bytesAvailableMetric: metric.NewGauge(metric.GaugeOpts{
			Namespace: namespace,
			Name:      "bytes_available",
			Help:      "Number of bytes of space currently available in the mempool",
		}),
	}

	err := errors.Join(
		registerer.Register(m.numTxs),
		registerer.Register(m.bytesAvailableMetric),
	)

	return m, err
}

func (m *metricsImpl) Update(numTxs, bytesAvailable int) {
	m.numTxs.Set(float64(numTxs))
	m.bytesAvailableMetric.Set(float64(bytesAvailable))
}
