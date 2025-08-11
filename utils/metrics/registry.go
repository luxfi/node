// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
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