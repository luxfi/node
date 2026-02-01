// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"time"

	consensustracker "github.com/luxfi/consensus/networking/tracker"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/tracker"
)

// resourceTrackerAdapter adapts node tracker to consensus tracker interface
type resourceTrackerAdapter struct {
	tracker tracker.ResourceTracker
}

func (r *resourceTrackerAdapter) CPUTracker() consensustracker.CPUTracker {
	return &cpuTrackerAdapter{tracker: r.tracker.CPUTracker()}
}

func (r *resourceTrackerAdapter) DiskTracker() consensustracker.DiskTracker {
	return &diskTrackerAdapter{tracker: r.tracker.DiskTracker()}
}

// cpuTrackerAdapter adapts node CPU tracker to consensus CPU tracker
type cpuTrackerAdapter struct {
	tracker tracker.Tracker
}

func (c *cpuTrackerAdapter) Usage(nodeID ids.NodeID, t time.Time) float64 {
	return c.tracker.Usage(nodeID, t)
}

func (c *cpuTrackerAdapter) TimeUntilUsage(nodeID ids.NodeID, t time.Time, usage float64) time.Duration {
	return c.tracker.TimeUntilUsage(nodeID, t, usage)
}

// diskTrackerAdapter adapts node disk tracker to consensus disk tracker
type diskTrackerAdapter struct {
	tracker tracker.Tracker
}

func (d *diskTrackerAdapter) Usage(nodeID ids.NodeID, t time.Time) float64 {
	return d.tracker.Usage(nodeID, t)
}

func (d *diskTrackerAdapter) TimeUntilUsage(nodeID ids.NodeID, t time.Time, usage float64) time.Duration {
	return d.tracker.TimeUntilUsage(nodeID, t, usage)
}
