// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/utils/timer/mockable"
	"github.com/luxfi/node/v2/vms/platformvm/state"
)

// uptimeManager implements a basic uptime manager with additional methods for compatibility
type uptimeManager struct {
	state state.State
	clock *mockable.Clock
}

// NewUptimeManager creates a new uptime manager with additional methods
func NewUptimeManager(state state.State, clock *mockable.Clock) uptimeManager {
	return uptimeManager{
		state: state,
		clock: clock,
	}
}

// CalculateUptimePercentFrom calculates uptime percentage from a start time to now
func (um uptimeManager) CalculateUptimePercentFrom(nodeID ids.NodeID, startTime time.Time) (float64, error) {
	// For now, return perfect uptime
	// In a real implementation, this would track actual node uptime
	return 1.0, nil
}

// CalculateUptime implements uptime.Calculator interface
func (um uptimeManager) CalculateUptime(nodeID ids.NodeID, startTime time.Time, endTime time.Time, connected bool) (float64, time.Duration, error) {
	duration := endTime.Sub(startTime)
	// For now, return perfect uptime
	return 1.0, duration, nil
}

// CalculateUptimePercent implements uptime.Calculator interface
func (um uptimeManager) CalculateUptimePercent(nodeID ids.NodeID, startTime time.Time, endTime time.Time) (float64, error) {
	// For now, return perfect uptime
	return 1.0, nil
}

// IsConnected checks if a node is currently connected
func (um uptimeManager) IsConnected(nodeID ids.NodeID) bool {
	// Check if the node is a current validator
	_, err := um.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	// If the node is a current validator and we can find it, consider it connected
	// This is a simplified implementation - in reality, this would check actual connection state
	return err == nil
}

// StartedTracking returns whether tracking has started
func (um uptimeManager) StartedTracking() bool {
	// For now, always return true
	return true
}

// StopTracking stops tracking the given validators
func (um uptimeManager) StopTracking(nodeIDs []ids.NodeID) error {
	// No-op for now
	return nil
}

// StartTracking starts tracking the given validators
func (um uptimeManager) StartTracking(nodeIDs []ids.NodeID) error {
	// No-op for now
	return nil
}

// Connect marks a validator as connected
func (um uptimeManager) Connect(nodeID ids.NodeID) error {
	// No-op for now
	return nil
}

// Disconnect marks a validator as disconnected
func (um uptimeManager) Disconnect(nodeID ids.NodeID) error {
	// No-op for now
	return nil
}