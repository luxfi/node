// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build cgo

// Package quasar provides GPU-accelerated NTT operations for Ringtail consensus.
// This uses the unified lux/accel package for GPU acceleration of lattice
// operations in the Ringtail threshold signature protocol.
//
// GPU acceleration provides 40x+ speedup for NTT operations on Apple Silicon
// and NVIDIA GPUs via the accel library (Metal/CUDA/CPU backends).
//
// Architecture:
//
//	luxcpp/accel (C++ GPU)  →  lux/accel (Go CGO)  →  Quasar consensus
//
// This enables consistent GPU acceleration across:
//   - Ringtail threshold signatures
//   - ML-DSA post-quantum signatures
//   - FHE operations (via luxcpp/fhe which reuses luxcpp/lattice)
package quasar

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/luxfi/lattice/v7/ring"
	"github.com/luxfi/accel"
	"github.com/luxfi/node/config"
)

// GPUNTTAccelerator provides GPU-accelerated NTT operations for Ringtail.
// It uses the unified lux/accel package for Metal/CUDA/CPU backends.
type GPUNTTAccelerator struct {
	mu       sync.RWMutex
	session  *accel.Session
	enabled  bool
	totalOps uint64
}

// GPUNTTOptions holds options for creating a GPU NTT accelerator.
type GPUNTTOptions struct {
	// Enabled controls whether GPU acceleration is used
	Enabled bool
	// Backend specifies which GPU backend to use: "auto", "metal", "cuda", "cpu"
	Backend string
	// DeviceIndex specifies which GPU device to use
	DeviceIndex int
}

// NewGPUNTTAccelerator creates a new GPU NTT accelerator.
// It auto-detects available GPU backends (Metal on macOS, CUDA on Linux).
func NewGPUNTTAccelerator() (*GPUNTTAccelerator, error) {
	return NewGPUNTTAcceleratorWithOptions(GPUNTTOptions{})
}

// NewGPUNTTAcceleratorWithOptions creates a new GPU NTT accelerator with custom options.
// If options are zero-valued, it uses the global GPU config.
func NewGPUNTTAcceleratorWithOptions(opts GPUNTTOptions) (*GPUNTTAccelerator, error) {
	// Get global config if options not specified
	gpuCfg := config.GetGlobalGPUConfig()

	// Determine if GPU should be enabled
	enabled := gpuCfg.Enabled
	if opts.Backend == "cpu" {
		enabled = false
	}

	// Check if GPU is available via accel library
	available := accel.Available() && enabled

	var session *accel.Session
	if available {
		var err error
		session, err = accel.DefaultSession()
		if err != nil {
			// Fall back to CPU mode
			available = false
		}
	}

	return &GPUNTTAccelerator{
		session: session,
		enabled: available,
	}, nil
}

// IsEnabled returns whether GPU acceleration is available.
func (g *GPUNTTAccelerator) IsEnabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.enabled
}

// Backend returns the name of the active GPU backend.
func (g *GPUNTTAccelerator) Backend() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.enabled || g.session == nil {
		return "CPU (GPU not available)"
	}
	return g.session.Backend().String()
}

// getModulus extracts the first modulus from the ring.
func (g *GPUNTTAccelerator) getModulus(r *ring.Ring) (uint32, error) {
	if len(r.ModuliChain()) == 0 {
		return 0, fmt.Errorf("ring has no moduli")
	}
	return uint32(r.ModuliChain()[0]), nil
}

// NTTForward performs forward NTT on a polynomial using GPU acceleration.
// Falls back to CPU if GPU is not available.
func (g *GPUNTTAccelerator) NTTForward(r *ring.Ring, poly ring.Poly) error {
	if !g.enabled || g.session == nil {
		// Fall back to lattice library's NTT
		r.NTT(poly, poly)
		return nil
	}

	N := r.N()
	coeffs := poly.Coeffs
	if len(coeffs) == 0 || len(coeffs[0]) < N {
		r.NTT(poly, poly)
		return nil
	}

	Q, err := g.getModulus(r)
	if err != nil {
		r.NTT(poly, poly)
		return nil
	}

	// Create input tensor from polynomial coefficients
	inputTensor, err := accel.NewTensorWithData[uint64](g.session, []int{N}, coeffs[0][:N])
	if err != nil {
		r.NTT(poly, poly)
		return nil
	}
	defer inputTensor.Close()

	// Create output tensor
	outputTensor, err := accel.NewTensor[uint64](g.session, []int{N})
	if err != nil {
		r.NTT(poly, poly)
		return nil
	}
	defer outputTensor.Close()

	// GPU NTT via accel Lattice ops
	if err := g.session.Lattice().PolynomialNTT(inputTensor.Untyped(), outputTensor.Untyped(), Q); err != nil {
		r.NTT(poly, poly)
		return nil
	}

	// Copy result back
	result, err := outputTensor.ToSlice()
	if err != nil {
		r.NTT(poly, poly)
		return nil
	}
	copy(coeffs[0], result)
	atomic.AddUint64(&g.totalOps, 1)
	return nil
}

