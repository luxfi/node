// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/metric"
)

type healthMetrics struct {
	// failingChecks keeps track of the number of check failing
	failingChecks metric.GaugeVec
}

func newMetrics(namespace string, registerer metric.Registerer) (*healthMetrics, error) {
	metric := &healthMetrics{
		failingChecks: metric.NewGaugeVec(
			metric.GaugeOpts{
				Namespace: namespace,
				Name:      "checks_failing",
				Help:      "number of currently failing health checks",
			},
			[]string{"tag"},
		),
	}
	metric.failingChecks.WithLabelValues(AllTag).Set(0)
	metric.failingChecks.WithLabelValues(ApplicationTag).Set(0)
	return metric, registerer.Register(metric.failingChecks.(prometheus.Collector))
}
