// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/luxfi/metric"
)

type APIInterceptor interface {
	InterceptRequest(i *rpc.RequestInfo) *http.Request
	AfterRequest(i *rpc.RequestInfo)
}

type contextKey int

const requestTimestampKey contextKey = iota

type apiInterceptor struct {
	requestDurationCount metrics.CounterVec
	requestDurationSum   metrics.GaugeVec
	requestErrors        metrics.CounterVec
}

func NewAPIInterceptor(registerer metrics.Registerer) (APIInterceptor, error) {
	requestDurationCount := metrics.NewCounterVec(
		metrics.CounterOpts{
			Name: "request_duration_count",
			Help: "Number of times this type of request was made",
		},
		[]string{"method"},
	)
	requestDurationSum := metrics.NewGaugeVec(
		metrics.GaugeOpts{
			Name: "request_duration_sum",
			Help: "Amount of time in nanoseconds that has been spent handling this type of request",
		},
		[]string{"method"},
	)
	requestErrors := metrics.NewCounterVec(
		metrics.CounterOpts{
			Name: "request_error_count",
		},
		[]string{"method"},
	)

	err := errors.Join(
		registerer.Register(requestDurationCount.(prometheus.Collector)),
		registerer.Register(requestDurationSum.(prometheus.Collector)),
		registerer.Register(requestErrors.(prometheus.Collector)),
	)
	return &apiInterceptor{
		requestDurationCount: requestDurationCount,
		requestDurationSum:   requestDurationSum,
		requestErrors:        requestErrors,
	}, err
}

func (*apiInterceptor) InterceptRequest(i *rpc.RequestInfo) *http.Request {
	ctx := i.Request.Context()
	ctx = context.WithValue(ctx, requestTimestampKey, time.Now())
	return i.Request.WithContext(ctx)
}

func (apr *apiInterceptor) AfterRequest(i *rpc.RequestInfo) {
	timestampIntf := i.Request.Context().Value(requestTimestampKey)
	timestamp, ok := timestampIntf.(time.Time)
	if !ok {
		return
	}

	durationMetricCount := apr.requestDurationCount.With(metrics.Labels{
		"method": i.Method,
	})
	durationMetricCount.Inc()

	duration := time.Since(timestamp)
	durationMetricSum := apr.requestDurationSum.With(metrics.Labels{
		"method": i.Method,
	})
	durationMetricSum.Add(float64(duration))

	if i.Error != nil {
		errMetric := apr.requestErrors.With(metrics.Labels{
			"method": i.Method,
		})
		errMetric.Inc()
	}
}
