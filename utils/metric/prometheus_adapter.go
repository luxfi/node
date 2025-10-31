// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilmetric

import (
	metric "github.com/luxfi/metric"
)

// PrometheusRegistryAdapter wraps a luxfi/metric Registry to implement metric.Registerer
type PrometheusRegistryAdapter struct {
	registry metric.Registry
}

// NewPrometheusRegistryAdapter creates a new adapter
func NewPrometheusRegistryAdapter(registry metric.Registry) metric.Registerer {
	return &PrometheusRegistryAdapter{
		registry: registry,
	}
}

// Register implements metric.Registerer
func (p *PrometheusRegistryAdapter) Register(c metric.Collector) error {
	// For now, this is a no-op adapter for testing
	// In production, we should properly convert between the two
	return nil
}

// MustRegister implements metric.Registerer
func (p *PrometheusRegistryAdapter) MustRegister(cs ...metric.Collector) {
	// For now, this is a no-op adapter for testing
	// In production, we should properly convert between the two
}

// Unregister implements metric.Registerer
func (p *PrometheusRegistryAdapter) Unregister(c metric.Collector) bool {
	// For now, this is a no-op adapter for testing
	return true
}
