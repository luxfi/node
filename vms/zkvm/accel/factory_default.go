// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !darwin || !arm64
// +build !darwin !arm64

package accel

// newMLXAccelerator is a stub for non-Apple platforms
func newMLXAccelerator(config Config) (Accelerator, error) {
	return nil, ErrBackendNotCompiled
}

// isMLXAvailable returns false on non-Apple platforms
func isMLXAvailable() bool {
	return false
}
