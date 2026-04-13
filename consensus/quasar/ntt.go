// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !cgo

// Package quasar provides NTT operations for Corona consensus.
// This file provides pure Go CPU implementation when CGO is not available.
// All operations use the luxfi/lattice library which provides optimized
// NTT implementations in pure Go.
package quasar

import (
	"sync"
	"sync/atomic"

	"github.com/luxfi/lattice/v7/ring"
)

// NTTAccelerator provides NTT operations for Corona.
// When CGO is disabled, this uses the pure Go lattice library
// which provides optimized CPU-based NTT transforms.
type NTTAccelerator struct {
	enabled bool
	stats   NTTStats
	statsmu sync.RWMutex
}

// NTTStats tracks NTT accelerator statistics.
type NTTStats struct {
	Enabled      bool
	Backend      string
	TotalOps     uint64
	GPUAvailable bool
}

// NewNTTAccelerator creates a new NTT accelerator using pure Go lattice library.
func NewNTTAccelerator() (*NTTAccelerator, error) {
	return &NTTAccelerator{
		enabled: true, // CPU implementation is always available
		stats: NTTStats{
			Enabled: true,
			Backend: "CPU (Pure Go)",
		},
	}, nil
}

// IsEnabled returns true - CPU implementation is always available.
func (g *NTTAccelerator) IsEnabled() bool {
	return true
}

// Backend returns the backend name.
func (g *NTTAccelerator) Backend() string {
	return "CPU (Pure Go lattice)"
}

// NTTForward performs forward NTT on a polynomial using lattice library.
func (g *NTTAccelerator) NTTForward(r *ring.Ring, poly ring.Poly) error {
	r.NTT(poly, poly)
	atomic.AddUint64(&g.stats.TotalOps, 1)
	return nil
}

// NTTInverse performs inverse NTT on a polynomial using lattice library.
func (g *NTTAccelerator) NTTInverse(r *ring.Ring, poly ring.Poly) error {
	r.INTT(poly, poly)
	atomic.AddUint64(&g.stats.TotalOps, 1)
	return nil
}

// BatchNTTForward performs forward NTT on multiple polynomials.
// Uses parallel processing for better performance on multi-core CPUs.
func (g *NTTAccelerator) BatchNTTForward(r *ring.Ring, polys []ring.Poly) error {
	if len(polys) == 0 {
		return nil
	}

	// For small batches, process sequentially
	if len(polys) < 8 {
		for _, poly := range polys {
			r.NTT(poly, poly)
		}
		atomic.AddUint64(&g.stats.TotalOps, uint64(len(polys)))
		return nil
	}

	// For larger batches, use parallel processing
	var wg sync.WaitGroup
	numWorkers := 4
	chunkSize := (len(polys) + numWorkers - 1) / numWorkers

	for i := 0; i < numWorkers; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(polys) {
			end = len(polys)
		}
		if start >= end {
			break
		}

		wg.Add(1)
		go func(batch []ring.Poly) {
			defer wg.Done()
			for _, poly := range batch {
				r.NTT(poly, poly)
			}
		}(polys[start:end])
	}
	wg.Wait()

	atomic.AddUint64(&g.stats.TotalOps, uint64(len(polys)))
	return nil
}

// BatchNTTInverse performs inverse NTT on multiple polynomials.
// Uses parallel processing for better performance on multi-core CPUs.
func (g *NTTAccelerator) BatchNTTInverse(r *ring.Ring, polys []ring.Poly) error {
	if len(polys) == 0 {
		return nil
	}

	// For small batches, process sequentially
	if len(polys) < 8 {
		for _, poly := range polys {
			r.INTT(poly, poly)
		}
		atomic.AddUint64(&g.stats.TotalOps, uint64(len(polys)))
		return nil
	}

	// For larger batches, use parallel processing
	var wg sync.WaitGroup
	numWorkers := 4
	chunkSize := (len(polys) + numWorkers - 1) / numWorkers

	for i := 0; i < numWorkers; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(polys) {
			end = len(polys)
		}
		if start >= end {
			break
		}

		wg.Add(1)
		go func(batch []ring.Poly) {
			defer wg.Done()
			for _, poly := range batch {
				r.INTT(poly, poly)
			}
		}(polys[start:end])
	}
	wg.Wait()

	atomic.AddUint64(&g.stats.TotalOps, uint64(len(polys)))
	return nil
}

// PolyMul performs polynomial multiplication using Barrett reduction.
func (g *NTTAccelerator) PolyMul(r *ring.Ring, a, b, out ring.Poly) error {
	r.MulCoeffsBarrett(a, b, out)
	atomic.AddUint64(&g.stats.TotalOps, 1)
	return nil
}

// ClearCache is a no-op for CPU implementation (no GPU cache).
func (g *NTTAccelerator) ClearCache() {}

// Stats returns current NTT accelerator statistics.
func (g *NTTAccelerator) Stats() NTTStats {
	g.statsmu.RLock()
	defer g.statsmu.RUnlock()
	return NTTStats{
		Enabled:      true,
		Backend:      "CPU (Pure Go lattice)",
		TotalOps:     atomic.LoadUint64(&g.stats.TotalOps),
		GPUAvailable: false, // CPU-only build
	}
}

// Global accelerator instance
var (
	globalNTTAccelerator     *NTTAccelerator
	globalNTTAcceleratorOnce sync.Once
)

// GetNTTAccelerator returns the global NTT accelerator instance.
func GetNTTAccelerator() (*NTTAccelerator, error) {
	globalNTTAcceleratorOnce.Do(func() {
		globalNTTAccelerator, _ = NewNTTAccelerator()
	})
	return globalNTTAccelerator, nil
}
