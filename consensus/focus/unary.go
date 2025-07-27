// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package focus

import (
	"sync"
	"time"

	"github.com/luxfi/node/ids"
)

// UnaryFocusTracker tracks confidence for unary consensus
// (Previously UnaryConfidence in Avalanche)
//
// Unary focus tracking is like measuring photon intensity -
// we're not choosing between options but confirming presence
// through repeated observations.
type UnaryFocusTracker struct {
	mu                sync.RWMutex
	beta              int
	currentChoice     ids.ID
	consecutivePolls  int
	totalPolls        int
	successfulPolls   int
	lastPollTime      time.Time
	pollHistory       []bool // Ring buffer of recent polls
	historySize       int
}

// NewUnaryFocusTracker creates a new unary focus tracker
func NewUnaryFocusTracker(beta int) *UnaryFocusTracker {
	historySize := beta * 2 // Keep history of 2x beta for analysis
	return &UnaryFocusTracker{
		beta:        beta,
		historySize: historySize,
		pollHistory: make([]bool, 0, historySize),
	}
}

// RecordPoll records the result of a photon sampling round
func (u *UnaryFocusTracker) RecordPoll(successful bool, choice ids.ID) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.totalPolls++
	u.lastPollTime = time.Now()

	// Update poll history
	if len(u.pollHistory) >= u.historySize {
		u.pollHistory = u.pollHistory[1:]
	}
	u.pollHistory = append(u.pollHistory, successful)

	if successful {
		u.successfulPolls++
		u.currentChoice = choice
		u.consecutivePolls++
	} else {
		// For unary, we might allow some failures without full reset
		// This is like photon detection with noise
		if u.shouldResetFocus() {
			u.consecutivePolls = 0
		}
	}
}

// shouldResetFocus determines if we should reset focus based on failure pattern
func (u *UnaryFocusTracker) shouldResetFocus() bool {
	// If we have too many recent failures, reset
	recentFailures := 0
	lookback := u.beta / 2
	if lookback < 1 {
		lookback = 1
	}

	start := len(u.pollHistory) - lookback
	if start < 0 {
		start = 0
	}

	for i := start; i < len(u.pollHistory); i++ {
		if !u.pollHistory[i] {
			recentFailures++
		}
	}

	// Reset if more than 1/3 of recent polls failed
	return recentFailures > lookback/3
}

// GetConfidence returns current confidence level (0 to Beta)
func (u *UnaryFocusTracker) GetConfidence() int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.consecutivePolls
}

// IsFocused returns true if fully focused (beta rounds achieved)
func (u *UnaryFocusTracker) IsFocused() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.consecutivePolls >= u.beta
}

// GetBeta returns the beta parameter
func (u *UnaryFocusTracker) GetBeta() int {
	return u.beta
}

// GetChoice returns the choice we're focusing on
func (u *UnaryFocusTracker) GetChoice() ids.ID {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.currentChoice
}

// Reset clears confidence state
func (u *UnaryFocusTracker) Reset() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.currentChoice = ids.Empty
	u.consecutivePolls = 0
	u.totalPolls = 0
	u.successfulPolls = 0
	u.pollHistory = make([]bool, 0, u.historySize)
}

// GetRecentSuccessRate returns success rate over recent history
func (u *UnaryFocusTracker) GetRecentSuccessRate() int {
	u.mu.RLock()
	defer u.mu.RUnlock()

	if len(u.pollHistory) == 0 {
		return 0
	}

	successes := 0
	for _, success := range u.pollHistory {
		if success {
			successes++
		}
	}

	return (successes * 100) / len(u.pollHistory)
}

// TimeSinceLastPoll returns duration since last poll
func (u *UnaryFocusTracker) TimeSinceLastPoll() time.Duration {
	u.mu.RLock()
	defer u.mu.RUnlock()

	if u.lastPollTime.IsZero() {
		return 0
	}
	return time.Since(u.lastPollTime)
}