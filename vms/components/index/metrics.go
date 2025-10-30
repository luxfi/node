// Copyright (C) 2019-2025, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package index

import "github.com/prometheus/client_golang/prometheus"

type indexMetrics struct {
	numObjects    prometheus.Gauge
	numTxsIndexed prometheus.Counter
}

func newMetrics(registerer prometheus.Registerer) (*indexMetrics, error) {
	m := &indexMetrics{
		numObjects: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "index_num_objects",
			Help: "Number of objects in the index",
		}),
		numTxsIndexed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "index_txs_indexed",
			Help: "Number of transactions indexed",
		}),
	}
	if registerer != nil {
		if err := registerer.Register(m.numObjects); err != nil {
			return nil, err
		}
		if err := registerer.Register(m.numTxsIndexed); err != nil {
			return nil, err
		}
	}
	return m, nil
}
