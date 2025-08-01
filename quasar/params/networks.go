// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"time"
)

// Network-specific consensus parameters for Lux
// These can be overridden at runtime via consensus tools

// MainnetParams defines consensus parameters for the mainnet (21 validators).
// Optimized for production deployment with higher fault tolerance.
var MainnetParams = Parameters{
	K:                     21,
	AlphaPreference:       13, // tolerate up to 8 failures for liveness
	AlphaConfidence:       18, // tolerate up to 3 failures for finality
	Beta:                  8,  // 8×50 ms + 100 ms = 500 ms finality
	ConcurrentRepolls:     8,  // pipeline 8 rounds
	OptimalProcessing:     10,
	MaxOutstandingItems:   369,
	MaxItemProcessingTime: 9630 * time.Millisecond, // ~9.6 s health timeout
	MinRoundInterval:      200 * time.Millisecond,
}

// TestnetParams defines consensus parameters for the testnet (11 validators).
// Balanced between performance and fault tolerance for testing.
var TestnetParams = Parameters{
	K:                     11,
	AlphaPreference:       8,  // tolerate up to 3 failures
	AlphaConfidence:       9,  // tolerate up to 2 failures
	Beta:                  10, // 10×50 ms + 100 ms = 600 ms finality
	ConcurrentRepolls:     10, // pipeline 10 rounds
	OptimalProcessing:     10,
	MaxOutstandingItems:   256,
	MaxItemProcessingTime: 6930 * time.Millisecond, // 6.9 s health timeout
	MinRoundInterval:      100 * time.Millisecond,
}

// LocalParams defines consensus parameters for local networks (5 validators).
// Optimized for 10 Gbps network with minimal latency.
var LocalParams = Parameters{
	K:                     5,
	AlphaPreference:       4,  // tolerate up to 1 failure
	AlphaConfidence:       4,  // tolerate up to 1 failure
	Beta:                  3,  // 3×10 ms + 20 ms ≈ 50 ms finality
	ConcurrentRepolls:     3,  // pipeline 3 rounds (limited by beta)
	OptimalProcessing:     32, // process 32 items in parallel
	MaxOutstandingItems:   256,
	MaxItemProcessingTime: 3690 * time.Millisecond, // 3.69 s health timeout
	MinRoundInterval:      10 * time.Millisecond,
}

// GetParams returns the appropriate parameters for the given network name.
func GetParams(network string) Parameters {
	switch network {
	case "mainnet":
		return MainnetParams
	case "testnet":
		return TestnetParams
	case "local", "localnet":
		return LocalParams
	default:
		// Return testnet params as default
		return TestnetParams
	}
}
