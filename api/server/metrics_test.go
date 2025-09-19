// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewMetrics(t *testing.T) {
	// Create a new registry for testing
	registry := prometheus.NewRegistry()

	// Create metrics
	metrics, err := newMetrics(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics: %v", err)
	}

	// Verify metrics are not nil
	if metrics == nil {
		t.Fatal("metrics should not be nil")
	}
	if metrics.requests == nil {
		t.Fatal("requests metric should not be nil")
	}
	if metrics.duration == nil {
		t.Fatal("duration metric should not be nil")
	}
	if metrics.inflight == nil {
		t.Fatal("inflight metric should not be nil")
	}

	// Test basic operations to ensure they work
	metrics.requests.WithLabelValues("GET", "/test").Inc()
	metrics.duration.WithLabelValues("POST", "/api").Observe(0.5)
	metrics.inflight.Inc()
	metrics.inflight.Dec()
}

func TestMetricsRegistration(t *testing.T) {
	// Create a new registry
	registry := prometheus.NewRegistry()

	// First registration should succeed
	_, err := newMetrics(registry)
	if err != nil {
		t.Fatalf("First registration failed: %v", err)
	}

	// Second registration with same names should fail
	_, err = newMetrics(registry)
	if err == nil {
		t.Fatal("Second registration should have failed due to duplicate metrics")
	}
}