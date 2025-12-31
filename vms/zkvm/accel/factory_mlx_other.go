// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !darwin || !arm64

package accel

// newMLXAccelerator returns GoAccelerator on non-Apple platforms.
// MLX is only available on Apple Silicon, so we fall back to pure Go.
func newMLXAccelerator(config Config) (Accelerator, error) {
	return NewGoAccelerator(config)
}

// isMLXAvailable returns false on non-Apple platforms.
func isMLXAvailable() bool {
	return false
}
