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
	hitLabels    = metrics.Labels{
		resultLabel: hitResult,
	}
	missLabels = metrics.Labels{
		resultLabel: missResult,
	}
)

type metricsImpl struct {
	getCount metrics.CounterVec
	getTime  metrics.GaugeVec

	putCount metrics.Counter
	putTime  metrics.Gauge

	len           metrics.Gauge
	portionFilled metrics.Gauge
}

func newMetrics(
	namespace string,
	reg metrics.Registerer,
) (*metricsImpl, error) {
	m := &metricsImpl{
		getCount: metrics.NewCounterVec(
			metrics.CounterOpts{
				Namespace: namespace,
				Name:      "get_count",
				Help:      "number of get calls",
			},
			resultLabels,
		),
		getTime: metrics.NewGaugeVec(
			metrics.GaugeOpts{
				Namespace: namespace,
				Name:      "get_time",
				Help:      "time spent (ns) in get calls",
			},
			resultLabels,
		),
		putCount: metrics.NewCounter(metrics.CounterOpts{
			Namespace: namespace,
			Name:      "put_count",
			Help:      "number of put calls",
		}),
		putTime: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: namespace,
			Name:      "put_time",
			Help:      "time spent (ns) in put calls",
		}),
		len: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: namespace,
			Name:      "len",
			Help:      "number of entries",
		}),
		portionFilled: metrics.NewGauge(metrics.GaugeOpts{
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

