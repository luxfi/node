// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/metric"
)

// PrometheusRegistryAdapter wraps a luxfi/metric Registry to implement prometheus.Registerer
type PrometheusRegistryAdapter struct {
	registry metrics.Registry
}

// NewPrometheusRegistryAdapter creates a new adapter
func NewPrometheusRegistryAdapter(registry metrics.Registry) prometheus.Registerer {
	return &PrometheusRegistryAdapter{
		registry: registry,
	}
}

// Register implements prometheus.Registerer
func (p *PrometheusRegistryAdapter) Register(c prometheus.Collector) error {
	// For now, this is a no-op adapter for testing
	// In production, we should properly convert between the two
	return nil
}

// MustRegister implements prometheus.Registerer
func (p *PrometheusRegistryAdapter) MustRegister(cs ...prometheus.Collector) {
	// For now, this is a no-op adapter for testing
	// In production, we should properly convert between the two
}

// Unregister implements prometheus.Registerer
func (p *PrometheusRegistryAdapter) Unregister(c prometheus.Collector) bool {
	// For now, this is a no-op adapter for testing
	return true
}