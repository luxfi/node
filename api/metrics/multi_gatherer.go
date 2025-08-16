// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"
)

// MultiGatherer extends the Gatherer interface by allowing additional gatherers
// to be registered.
type MultiGatherer interface {
	prometheus.Gatherer

	// Register adds the outputs of [gatherer] to the results of future calls to
	// Gather with the provided [name] added to the metric.
	Register(name string, gatherer prometheus.Gatherer) error

	// Deregister removes the outputs of a gatherer with [name] from the results
	// of future calls to Gather. Returns true if a gatherer with [name] was
	// found.
	Deregister(name string) bool
}

// Deprecated: Use NewPrefixGatherer instead.
//
// TODO: Remove once coreth is updated.
func NewMultiGatherer() MultiGatherer {
	return NewPrefixGatherer()
}

type multiGatherer struct {
	lock      sync.RWMutex
	names     []string
	gatherers prometheus.Gatherers
}

func (g *multiGatherer) Gather() ([]*dto.MetricFamily, error) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.gatherers.Gather()
}

func (g *multiGatherer) Register(name string, gatherer prometheus.Gatherer) error {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.names = append(g.names, name)
	g.gatherers = append(g.gatherers, gatherer)
	return nil
}

func (g *multiGatherer) Deregister(name string) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	for i, existingName := range g.names {
		if existingName == name {
			// Remove the gatherer and name
			g.names = append(g.names[:i], g.names[i+1:]...)
			g.gatherers = append(g.gatherers[:i], g.gatherers[i+1:]...)
			return true
		}
	}
	return false
}

func MakeAndRegister(gatherer MultiGatherer, name string) (*prometheus.Registry, error) {
	reg := prometheus.NewRegistry()
	if err := gatherer.Register(name, reg); err != nil {
		return nil, fmt.Errorf("couldn't register %q metrics: %w", name, err)
	}
	return reg, nil
}
