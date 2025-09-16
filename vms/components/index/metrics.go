// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import "github.com/prometheus/client_golang/prometheus"

type indexMetrics struct {
	numObjects prometheus.Gauge
}

func newMetrics(registerer prometheus.Registerer) (*indexMetrics, error) {
	m := &indexMetrics{
		numObjects: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "index_num_objects",
			Help: "Number of objects in the index",
		}),
	}
	if err := registerer.Register(m.numObjects); err != nil {
		return nil, err
	}
	return m, nil
}
