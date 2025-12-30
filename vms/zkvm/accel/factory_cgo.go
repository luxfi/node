// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build cgo && linux

package accel

// newCGOAccelerator creates a CGO accelerator on Linux
func newCGOAccelerator(config Config) (Accelerator, error) {
	return NewCGOAccelerator(config)
}

// isCGOAvailable returns true when CGO is enabled on Linux
func isCGOAvailable() bool {
	return true
}
