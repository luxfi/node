// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus_test

import (
	"testing"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"
)

// This test assumes that there are no imported packages that might change the
// default value of [metric.Enabled]. It is therefore in package
// `prometheus_test` in case any other tests modify the variable. If any imports
// here or in the implementation do actually do so then this test may have false
// negatives.
func TestMetricsEnabledByDefault(t *testing.T) {
	require.True(t, metric.Enabled, "metric.Enabled")
	require.IsType(t, (*metric.StandardCounter)(nil), metric.NewCounter(), "metric.NewCounter() returned wrong type")
}
