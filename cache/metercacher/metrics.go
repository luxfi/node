// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metercacher

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/metric"
)

const (
	resultLabel = "result"
	hitResult   = "hit"
	missResult  = "miss"
)

var (
	resultLabels = []string{resultLabel}
	hitLabels    = metric.Labels{
		resultLabel: hitResult,
	}
	missLabels = metric.Labels{
		resultLabel: missResult,
	}
)

type metricsImpl struct {
	getCount metric.CounterVec
	getTime  metric.GaugeVec

	putCount metric.Counter
	putTime  metric.Gauge

	len           metric.Gauge
	portionFilled metric.Gauge
}

func newMetrics(
	namespace string,
	reg metric.Registerer,
) (*metricsImpl, error) {
	m := &metricsImpl{
		getCount: metric.NewCounterVec(
			metric.CounterOpts{
				Namespace: namespace,
				Name:      "get_count",
				Help:      "number of get calls",
			},
			resultLabels,
		),
		getTime: metric.NewGaugeVec(
			metric.GaugeOpts{
				Namespace: namespace,
				Name:      "get_time",
				Help:      "time spent (ns) in get calls",
			},
			resultLabels,
		),
		putCount: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "put_count",
			Help:      "number of put calls",
		}),
		putTime: metric.NewGauge(metric.GaugeOpts{
			Namespace: namespace,
			Name:      "put_time",
			Help:      "time spent (ns) in put calls",
		}),
		len: metric.NewGauge(metric.GaugeOpts{
			Namespace: namespace,
			Name:      "len",
			Help:      "number of entries",
		}),
		portionFilled: metric.NewGauge(metric.GaugeOpts{
			Namespace: namespace,
			Name:      "portion_filled",
			Help:      "fraction of cache filled",
		}),
	}
	err := errors.Join(
		reg.Register(m.getCount.(prometheus.Collector)),
		reg.Register(m.getTime.(prometheus.Collector)),
		reg.Register(m.putCount.(prometheus.Collector)),
		reg.Register(m.putTime.(prometheus.Collector)),
		reg.Register(m.len.(prometheus.Collector)),
		reg.Register(m.portionFilled.(prometheus.Collector)),
	)
	return m, err
}

