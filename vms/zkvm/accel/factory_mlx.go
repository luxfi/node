// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build darwin && arm64

package accel

// newMLXAccelerator creates an MLX accelerator on Apple Silicon
func newMLXAccelerator(config Config) (Accelerator, error) {
	return NewMLXAccelerator(config)
}

// isMLXAvailable returns true on Apple Silicon
func isMLXAvailable() bool {
	return true
}
