// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !cgo || !linux

package accel

// newCGOAccelerator is a stub when CGO is not available
func newCGOAccelerator(config Config) (Accelerator, error) {
	return nil, ErrBackendNotCompiled
}

// isCGOAvailable returns false when CGO is not enabled
func isCGOAvailable() bool {
	return false
}
