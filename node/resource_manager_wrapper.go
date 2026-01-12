// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"github.com/luxfi/node/network/tracker"
	"github.com/luxfi/resource"
)

// resourceManagerWrapper adapts resource.Manager to tracker.ResourceManager
type resourceManagerWrapper struct {
	manager resource.Manager
}

// NewResourceManagerWrapper creates a new resource manager wrapper
func NewResourceManagerWrapper(manager resource.Manager) tracker.ResourceManager {
	return &resourceManagerWrapper{
		manager: manager,
	}
}

// CPUUsage returns the current CPU usage percentage
func (r *resourceManagerWrapper) CPUUsage() float64 {
	return r.manager.CPUUsage()
}

// DiskUsage returns the current disk usage percentage
// Combines read and write usage into a single value
func (r *resourceManagerWrapper) DiskUsage() float64 {
	read, write := r.manager.DiskUsage()
	// Return the sum of read and write as a single metric
	return read + write
}

// Shutdown stops the resource manager
func (r *resourceManagerWrapper) Shutdown() {
	r.manager.Shutdown()
}
