// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/luxfi/metric"
)

type gossipTrackerMetrics struct {
	trackedPeersSize metric.Gauge
	validatorsSize   metric.Gauge
}

func newGossipTrackerMetrics(registerer metric.Registerer, namespace string) (gossipTrackerMetrics, error) {
	m := gossipTrackerMetrics{
		trackedPeersSize: metric.NewGauge(
			metric.GaugeOpts{
				Namespace: namespace,
				Name:      "tracked_peers_size",
				Help:      "amount of peers that are being tracked",
			},
		),
		validatorsSize: metric.NewGauge(
			metric.GaugeOpts{
				Namespace: namespace,
				Name:      "validators_size",
				Help:      "number of validators this node is tracking",
			},
		),
	}

	// Metrics work without explicit registration
	return m, nil
}
