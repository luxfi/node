// Copyright (C) 2025-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metricstest

import (
	"sync"
	"testing"
)

var metricsLock sync.Mutex

// WithMetrics enables metrics for the test and prevents any other
// tests with metrics from running concurrently.
//
// Metrics are restored to their original value during testing cleanup.
func WithMetrics(t testing.TB) {
	metricsLock.Lock()
	t.Cleanup(func() {
		metricsLock.Unlock()
	})
}
