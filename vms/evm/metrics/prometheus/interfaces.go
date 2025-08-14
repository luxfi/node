// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

import "github.com/luxfi/node/vms/evm/metrics"

var _ Registry = (*metricsRegistry)(nil)

type metricsRegistry struct{}

type Registry interface {
	// Call the given function for each registered metric.
	Each(func(name string, metric any))
	// Get the metric by the given name or nil if none is registered.
	Get(name string) any
}
