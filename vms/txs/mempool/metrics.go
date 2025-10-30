// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/metric"
)

type mempoolMetrics struct {
	numTxs prometheus.Gauge
	bytesUsed prometheus.Gauge
}

func newMetrics(registerer metric.Registerer) (*mempoolMetrics, error) {
	m := &mempoolMetrics{
		numTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "mempool_num_txs",
			Help: "Number of transactions in mempool",
		}),
		bytesUsed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "mempool_bytes_used",
			Help: "Number of bytes used by mempool",
		}),
	}

	err := registerer.Register(m.numTxs)
	if err != nil {
		return nil, err
	}
	err = registerer.Register(m.bytesUsed)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (m *mempoolMetrics) Update(numTxs, bytesAvailable int) {
	m.numTxs.Set(float64(numTxs))
	m.bytesUsed.Set(float64(maxMempoolSize - bytesAvailable))
}

// NewMetrics creates a new Metrics instance
func NewMetrics(namespace string, registerer metric.Registerer) (Metrics, error) {
	return newMetrics(registerer)
}
