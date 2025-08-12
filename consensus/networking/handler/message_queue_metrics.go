// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"github.com/luxfi/metric"
)

const opLabel = "op"

var opLabels = []string{opLabel}

type messageQueueMetrics struct {
	count             metrics.GaugeVec
	nodesWithMessages metrics.Gauge
	numExcessiveCPU   metrics.Counter
}

func (m *messageQueueMetrics) initialize(
	metricsNamespace string,
	metricsRegisterer metrics.Registry,
) error {
	namespace := metricsNamespace + "_unprocessed_msgs"
	metricsInstance := metrics.NewWithRegistry(namespace, metricsRegisterer)
	
	m.count = metricsInstance.NewGaugeVec(
		"count",
		"messages in the queue",
		opLabels,
	)
	m.nodesWithMessages = metricsInstance.NewGauge(
		"nodes",
		"nodes with at least 1 message ready to be processed",
	)
	m.numExcessiveCPU = metricsInstance.NewCounter(
		"excessive_cpu",
		"times a message has been deferred due to excessive CPU usage",
	)

	// Metrics are auto-registered with luxfi/metric
	return nil
}
