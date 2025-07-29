// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package focus

import (
	"sync"

	"github.com/luxfi/ids"
)

// BinaryFocusTracker tracks confidence for binary consensus
// (Previously BinaryConfidence in Avalanche)
//
// Binary focus tracking is like a lens focusing between two
// possible states - as consensus forms, focus sharpens on
// one choice through beta consecutive rounds.
type BinaryFocusTracker struct {
	mu               sync.RWMutex
	beta             int
	currentChoice    ids.ID
	consecutivePolls int
	totalPolls       int
	successfulPolls  int
}

// NewBinaryFocusTracker creates a new binary focus tracker
func NewBinaryFocusTracker(beta int) *BinaryFocusTracker {
	return &BinaryFocusTracker{
		beta: beta,
	}
}

// RecordPoll records the result of a photon sampling round
func (b *BinaryFocusTracker) RecordPoll(successful bool, choice ids.ID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.totalPolls++

	if successful {
		b.successfulPolls++

		// Check if we're continuing to focus on the same choice
		if b.currentChoice == choice || b.currentChoice == ids.Empty {
			b.currentChoice = choice
			b.consecutivePolls++
		} else {
			// Focus shifted to different choice, reset
			b.currentChoice = choice
			b.consecutivePolls = 1
		}
	} else {
		// Lost focus, reset consecutive count
		b.consecutivePolls = 0
		// Keep current choice to see if we can refocus
	}
}

// GetConfidence returns current confidence level (0 to Beta)
func (b *BinaryFocusTracker) GetConfidence() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.consecutivePolls
}

// IsFocused returns true if fully focused (beta rounds achieved)
func (b *BinaryFocusTracker) IsFocused() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.consecutivePolls >= b.beta
}

// GetBeta returns the beta parameter
func (b *BinaryFocusTracker) GetBeta() int {
	return b.beta
}

// GetChoice returns the choice we're focusing on
func (b *BinaryFocusTracker) GetChoice() ids.ID {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.currentChoice
}

// Reset clears confidence state
func (b *BinaryFocusTracker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.currentChoice = ids.Empty
	b.consecutivePolls = 0
	b.totalPolls = 0
	b.successfulPolls = 0
}

// GetFocusState returns the current focus state
func (b *BinaryFocusTracker) GetFocusState() *FocusState {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return &FocusState{
		Choice:           b.currentChoice,
		ConsecutivePolls: b.consecutivePolls,
		Beta:             b.beta,
		TotalPolls:       b.totalPolls,
		SuccessfulPolls:  b.successfulPolls,
	}
}

// GetSuccessRate returns the overall success rate (0-100)
func (b *BinaryFocusTracker) GetSuccessRate() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.totalPolls == 0 {
		return 0
	}
	return (b.successfulPolls * 100) / b.totalPolls
}