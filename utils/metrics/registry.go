// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	luxmetrics "github.com/luxfi/metrics"
)

// NewRegistry creates a new prometheus registry.
// This is a wrapper to avoid importing prometheus directly in test files.
func NewRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// NewTestRegistry creates a new prometheus registry for testing.
// Alias for NewRegistry to make intent clear in tests.
func NewTestRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// WrapPrometheusRegistry is deprecated - just use the registry directly
// since luxmetrics.Registry is now an alias for *prometheus.Registry
func WrapPrometheusRegistry(registry *prometheus.Registry) luxmetrics.Registry {
	// Since Registry is now an alias, we can return it directly
	return registry
}

// NewNoOpMetrics creates a no-op metrics instance for testing.
func NewNoOpMetrics(namespace string) luxmetrics.Metrics {
	return luxmetrics.NewNoOpMetrics(namespace)
}

// Noop is a no-op metrics instance.
var Noop = luxmetrics.NewNoOpMetrics("")