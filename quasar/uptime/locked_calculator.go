// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"sync"
	"time"

	"github.com/luxfi/ids"
)

// LockedCalculator is a thread-safe wrapper for Calculator
type LockedCalculator struct {
	lock         sync.RWMutex
	calculator   Calculator
	bootstrapped *bool
	chainLock    *sync.RWMutex
}

// NewLockedCalculator returns a new LockedCalculator
func NewLockedCalculator() LockedCalculator {
	return LockedCalculator{
		calculator: NoOpCalculator{},
	}
}

// SetCalculator sets the calculator to use once the chain is bootstrapped
func (lc *LockedCalculator) SetCalculator(bootstrapped *bool, chainLock *sync.RWMutex, calculator Calculator) {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	lc.bootstrapped = bootstrapped
	lc.chainLock = chainLock
	lc.calculator = calculator
}

// CalculateUptime calculates the uptime of a validator
func (lc *LockedCalculator) CalculateUptime(nodeID ids.NodeID, startTime time.Time, endTime time.Time, connected bool) (float64, time.Duration, error) {
	lc.lock.RLock()
	defer lc.lock.RUnlock()

	// If not bootstrapped or calculator not set, return perfect uptime
	if lc.bootstrapped == nil || !*lc.bootstrapped || lc.calculator == nil {
		return 1.0, endTime.Sub(startTime), nil
	}

	return lc.calculator.CalculateUptime(nodeID, startTime, endTime, connected)
}

// CalculateUptimePercent calculates the uptime percentage of a validator
func (lc *LockedCalculator) CalculateUptimePercent(nodeID ids.NodeID, startTime time.Time, endTime time.Time) (float64, error) {
	lc.lock.RLock()
	defer lc.lock.RUnlock()

	// If not bootstrapped or calculator not set, return perfect uptime
	if lc.bootstrapped == nil || !*lc.bootstrapped || lc.calculator == nil {
		return 1.0, nil
	}

	return lc.calculator.CalculateUptimePercent(nodeID, startTime, endTime)
}