// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"errors"

	metrics "github.com/luxfi/metric"

	"github.com/luxfi/node/utils/wrappers"
)

var ErrFailedRegistering = errors.New("failed registering metric")

type Averager interface {
	Observe(float64)
}

type averager struct {
	count metrics.Counter
	sum   metrics.Gauge
}

func NewAverager(name, desc string, registry metrics.Registry) (Averager, error) {
	errs := wrappers.Errs{}
	a := NewAveragerWithErrs(name, desc, registry, &errs)
	return a, errs.Err
}

func NewAveragerWithErrs(name, desc string, registry metrics.Registry, errs *wrappers.Errs) Averager {
	metricsInstance := metrics.NewWithRegistry("", registry)

	a := averager{
		count: metricsInstance.NewCounter(
			AppendNamespace(name, "count"),
			"Total # of observations of " + desc,
		),
		sum: metricsInstance.NewGauge(
			AppendNamespace(name, "sum"),
			"Sum of " + desc,
		),
	}

	return &a
}

func (a *averager) Observe(v float64) {
	a.count.Inc()
	a.sum.Add(v)
}

type noAverager struct{}

func NewNoAverager() Averager {
	return noAverager{}
}

func (noAverager) Observe(float64) {}
