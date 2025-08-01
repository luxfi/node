// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"fmt"
	"time"
)

// Parameters defines the consensus parameters
type Parameters struct {
	// K is the number of nodes to poll
	K int

	// AlphaPreference is the vote threshold to change preference
	AlphaPreference int

	// AlphaConfidence is the vote threshold for confidence
	AlphaConfidence int

	// Beta is the number of consecutive successful polls required for finalization
	Beta int

	// ConcurrentRepolls is the number of concurrent polls
	ConcurrentRepolls int

	// OptimalProcessing is the number of blocks to process optimally
	OptimalProcessing int

	// MaxOutstandingItems is the maximum number of outstanding items
	MaxOutstandingItems int

	// MaxItemProcessingTime is the maximum time to process an item
	MaxItemProcessingTime time.Duration

	// MinRoundInterval is the minimum time between rounds
	MinRoundInterval time.Duration
}

// Valid returns an error if the parameters are invalid
func (p Parameters) Valid() error {
	if p.K <= 0 {
		return fmt.Errorf("K must be positive, got %d", p.K)
	}
	if p.AlphaPreference <= p.K/2 {
		return fmt.Errorf("AlphaPreference must be > K/2, got %d", p.AlphaPreference)
	}
	if p.AlphaPreference > p.K {
		return fmt.Errorf("AlphaPreference must be <= K, got %d", p.AlphaPreference)
	}
	if p.AlphaConfidence <= p.K/2 {
		return fmt.Errorf("AlphaConfidence must be > K/2, got %d", p.AlphaConfidence)
	}
	if p.AlphaConfidence > p.K {
		return fmt.Errorf("AlphaConfidence must be <= K, got %d", p.AlphaConfidence)
	}
	if p.AlphaPreference > p.AlphaConfidence {
		return fmt.Errorf("AlphaPreference must be <= AlphaConfidence, got %d > %d", p.AlphaPreference, p.AlphaConfidence)
	}
	if p.Beta <= 0 {
		return fmt.Errorf("Beta must be positive, got %d", p.Beta)
	}
	if p.ConcurrentRepolls <= 0 {
		return fmt.Errorf("ConcurrentRepolls must be positive, got %d", p.ConcurrentRepolls)
	}
	if p.OptimalProcessing <= 0 {
		return fmt.Errorf("OptimalProcessing must be positive, got %d", p.OptimalProcessing)
	}
	if p.MaxOutstandingItems <= 0 {
		return fmt.Errorf("MaxOutstandingItems must be positive, got %d", p.MaxOutstandingItems)
	}
	if p.MaxItemProcessingTime <= 0 {
		return fmt.Errorf("MaxItemProcessingTime must be positive, got %v", p.MaxItemProcessingTime)
	}
	if p.MinRoundInterval < 0 {
		return fmt.Errorf("MinRoundInterval must be non-negative, got %v", p.MinRoundInterval)
	}
	return nil
}