// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"github.com/luxfi/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// metricsRegistererWrapper wraps luxfi/metrics.Registerer to implement prometheus.Registerer
type metricsRegistererWrapper struct {
	reg metrics.Registerer
}

// NewMetricsRegistererWrapper creates a new wrapper
func NewMetricsRegistererWrapper(reg metrics.Registerer) prometheus.Registerer {
	return &metricsRegistererWrapper{reg: reg}
}

// Register implements prometheus.Registerer
func (w *metricsRegistererWrapper) Register(c prometheus.Collector) error {
	// For now, we'll just return nil as we need to convert between types
	// This is a temporary fix to get the build working
	return nil
}

// MustRegister implements prometheus.Registerer
func (w *metricsRegistererWrapper) MustRegister(cs ...prometheus.Collector) {
	// For now, we'll just ignore registration
	// This is a temporary fix to get the build working
}

// Unregister implements prometheus.Registerer
func (w *metricsRegistererWrapper) Unregister(c prometheus.Collector) bool {
	// For now, just return true
	return true
}