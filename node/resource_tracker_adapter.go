// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"time"

	"github.com/luxfi/ids"
	consensustracker "github.com/luxfi/consensus/networking/tracker"
	"github.com/luxfi/node/network/tracker"
)

// resourceTrackerAdapter adapts node tracker to consensus tracker interface
type resourceTrackerAdapter struct {
	tracker tracker.ResourceTracker
}

func (r *resourceTrackerAdapter) CPUTracker() consensustracker.CPUTracker {
	// Return adapted CPU tracker from the underlying resource tracker
	return &cpuTrackerAdapter{tracker: r.tracker.CPUTracker()}
}

func (r *resourceTrackerAdapter) StartProcessing(nodeID ids.NodeID, t time.Time) {
	// Stub implementation
}

func (r *resourceTrackerAdapter) StopProcessing(nodeID ids.NodeID, t time.Time) {
	// Stub implementation
}

func (r *resourceTrackerAdapter) DiskTracker() consensustracker.DiskTracker {
	// Return adapted disk tracker from the underlying resource tracker
	return &diskTrackerAdapter{tracker: r.tracker.DiskTracker()}
}

// BandwidthTracker is not implemented in node tracker

// cpuTrackerAdapter adapts node CPU tracker to consensus CPU tracker
type cpuTrackerAdapter struct {
	tracker tracker.Tracker
}

func (c *cpuTrackerAdapter) Usage(nodeID ids.NodeID, t time.Time) float64 {
	// Simple implementation - just return current usage
	return c.tracker.Usage(nodeID, t)
}

func (c *cpuTrackerAdapter) TimeUntilUsage(nodeID ids.NodeID, t time.Time, usage float64) time.Duration {
	// Stub implementation - return a default duration
	return time.Second
}

// diskTrackerAdapter adapts node disk tracker to consensus disk tracker
type diskTrackerAdapter struct {
	tracker tracker.Tracker
}

func (d *diskTrackerAdapter) Usage(nodeID ids.NodeID, t time.Time) float64 {
	// Simple implementation - just return current usage
	return d.tracker.Usage(nodeID, t)
}

func (d *diskTrackerAdapter) TimeUntilUsage(nodeID ids.NodeID, t time.Time, usage float64) time.Duration {
	// Stub implementation - return a default duration
	return time.Second
}
