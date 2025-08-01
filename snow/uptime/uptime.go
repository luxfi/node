// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/luxfi/ids"
)

// Manager tracks validator uptimes
type Manager interface {
	// IsValidator returns whether the node ID is currently a validator
	IsValidator(nodeID ids.NodeID, subnetID ids.ID) bool

	// StartTracking starts tracking the uptime of the specified validator
	StartTracking(nodeIDs []ids.NodeID, subnetID ids.ID) error

	// StopTracking stops tracking the uptime of the specified validator
	StopTracking(nodeIDs []ids.NodeID, subnetID ids.ID) error

	// Connect marks the validator as connected
	Connect(nodeID ids.NodeID, subnetID ids.ID) error

	// Disconnect marks the validator as disconnected
	Disconnect(nodeID ids.NodeID) error

	// GetUptime returns the uptime of the validator as a percentage
	CalculateUptime(nodeID ids.NodeID, subnetID ids.ID) (time.Duration, time.Time, error)

	// GetUptimePercent returns the uptime of the validator as a percentage
	CalculateUptimePercent(nodeID ids.NodeID, subnetID ids.ID) (float64, error)

	// GetUptimePercentFrom returns the uptime of the validator as a percentage
	// from the specified start time
	CalculateUptimePercentFrom(nodeID ids.NodeID, subnetID ids.ID, startTime time.Time) (float64, error)
}

// TestManager extends Manager for testing
type TestManager interface {
	Manager

	// SetTime sets the current time for testing
	SetTime(time.Time)
}