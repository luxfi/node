// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !fpga

package accel

// newFPGAAccelerator returns GoAccelerator when FPGA is not available.
// Falls back to pure Go implementation.
func newFPGAAccelerator(config Config) (Accelerator, error) {
	return NewGoAccelerator(config)
}

// isFPGAAvailable returns false when FPGA support is not compiled.
func isFPGAAvailable() bool {
	return false
}
