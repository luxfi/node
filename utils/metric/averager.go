// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"errors"
	"fmt"

	"github.com/luxfi/metric"

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

func NewAverager(name, desc string, reg metrics.Registerer) (Averager, error) {
	errs := wrappers.Errs{}
	a := NewAveragerWithErrs(name, desc, reg, &errs)
	return a, errs.Err
}

func NewAveragerWithErrs(name, desc string, reg metrics.Registerer, errs *wrappers.Errs) Averager {
	a := averager{
		count: metrics.NewCounter(metrics.CounterOpts{
			Name: AppendNamespace(name, "count"),
			Help: "Total # of observations of " + desc,
		}),
		sum: metrics.NewGauge(metrics.GaugeOpts{
			Name: AppendNamespace(name, "sum"),
			Help: "Sum of " + desc,
		}),
	}

	if err := reg.Register(a.count.(prometheus.Collector)); err != nil {
		errs.Add(fmt.Errorf("%w: %w", ErrFailedRegistering, err))
	}
	if err := reg.Register(a.sum.(prometheus.Collector)); err != nil {
		errs.Add(fmt.Errorf("%w: %w", ErrFailedRegistering, err))
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
