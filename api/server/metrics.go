// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"errors"
	"net/http"
	"time"

	metrics "github.com/luxfi/metric"
)

type metrics struct {
	numProcessing metrics.GaugeVec
	numCalls      metrics.CounterVec
	totalDuration metrics.GaugeVec
}

func newMetrics(registerer metrics.Registerer) (*metrics, error) {
	m := &metrics{
		numProcessing: metrics.NewGaugeVec(
			metrics.GaugeOpts{
				Name: "calls_processing",
				Help: "The number of calls this API is currently processing",
			},
			[]string{"base"},
		),
		numCalls: metrics.NewCounterVec(
			metrics.CounterOpts{
				Name: "calls",
				Help: "The number of calls this API has processed",
			},
			[]string{"base"},
		),
		totalDuration: metrics.NewGaugeVec(
			metrics.GaugeOpts{
				Name: "calls_duration",
				Help: "The total amount of time, in nanoseconds, spent handling API calls",
			},
			[]string{"base"},
		),
	}

	err := errors.Join(
		registerer.Register(m.numProcessing),
		registerer.Register(m.numCalls),
		registerer.Register(m.totalDuration),
	)
	return m, err
}

func (m *metrics) wrapHandler(chainName string, handler http.Handler) http.Handler {
	numProcessing := m.numProcessing.WithLabelValues(chainName)
	numCalls := m.numCalls.WithLabelValues(chainName)
	totalDuration := m.totalDuration.WithLabelValues(chainName)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		numProcessing.Inc()

		defer func() {
			numProcessing.Dec()
			numCalls.Inc()
			totalDuration.Add(float64(time.Since(startTime)))
		}()

		handler.ServeHTTP(w, r)
	})
}
