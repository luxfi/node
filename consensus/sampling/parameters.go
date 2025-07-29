// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampling

import (
	"errors"
	"time"
)

var (
	errK                     = errors.New("k must be positive")
	errAlpha                 = errors.New("alpha must be in (0, k]")
	errBeta                  = errors.New("beta must be positive")
	errConcurrentRepolls     = errors.New("concurrent repolls must be positive")
	errOptimalProcessing     = errors.New("optimal processing must be positive")
	errMaxOutstandingItems   = errors.New("max outstanding items must be positive")
	errMaxItemProcessingTime = errors.New("max item processing time must be positive")
)

// Parameters defines the consensus parameters for binary voting
type Parameters struct {
	// K is the number of nodes to poll
	K int `json:"k" yaml:"k"`

	// AlphaPreference is the vote threshold to change preference
	AlphaPreference int `json:"alphaPreference" yaml:"alphaPreference"`

	// AlphaConfidence is the vote threshold for confidence
	AlphaConfidence int `json:"alphaConfidence" yaml:"alphaConfidence"`

	// Beta is the number of consecutive successful polls required for finalization
	Beta int `json:"beta" yaml:"beta"`

	// ConcurrentRepolls is the number of concurrent polls
	ConcurrentRepolls int `json:"concurrentRepolls" yaml:"concurrentRepolls"`

	// OptimalProcessing is the number of items to process optimally
	OptimalProcessing int `json:"optimalProcessing" yaml:"optimalProcessing"`

	// MaxOutstandingItems is the maximum number of outstanding items
	MaxOutstandingItems int `json:"maxOutstandingItems" yaml:"maxOutstandingItems"`

	// MaxItemProcessingTime is the maximum time to process an item
	MaxItemProcessingTime time.Duration `json:"maxItemProcessingTime" yaml:"maxItemProcessingTime"`
}

// Valid returns nil if the parameters are valid
func (p Parameters) Valid() error {
	switch {
	case p.K <= 0:
		return errK
	case p.AlphaPreference <= 0 || p.AlphaPreference > p.K:
		return errAlpha
	case p.AlphaConfidence <= 0 || p.AlphaConfidence > p.K:
		return errAlpha
	case p.Beta <= 0:
		return errBeta
	case p.ConcurrentRepolls <= 0:
		return errConcurrentRepolls
	case p.OptimalProcessing <= 0:
		return errOptimalProcessing
	case p.MaxOutstandingItems <= 0:
		return errMaxOutstandingItems
	case p.MaxItemProcessingTime <= 0:
		return errMaxItemProcessingTime
	default:
		return nil
	}
}