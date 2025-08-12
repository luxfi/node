// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"github.com/luxfi/metric"
)

// Averager tracks average values using count and sum
type Averager interface {
	Observe(float64)
}

type averager struct {
	count metrics.Counter
	sum   metrics.Gauge
}

func NewAverager(name, desc string, reg metrics.Registry) (Averager, error) {
	// Create a metrics instance using the registry
	m := metrics.NewWithRegistry("", reg)

	a := &averager{
		count: m.NewCounter(name+"_count", "Total # of observations of "+desc),
		sum:   m.NewGauge(name+"_sum", "Sum of "+desc),
	}

	return a, nil
}

func (a *averager) Observe(v float64) {
	a.count.Inc()
	a.sum.Add(v)
}