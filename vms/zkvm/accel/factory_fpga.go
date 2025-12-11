// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build fpga
// +build fpga

package accel

// newFPGAAccelerator creates an FPGA accelerator
func newFPGAAccelerator(config Config) (Accelerator, error) {
	return NewFPGAAccelerator(config)
}

// isFPGAAvailable returns true when FPGA support is compiled in
func isFPGAAvailable() bool {
	return true
}