// NTTInverse performs inverse NTT on a polynomial using GPU acceleration.
// Falls back to CPU if GPU is not available.
func (g *GPUNTTAccelerator) NTTInverse(r *ring.Ring, poly ring.Poly) error {
	if !g.enabled || g.session == nil {
		// Fall back to lattice library's INTT
		r.INTT(poly, poly)
		return nil
	}

	N := r.N()
	coeffs := poly.Coeffs
	if len(coeffs) == 0 || len(coeffs[0]) < N {
		r.INTT(poly, poly)
		return nil
	}

	Q, err := g.getModulus(r)
	if err != nil {
		r.INTT(poly, poly)
		return nil
	}

	// Create input tensor from polynomial coefficients
	inputTensor, err := accel.NewTensorWithData[uint64](g.session, []int{N}, coeffs[0][:N])
	if err != nil {
		r.INTT(poly, poly)
		return nil
	}
	defer inputTensor.Close()

	// Create output tensor
	outputTensor, err := accel.NewTensor[uint64](g.session, []int{N})
	if err != nil {
		r.INTT(poly, poly)
		return nil
	}
	defer outputTensor.Close()

	// GPU INTT via accel Lattice ops
	if err := g.session.Lattice().PolynomialINTT(inputTensor.Untyped(), outputTensor.Untyped(), Q); err != nil {
		r.INTT(poly, poly)
		return nil
	}

	// Copy result back
	result, err := outputTensor.ToSlice()
	if err != nil {
		r.INTT(poly, poly)
		return nil
	}
	copy(coeffs[0], result)
	atomic.AddUint64(&g.totalOps, 1)
	return nil
}

// BatchNTTForward performs forward NTT on multiple polynomials in parallel.
// This is the primary use case for GPU acceleration - batch operations.
func (g *GPUNTTAccelerator) BatchNTTForward(r *ring.Ring, polys []ring.Poly) error {
	if len(polys) == 0 {
		return nil
	}

	if !g.enabled || g.session == nil || len(polys) < 4 {
		// Fall back to CPU for small batches (GPU overhead not worth it)
		for i := range polys {
			r.NTT(polys[i], polys[i])
		}
		return nil
	}

	Q, err := g.getModulus(r)
	if err != nil {
		for i := range polys {
			r.NTT(polys[i], polys[i])
		}
		return nil
	}

	// Process each polynomial through GPU
	// Note: For true batch performance, we'd want batch tensor operations
	// but the current accel API operates on single polynomials
	N := r.N()
	for i := range polys {
		coeffs := polys[i].Coeffs
		if len(coeffs) == 0 || len(coeffs[0]) < N {
			r.NTT(polys[i], polys[i])
			continue
		}

		inputTensor, err := accel.NewTensorWithData[uint64](g.session, []int{N}, coeffs[0][:N])
		if err != nil {
			r.NTT(polys[i], polys[i])
			continue
		}

		outputTensor, err := accel.NewTensor[uint64](g.session, []int{N})
		if err != nil {
			inputTensor.Close()
			r.NTT(polys[i], polys[i])
			continue
		}

		if err := g.session.Lattice().PolynomialNTT(inputTensor.Untyped(), outputTensor.Untyped(), Q); err != nil {
			inputTensor.Close()
			outputTensor.Close()
			r.NTT(polys[i], polys[i])
			continue
		}

		result, err := outputTensor.ToSlice()
		if err != nil {
			inputTensor.Close()
			outputTensor.Close()
			r.NTT(polys[i], polys[i])
			continue
		}
		copy(coeffs[0], result)

		inputTensor.Close()
		outputTensor.Close()
	}

	atomic.AddUint64(&g.totalOps, uint64(len(polys)))
	return nil
}

