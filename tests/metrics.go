// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"fmt"

	"github.com/luxfi/metric"
	"github.com/luxfi/node/api/metrics"
)

// "metric name" -> "metric value"
type NodeMetrics map[string]*metric.MetricFamily

// URI -> "metric name" -> "metric value"
type NodesMetrics map[string]NodeMetrics

// GetNodeMetrics retrieves the specified metrics the provided node URI.
func GetNodeMetrics(ctx context.Context, nodeURI string) (NodeMetrics, error) {
	client := metrics.NewClient(nodeURI)
	return client.GetMetrics(ctx)
}

// GetNodesMetrics retrieves the specified metrics for the provided node URIs.
func GetNodesMetrics(ctx context.Context, nodeURIs []string) (NodesMetrics, error) {
	metrics := make(NodesMetrics, len(nodeURIs))
	for _, u := range nodeURIs {
		var err error
		metrics[u], err = GetNodeMetrics(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve metrics for %s: %w", u, err)
		}
	}
	return metrics, nil
}

// GetMetricValue returns the value of the specified metric which has the
// required labels.
//
// If multiple metrics match the provided labels, the first metric found is
// returned.
//
// Only Counter and Gauge metrics are supported.
func GetMetricValue(metrics NodeMetrics, name string, labels metric.Labels) (float64, bool) {
	metricFamily, ok := metrics[name]
	if !ok {
		return 0, false
	}

	for _, m := range metricFamily.Metrics {
		if !labelsMatch(m, labels) {
			continue
		}

		// For counters and gauges, Value holds the current value
		switch metricFamily.Type {
		case metric.MetricTypeCounter, metric.MetricTypeGauge:
			return m.Value.Value, true
		}
	}
	return 0, false
}

func labelsMatch(m metric.Metric, labels metric.Labels) bool {
	var found int
	for _, label := range m.Labels {
		expectedValue, ok := labels[label.Name]
		if !ok {
			continue
		}
		if label.Value != expectedValue {
			return false
		}
		found++
	}
	return found == len(labels)
}
