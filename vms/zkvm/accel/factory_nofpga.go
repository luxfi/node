// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !fpga

package accel

// newFPGAAccelerator is a stub when FPGA support is not compiled
func newFPGAAccelerator(config Config) (Accelerator, error) {
	return nil, ErrBackendNotCompiled
}

// isFPGAAvailable returns false when FPGA support is not compiled
func isFPGAAvailable() bool {
	return false
}
