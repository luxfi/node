// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"github.com/luxfi/metric"
)

// PrometheusRegistryAdapter wraps a luxfi/metric Registry to implement metrics.Registerer
type PrometheusRegistryAdapter struct {
	registry metrics.Registry
}

// NewPrometheusRegistryAdapter creates a new adapter
func NewPrometheusRegistryAdapter(registry metrics.Registry) metrics.Registerer {
	return &PrometheusRegistryAdapter{
		registry: registry,
	}
}

// Register implements metrics.Registerer
func (p *PrometheusRegistryAdapter) Register(c metrics.Collector) error {
	// For now, this is a no-op adapter for testing
	// In production, we should properly convert between the two
	return nil
}

// MustRegister implements metrics.Registerer
func (p *PrometheusRegistryAdapter) MustRegister(cs ...metrics.Collector) {
	// For now, this is a no-op adapter for testing
	// In production, we should properly convert between the two
}

// Unregister implements metrics.Registerer
func (p *PrometheusRegistryAdapter) Unregister(c metrics.Collector) bool {
	// For now, this is a no-op adapter for testing
	return true
}
