// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

type Registry interface {
	// Call the given function for each registered metrics.
	Each(func(name string, metric any))
	// Get the metric by the given name or nil if none is registered.
	Get(name string) any
}
