// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"

	"github.com/luxfi/metric"
	"github.com/luxfi/metric"
	"github.com/luxfi/metric/promhttp"
)

type serverMetrics struct {
	requests *metric.CounterVec
	duration *metric.HistogramVec
	inflight metric.Gauge
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
		duration: metric.NewHistogramVec(
			metric.HistogramOpts{
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

func (m *serverMetrics) wrapHandler(chainName string, handler http.Handler) http.Handler {
	return promhttp.InstrumentHandlerInFlight(m.inflight,
		promhttp.InstrumentHandlerDuration(m.duration.MustCurryWith(metric.Labels{"endpoint": chainName}),
			promhttp.InstrumentHandlerCounter(m.requests.MustCurryWith(metric.Labels{"endpoint": chainName}),
				handler,
			),
		),
	)
}
