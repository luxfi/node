// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	metrics "github.com/luxfi/metric"
)

const (
	resultLabel = "result"
	hitResult   = "hit"
	missResult  = "miss"
)

var (
	resultLabels = []string{resultLabel}
	hitLabels    = metrics.Labels{
		resultLabel: hitResult,
	}
	missLabels = metrics.Labels{
		resultLabel: missResult,
	}
)

type cacheMetrics struct {
	getCount metrics.CounterVec
	getTime  metrics.GaugeVec

	putCount metrics.Counter
	putTime  metrics.Gauge

	len           metrics.Gauge
	portionFilled metrics.Gauge
}

func newMetrics(
	namespace string,
	registry metrics.Registry,
) (*cacheMetrics, error) {
	metricsInstance := metrics.NewWithRegistry(namespace, registry)

	m := &cacheMetrics{
		getCount: metricsInstance.NewCounterVec(
			"get_count",
			"number of get calls",
			resultLabels,
		),
		getTime: metricsInstance.NewGaugeVec(
			"get_time",
			"time spent (ns) in get calls",
			resultLabels,
		),
		putCount: metricsInstance.NewCounter(
			"put_count",
			"number of put calls",
		),
		putTime: metricsInstance.NewGauge(
			"put_time",
			"time spent (ns) in put calls",
		),
		len: metricsInstance.NewGauge(
			"len",
			"number of entries",
		),
		portionFilled: metricsInstance.NewGauge(
			"portion_filled",
			"fraction of cache filled",
		),
	}
	return m, nil
}