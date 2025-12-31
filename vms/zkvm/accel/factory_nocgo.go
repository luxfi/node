// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !cgo || !linux

package accel

// newCGOAccelerator returns the pure Go accelerator when CGO is not available.
// This provides full ZK functionality using optimized pure Go implementations.
func newCGOAccelerator(config Config) (Accelerator, error) {
	return NewGoAccelerator(config)
}

// isCGOAvailable returns false when CGO is not enabled
func isCGOAvailable() bool {
	return false
}
