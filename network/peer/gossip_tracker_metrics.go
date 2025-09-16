// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	metrics "github.com/luxfi/metric"

	"github.com/luxfi/node/utils"
)

type gossipTrackerMetrics struct {
	trackedPeersSize metrics.Gauge
	validatorsSize   metrics.Gauge
}

func newGossipTrackerMetrics(registerer metrics.Registerer, namespace string) (gossipTrackerMetrics, error) {
	m := gossipTrackerMetrics{
		trackedPeersSize: metrics.NewGauge(
			metrics.GaugeOpts{
				Namespace: namespace,
				Name:      "tracked_peers_size",
				Help:      "amount of peers that are being tracked",
			},
		),
		validatorsSize: metrics.NewGauge(
			metrics.GaugeOpts{
				Namespace: namespace,
				Name:      "validators_size",
				Help:      "number of validators this node is tracking",
			},
		),
	}

	err := utils.Err(
		registerer.Register(m.trackedPeersSize),
		registerer.Register(m.validatorsSize),
	)
	return m, err
}
