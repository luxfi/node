// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metricstest

import (
	"sync"
	"testing"

	"github.com/luxfi/node/vms/evm/metrics"
)

var metricsLock sync.Mutex

// WithMetrics enables [metrics.Enabled] for the test and prevents any other
// tests with metrics from running concurrently.
//
// [metrics.Enabled] is restored to its original value during testing cleanup.
func WithMetrics(t testing.TB) {
	metricsLock.Lock()
	initialValue := metrics.Enabled
	metrics.Enabled = true
	t.Cleanup(func() {
		metrics.Enabled = initialValue
		metricsLock.Unlock()
	})
}
