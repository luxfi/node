//go:build !cgo

// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fhe

import (
	"github.com/luxfi/lattice/v7/ring"
	"github.com/luxfi/log"
)

// GPUFHEAccelerator is a stub when CGO is disabled.
// All operations fall back to CPU.
type GPUFHEAccelerator struct {
	stats *GPUFHEStats
}

// GPUFHEStats tracks GPU acceleration statistics
type GPUFHEStats struct {
	NTTForwardCalls   uint64
	NTTInverseCalls   uint64
	PolyMulCalls      uint64
	BatchCalls        uint64
	GPUFallbackCalls  uint64
	TotalGPUTimeNs    uint64
}

// NewGPUFHEAccelerator returns a stub accelerator when CGO is disabled.
func NewGPUFHEAccelerator(_ log.Logger) (*GPUFHEAccelerator, error) {
	return &GPUFHEAccelerator{
		stats: &GPUFHEStats{},
	}, nil
}

// IsEnabled always returns false when CGO is disabled.
func (g *GPUFHEAccelerator) IsEnabled() bool {
	return false
}

// Backend returns CPU-only indicator.
func (g *GPUFHEAccelerator) Backend() string {
	return "CPU (CGO disabled)"
}

// Stats returns empty statistics.
func (g *GPUFHEAccelerator) Stats() GPUFHEStats {
	return *g.stats
}

// BatchNTTForward falls back to CPU.
func (g *GPUFHEAccelerator) BatchNTTForward(r *ring.Ring, polys []ring.Poly) error {
	for i := range polys {
		r.NTT(polys[i], polys[i])
	}
	return nil
}

// BatchNTTInverse falls back to CPU.
func (g *GPUFHEAccelerator) BatchNTTInverse(r *ring.Ring, polys []ring.Poly) error {
	for i := range polys {
		r.INTT(polys[i], polys[i])
	}
	return nil
}

// BatchPolyMul falls back to CPU.
func (g *GPUFHEAccelerator) BatchPolyMul(r *ring.Ring, a, b, out []ring.Poly) error {
	for i := range a {
		r.MulCoeffsBarrett(a[i], b[i], out[i])
	}
	return nil
}

// ClearCache is a no-op when CGO is disabled.
func (g *GPUFHEAccelerator) ClearCache() {}

// Close is a no-op when CGO is disabled.
func (g *GPUFHEAccelerator) Close() {}

// GetGPUFHEAccelerator returns the global CPU-only accelerator.
func GetGPUFHEAccelerator() (*GPUFHEAccelerator, error) {
	return NewGPUFHEAccelerator(nil)
}

// GPUNumberTheoreticTransformer is not available without CGO.
// Use ring.NewNumberTheoreticTransformerStandard directly.
type GPUNumberTheoreticTransformer struct {
	fallback ring.NumberTheoreticTransformer
}

// NewGPUNumberTheoreticTransformer returns a CPU fallback transformer.
func NewGPUNumberTheoreticTransformer(
	_ *GPUFHEAccelerator,
	subring *ring.SubRing,
	n int,
) ring.NumberTheoreticTransformer {
	return ring.NewNumberTheoreticTransformerStandard(subring, n)
}
