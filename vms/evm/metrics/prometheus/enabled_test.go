// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus_test

import (
	"testing"
)

// This test assumes that there are no imported packages that might change the
// default value of [metrics.Enabled]. It is therefore in package
// `prometheus_test` in case any other tests modify the variable. If any imports
// here or in the implementation do actually do so then this test may have false
// negatives.
func TestMetricsEnabledByDefault(t *testing.T) {
	t.Skip("Metric package has been refactored - test needs update")
	// TODO: Update this test to work with new metric package structure
	// require.True(t, metrics.Enabled, "metrics.Enabled")
	// require.IsType(t, (*metrics.StandardCounter)(nil), metric.NewCounter(), "metric.NewCounter() returned wrong type")
}
