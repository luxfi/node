// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"github.com/luxfi/metric"
	"github.com/luxfi/node/utils/wrappers"
)

// Averager tracks average values using count and sum
type Averager interface {
	Observe(float64)
}

type averager struct {
	count metrics.Counter
	sum   metrics.Gauge
}

func NewAveragerWithErrs(name, desc string, reg metrics.Registry, errs *wrappers.Errs) Averager {
	// Create a metrics instance using the registry
	m := metrics.NewWithRegistry("", reg)

	a := &averager{
		count: m.NewCounter(name+"_count", "Total # of observations of "+desc),
		sum:   m.NewGauge(name+"_sum", "Sum of "+desc),
	}

	return a
}

func (a *averager) Observe(v float64) {
	a.count.Inc()
	a.sum.Add(v)
}