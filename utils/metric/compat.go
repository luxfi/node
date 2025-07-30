// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)


// NewPrefixGatherer creates a gatherer that prefixes all metrics with the given prefix
func NewPrefixGatherer(prefix string, gatherer prometheus.Gatherer) prometheus.Gatherer {
	// For now, return the original gatherer
	// This is a temporary compatibility layer
	return gatherer
}

// NewLabelGatherer creates a gatherer that adds labels to all metrics
func NewLabelGatherer(labelName, labelValue string, gatherer prometheus.Gatherer) prometheus.Gatherer {
	// For now, return the original gatherer
	// This is a temporary compatibility layer
	return gatherer
}

// MultiGatherer is an interface for gatherers that can register multiple sub-gatherers
type MultiGatherer interface {
	prometheus.Gatherer
	Register(namespace string, gatherer prometheus.Gatherer) error
	Deregister(namespace string) bool
}

// multiGatherer implements MultiGatherer
type multiGatherer struct {
	gatherers map[string]prometheus.Gatherer
}

// NewMultiGatherer creates a new MultiGatherer
func NewMultiGatherer() MultiGatherer {
	return &multiGatherer{
		gatherers: make(map[string]prometheus.Gatherer),
	}
}

func (m *multiGatherer) Gather() ([]*dto.MetricFamily, error) {
	// Temporary implementation
	return nil, nil
}

func (m *multiGatherer) Register(namespace string, gatherer prometheus.Gatherer) error {
	m.gatherers[namespace] = gatherer
	return nil
}

func (m *multiGatherer) Deregister(namespace string) bool {
	_, exists := m.gatherers[namespace]
	delete(m.gatherers, namespace)
	return exists
}