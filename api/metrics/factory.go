// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	luxmetrics "github.com/luxfi/metrics"
)

// NewLuxMetricsMultiGatherer creates a MultiGatherer using Lux metrics
func NewLuxMetricsMultiGatherer() MultiGatherer {
	// Create a new PrefixGatherer which implements the MultiGatherer interface
	return NewPrefixGatherer()
}

// CreateLuxMetrics creates a Lux metrics instance with a prometheus backend
func CreateLuxMetrics(namespace string) luxmetrics.Metrics {
	// Use the prometheus factory from Lux metrics
	factory := luxmetrics.NewPrometheusFactory()
	return factory.New(namespace)
}

// GetPrometheusRegistry extracts the prometheus registry from Lux metrics
func GetPrometheusRegistry(metrics luxmetrics.Metrics) (*prometheus.Registry, bool) {
	registry := metrics.Registry()
	return luxmetrics.UnwrapPrometheusRegistry(registry)
}