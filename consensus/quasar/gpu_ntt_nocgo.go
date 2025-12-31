// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !cgo

package quasar

import (
	"errors"

	"github.com/luxfi/lattice/v7/ring"
)

var errGPUNotAvailable = errors.New("GPU acceleration not available (CGO disabled)")

// GPUNTTAccelerator provides GPU-accelerated NTT operations for Ringtail.
// This is a stub implementation when CGO is not available.
type GPUNTTAccelerator struct {
	enabled bool
}

// NewGPUNTTAccelerator creates a new GPU NTT accelerator.
// Returns a disabled accelerator when CGO is not available.
func NewGPUNTTAccelerator() (*GPUNTTAccelerator, error) {
	return &GPUNTTAccelerator{enabled: false}, nil
}

// IsEnabled returns whether GPU acceleration is available.
func (g *GPUNTTAccelerator) IsEnabled() bool {
	return false
}

// Backend returns the name of the active GPU backend.
func (g *GPUNTTAccelerator) Backend() string {
	return "CPU (CGO disabled)"
}

// NTTForward performs forward NTT on a polynomial.
// Falls back to CPU since GPU is not available.
func (g *GPUNTTAccelerator) NTTForward(r *ring.Ring, poly ring.Poly) error {
	r.NTT(poly, poly)
	return nil
}

// NTTInverse performs inverse NTT on a polynomial.
// Falls back to CPU since GPU is not available.
func (g *GPUNTTAccelerator) NTTInverse(r *ring.Ring, poly ring.Poly) error {
	r.INTT(poly, poly)
	return nil
}

// BatchNTTForward performs forward NTT on multiple polynomials.
// Falls back to CPU since GPU is not available.
func (g *GPUNTTAccelerator) BatchNTTForward(r *ring.Ring, polys []ring.Poly) error {
	for _, poly := range polys {
		r.NTT(poly, poly)
	}
	return nil
}

// BatchNTTInverse performs inverse NTT on multiple polynomials.
// Falls back to CPU since GPU is not available.
func (g *GPUNTTAccelerator) BatchNTTInverse(r *ring.Ring, polys []ring.Poly) error {
	for _, poly := range polys {
		r.INTT(poly, poly)
	}
	return nil
}

// PolyMul performs polynomial multiplication.
// Falls back to CPU since GPU is not available.
func (g *GPUNTTAccelerator) PolyMul(r *ring.Ring, a, b, out ring.Poly) error {
	r.MulCoeffsBarrett(a, b, out)
	return nil
}

// ClearCache clears the GPU NTT context cache.
func (g *GPUNTTAccelerator) ClearCache() {}

// GPUNTTStats returns GPU accelerator statistics.
type GPUNTTStats struct {
	Enabled       bool
	Backend       string
	CachedRings   int
	TotalOps      uint64
}

// Stats returns current GPU NTT accelerator statistics.
func (g *GPUNTTAccelerator) Stats() GPUNTTStats {
	return GPUNTTStats{
		Enabled:     false,
		Backend:     "CPU (CGO disabled)",
		CachedRings: 0,
		TotalOps:    0,
	}
}

// GetGPUAccelerator returns the global GPU NTT accelerator instance.
func GetGPUAccelerator() (*GPUNTTAccelerator, error) {
	return NewGPUNTTAccelerator()
}
