// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import "time"

// router package stubs for compilation
type RouterHealthConfig struct {
	MaxDropRate            float64
	MaxOutstandingRequests int
	MaxOutstandingDuration time.Duration
	MaxRunTimeRequests     time.Duration
	MaxDropRateHalflife    time.Duration
}