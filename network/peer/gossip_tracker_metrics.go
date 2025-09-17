// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/metric"

	"github.com/luxfi/node/utils"
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

	err := utils.Err(
		registerer.Register(m.trackedPeersSize.(prometheus.Collector)),
		registerer.Register(m.validatorsSize.(prometheus.Collector)),
	)
	return m, err
}