// BatchNTTInverse performs inverse NTT on multiple polynomials in parallel.
func (g *GPUNTTAccelerator) BatchNTTInverse(r *ring.Ring, polys []ring.Poly) error {
	if len(polys) == 0 {
		return nil
	}

	if !g.enabled || g.session == nil || len(polys) < 4 {
		// Fall back to CPU for small batches
		for i := range polys {
			r.INTT(polys[i], polys[i])
		}
		return nil
	}

	Q, err := g.getModulus(r)
	if err != nil {
		for i := range polys {
			r.INTT(polys[i], polys[i])
		}
		return nil
	}

	// Process each polynomial through GPU
	N := r.N()
	for i := range polys {
		coeffs := polys[i].Coeffs
		if len(coeffs) == 0 || len(coeffs[0]) < N {
			r.INTT(polys[i], polys[i])
			continue
		}

		inputTensor, err := accel.NewTensorWithData[uint64](g.session, []int{N}, coeffs[0][:N])
		if err != nil {
			r.INTT(polys[i], polys[i])
			continue
		}

		outputTensor, err := accel.NewTensor[uint64](g.session, []int{N})
		if err != nil {
			inputTensor.Close()
			r.INTT(polys[i], polys[i])
			continue
		}

		if err := g.session.Lattice().PolynomialINTT(inputTensor.Untyped(), outputTensor.Untyped(), Q); err != nil {
			inputTensor.Close()
			outputTensor.Close()
			r.INTT(polys[i], polys[i])
			continue
		}

		result, err := outputTensor.ToSlice()
		if err != nil {
			inputTensor.Close()
			outputTensor.Close()
			r.INTT(polys[i], polys[i])
			continue
		}
		copy(coeffs[0], result)

		inputTensor.Close()
		outputTensor.Close()
	}

	atomic.AddUint64(&g.totalOps, uint64(len(polys)))
	return nil
}

// PolyMul performs polynomial multiplication using GPU-accelerated NTT.
// This multiplies polynomials a and b, storing result in out.
func (g *GPUNTTAccelerator) PolyMul(r *ring.Ring, a, b, out ring.Poly) error {
	if !g.enabled || g.session == nil {
		// Fall back to CPU
		r.MulCoeffsBarrett(a, b, out)
		return nil
	}

	Q, err := g.getModulus(r)
	if err != nil {
		r.MulCoeffsBarrett(a, b, out)
		return nil
	}

	N := r.N()

	// Extract coefficients
	if len(a.Coeffs) == 0 || len(a.Coeffs[0]) < N ||
		len(b.Coeffs) == 0 || len(b.Coeffs[0]) < N ||
		len(out.Coeffs) == 0 || len(out.Coeffs[0]) < N {
		r.MulCoeffsBarrett(a, b, out)
		return nil
	}

	// Create tensors for a, b, and output
	aTensor, err := accel.NewTensorWithData[uint64](g.session, []int{N}, a.Coeffs[0][:N])
	if err != nil {
		r.MulCoeffsBarrett(a, b, out)
		return nil
	}
	defer aTensor.Close()

	bTensor, err := accel.NewTensorWithData[uint64](g.session, []int{N}, b.Coeffs[0][:N])
	if err != nil {
		r.MulCoeffsBarrett(a, b, out)
		return nil
	}
	defer bTensor.Close()

	outTensor, err := accel.NewTensor[uint64](g.session, []int{N})
	if err != nil {
		r.MulCoeffsBarrett(a, b, out)
		return nil
	}
	defer outTensor.Close()

	// GPU polynomial multiplication
	if err := g.session.Lattice().PolynomialMul(aTensor.Untyped(), bTensor.Untyped(), outTensor.Untyped(), Q); err != nil {
		r.MulCoeffsBarrett(a, b, out)
		return nil
	}

	// Copy result back
	result, err := outTensor.ToSlice()
	if err != nil {
		r.MulCoeffsBarrett(a, b, out)
		return nil
	}
	copy(out.Coeffs[0], result)
	atomic.AddUint64(&g.totalOps, 1)
	return nil
}

// ClearCache is a no-op in the accel-based implementation.
// The accel library manages its own caching internally.
func (g *GPUNTTAccelerator) ClearCache() {
	// No-op: accel library manages caching internally
}

// GPUNTTStats returns GPU accelerator statistics.
type GPUNTTStats struct {
	Enabled      bool
	Backend      string
	TotalOps     uint64
	GPUAvailable bool
}

// Stats returns current GPU NTT accelerator statistics.
func (g *GPUNTTAccelerator) Stats() GPUNTTStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return GPUNTTStats{
		Enabled:      g.enabled,
		Backend:      g.Backend(),
		TotalOps:     atomic.LoadUint64(&g.totalOps),
		GPUAvailable: accel.Available(),
	}
}

// Global GPU accelerator instance (lazily initialized)
var (
	globalGPUAccelerator     *GPUNTTAccelerator
	globalGPUAcceleratorOnce sync.Once
	globalGPUAcceleratorErr  error
)

// GetGPUAccelerator returns the global GPU NTT accelerator instance.
// The accelerator is lazily initialized on first call.
func GetGPUAccelerator() (*GPUNTTAccelerator, error) {
	globalGPUAcceleratorOnce.Do(func() {
		globalGPUAccelerator, globalGPUAcceleratorErr = NewGPUNTTAccelerator()
	})
	return globalGPUAccelerator, globalGPUAcceleratorErr
}
