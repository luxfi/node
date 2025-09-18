// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"github.com/luxfi/metric"
	"github.com/prometheus/client_golang/prometheus"
)

type serverMetrics struct {
	requests  metric.CounterVec
	duration  *prometheus.HistogramVec
	inflight  metric.Gauge
}

func newMetrics(registerer metric.Registerer) (*serverMetrics, error) {
	m := &serverMetrics{
		requests: metric.NewCounterVec(
			metric.CounterOpts{
				Name: "api_requests_total",
				Help: "Total number of API requests",
			},
			[]string{"method", "endpoint"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "api_request_duration_seconds",
				Help: "API request duration in seconds",
			},
			[]string{"method", "endpoint"},
		),
		inflight: metric.NewGauge(
			metric.GaugeOpts{
				Name: "api_requests_inflight",
				Help: "Number of inflight API requests",
			},
		),
	}
	
	if err := registerer.Register(m.requests); err != nil {
		return nil, err
	}
	if err := registerer.Register(m.duration); err != nil {
		return nil, err
	}
	if err := registerer.Register(m.inflight); err != nil {
		return nil, err
	}
	
	return m, nil
}

