// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import "github.com/prometheus/client_golang/prometheus"

type healthMetrics struct {
	// failingChecks keeps track of the number of check failing
	failingChecks *prometheus.GaugeVec
}

func newMetrics(namespace string, registerer prometheus.Registerer) (*healthMetrics, error) {
	metrics := &healthMetrics{
		failingChecks: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "checks_failing",
				Help:      "number of currently failing health checks",
			},
			[]string{"tag"},
		),
	}
	metrics.failingChecks.WithLabelValues(AllTag).Set(0)
	metrics.failingChecks.WithLabelValues(ApplicationTag).Set(0)
	return metrics, registerer.Register(metrics.failingChecks)
}
