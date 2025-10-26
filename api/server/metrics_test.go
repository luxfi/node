// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"testing"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"
)

func TestNewMetrics(t *testing.T) {
	// Create a new registry for testing
	reg := metric.NewRegistry()

	// Create metrics
	metrics, err := newMetrics(reg)
	require.NoError(t, err)
	require.NotNil(t, metrics)
	require.NotNil(t, metric.requests)
	require.NotNil(t, metric.duration)
	require.NotNil(t, metric.inflight)

	// Test basic operations to ensure they work
	metric.requests.WithLabelValues("GET", "/test").Inc()
	metric.duration.WithLabelValues("POST", "/api").Observe(0.5)
	metric.inflight.Inc()
	metric.inflight.Dec()
}

func TestMetricsRegistrationFailure(t *testing.T) {
	// Test that duplicate registration fails
	reg := metric.NewRegistry()

	// First registration should succeed
	metrics1, err := newMetrics(reg)
	require.NoError(t, err)
	require.NotNil(t, metrics1)

	// Second registration should fail due to duplicate metrics
	metrics2, err := newMetrics(reg)
	require.Error(t, err, "second registration should fail due to duplicate metrics")
	require.Nil(t, metrics2)
}

func TestMetricsOperations(t *testing.T) {
	reg := metric.NewRegistry()

	metrics, err := newMetrics(reg)
	require.NoError(t, err)

	// Test various label combinations
	testCases := []struct {
		method   string
		endpoint string
		duration float64
	}{
		{"GET", "/health", 0.001},
		{"POST", "/api/v1/users", 0.123},
		{"PUT", "/api/v1/users/123", 0.456},
		{"DELETE", "/api/v1/users/456", 0.789},
		{"GET", "/metrics", 0.002},
	}

	for _, tc := range testCases {
		// Increment request counter
		metric.requests.WithLabelValues(tc.method, tc.endpoint).Inc()

		// Observe duration
		metric.duration.WithLabelValues(tc.method, tc.endpoint).Observe(tc.duration)

		// Simulate inflight request
		metric.inflight.Inc()
		metric.inflight.Dec()
	}

	// Operations completed successfully without panics
}
