// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"github.com/luxfi/metric"
)

type serverMetrics struct {
	requests  metric.CounterVec
	duration  interface{} // Keep as interface{} since metric.HistogramVec doesn't have a constructor
	inflight  metric.Gauge
}

func newMetrics(registerer metric.Registerer) (*serverMetrics, error) {
	// Create prometheus histogram directly since metric package doesn't provide wrapper
	promHistogramVec := metric.NewHistogramVec(
		metric.HistogramOpts{
			Name: "api_request_duration_seconds",
			Help: "API request duration in seconds",
		},
		[]string{"method", "endpoint"},
	)

	m := &serverMetrics{
		requests: metric.NewCounterVec(
			metric.CounterOpts{
				Name: "api_requests_total",
				Help: "Total number of API requests",
			},
			[]string{"method", "endpoint"},
		),
		duration: promHistogramVec,
		inflight: metric.NewGauge(
			metric.GaugeOpts{
				Name: "api_requests_inflight",
				Help: "Number of inflight API requests",
			},
		),
	}

	if err := registerer.Register(metric.AsCollector(m.requests)); err != nil {
		return nil, err
	}
	if err := registerer.Register(promHistogramVec); err != nil {
		return nil, err
	}
	if err := registerer.Register(metric.AsCollector(m.inflight)); err != nil {
		return nil, err
	}

	return m, nil
}
