// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import "github.com/luxfi/metric"

type indexMetrics struct {
	numObjects    metric.Gauge
	numTxsIndexed metric.Counter
}

func newMetrics(registerer metric.Registerer) (*indexMetrics, error) {
	m := &indexMetrics{
		numObjects: metric.NewGauge(metric.GaugeOpts{
			Name: "index_num_objects",
			Help: "Number of objects in the index",
		}),
		numTxsIndexed: metric.NewCounter(metric.CounterOpts{
			Name: "index_txs_indexed",
			Help: "Number of transactions indexed",
		}),
	}
	if registerer != nil {
		if err := registerer.Register(metric.AsCollector(m.numObjects)); err != nil {
			return nil, err
		}
		if err := registerer.Register(metric.AsCollector(m.numTxsIndexed)); err != nil {
			return nil, err
		}
	}
	return m, nil
}
