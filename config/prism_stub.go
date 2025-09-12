// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import "time"

// prism package stubs for compilation
type PrismParameters struct {
	K                int
	Alpha            *int    // Optional alpha override
	AlphaPreference  int
	AlphaConfidence  int
	BetaVirtuous     int
	BetaRogue        int
	ConcurrentRepolls int
	OptimalProcessing int
	MaxOutstandingItems int
	MaxItemProcessingTime string
}

// benchlist stubs
type BenchlistConfig struct {
	FailThreshold          int
	MinFailingDuration     time.Duration
	Duration               time.Duration
	MaxPortion             float64
}