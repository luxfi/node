// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/set"
)

// State represents the uptime state of validators
type State interface {
	// AddNode adds a node to track uptime for
	AddNode(nodeID ids.NodeID, startTime time.Time) error

	// RemoveNode removes a node from uptime tracking
	RemoveNode(nodeID ids.NodeID) error

	// IsConnected returns whether a node is connected
	IsConnected(nodeID ids.NodeID) bool

	// SetConnected sets a node's connected status
	SetConnected(nodeID ids.NodeID, connected bool) error

	// GetUptime returns a node's uptime percentage
	GetUptime(nodeID ids.NodeID) (float64, error)

	// GetUptimes returns all uptimes
	GetUptimes() (map[ids.NodeID]float64, error)

	// GetStartTime returns when a node started being tracked
	GetStartTime(nodeID ids.NodeID) (time.Time, error)
}

// Manager calculates validator uptimes
type Manager interface {
	// StartTracking starts tracking a validator's uptime
	StartTracking(nodeID ids.NodeID) error

	// StopTracking stops tracking a validator's uptime
	StopTracking(nodeID ids.NodeID) error

	// Connect marks a validator as connected
	Connect(nodeID ids.NodeID) error

	// Disconnect marks a validator as disconnected
	Disconnect(nodeID ids.NodeID) error

	// CalculateUptime calculates a validator's uptime percentage
	CalculateUptime(nodeID ids.NodeID) (float64, error)

	// CalculateUptimePercent calculates uptime percentage for a duration
	CalculateUptimePercent(nodeID ids.NodeID, startTime time.Time) (float64, error)

	// GetTrackedValidators returns the set of tracked validators
	GetTrackedValidators() set.Set[ids.NodeID]
}

// Calculator calculates uptime scores
type Calculator interface {
	// CalculateUptime calculates the uptime of a validator given their connected status
	CalculateUptime(nodeID ids.NodeID, startTime time.Time, endTime time.Time, connected bool) (float64, time.Duration, error)

	// CalculateUptimePercent calculates the uptime percentage of a validator
	CalculateUptimePercent(nodeID ids.NodeID, startTime time.Time, endTime time.Time) (float64, error)
}

// NoOpCalculator is a no-op implementation of Calculator
type NoOpCalculator struct{}

// CalculateUptime always returns 100% uptime
func (NoOpCalculator) CalculateUptime(nodeID ids.NodeID, startTime time.Time, endTime time.Time, connected bool) (float64, time.Duration, error) {
	return 1.0, endTime.Sub(startTime), nil
}

// CalculateUptimePercent always returns 100% uptime
func (NoOpCalculator) CalculateUptimePercent(nodeID ids.NodeID, startTime time.Time, endTime time.Time) (float64, error) {
	return 1.0, nil
}