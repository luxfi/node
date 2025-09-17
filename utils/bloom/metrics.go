// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/metric"
)

// Metrics is a collection of commonly useful metrics when using a long-lived
// bloom filter.
type Metrics struct {
	Count      metrics.Gauge
	NumHashes  metrics.Gauge
	NumEntries metrics.Gauge
	MaxCount   metrics.Gauge
	ResetCount metrics.Counter
}

func NewMetrics(
	namespace string,
	registerer metrics.Registerer,
) (*Metrics, error) {
	m := &Metrics{
		Count: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: namespace,
			Name:      "count",
			Help:      "Number of additions that have been performed to the bloom",
		}),
		NumHashes: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: namespace,
			Name:      "hashes",
			Help:      "Number of hashes in the bloom",
		}),
		NumEntries: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: namespace,
			Name:      "entries",
			Help:      "Number of bytes allocated to slots in the bloom",
		}),
		MaxCount: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: namespace,
			Name:      "max_count",
			Help:      "Maximum number of additions that should be performed to the bloom before resetting",
		}),
		ResetCount: metrics.NewCounter(metrics.CounterOpts{
			Namespace: namespace,
			Name:      "reset_count",
			Help:      "Number times the bloom has been reset",
		}),
	}
	err := errors.Join(
		registerer.Register(m.Count.(prometheus.Collector)),
		registerer.Register(m.NumHashes.(prometheus.Collector)),
		registerer.Register(m.NumEntries.(prometheus.Collector)),
		registerer.Register(m.MaxCount.(prometheus.Collector)),
		registerer.Register(m.ResetCount.(prometheus.Collector)),
	)
	return m, err
}

// Reset the metrics to align with the provided bloom filter and max count.
func (m *Metrics) Reset(newFilter *Filter, maxCount int) {
	m.Count.Set(float64(newFilter.Count()))
	m.NumHashes.Set(float64(len(newFilter.hashSeeds)))
	m.NumEntries.Set(float64(len(newFilter.entries)))
	m.MaxCount.Set(float64(maxCount))
	m.ResetCount.Inc()
}
