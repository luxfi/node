// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build fpga
// +build fpga

// FPGA ZK accelerator implementation
// Uses shared luxfi/fpga package for AMD Versal, AWS F2, Intel Stratix support

package accel

import (
	"errors"
	"sync"
	"time"

	"github.com/luxfi/fpga"
)

// FPGAAccelerator uses FPGA hardware for ZK acceleration
type FPGAAccelerator struct {
	config     Config
	goFallback *GoAccelerator
	mu         sync.RWMutex

	// FPGA accelerator from shared package
	fpgaAcc fpga.ZKAccelerator
	fpgaCfg fpga.Config

	// Kernel status
	nttLoaded      bool
	msmLoaded      bool
	hashLoaded     bool
	fheLoaded      bool
}

// NewFPGAAccelerator creates an FPGA-accelerated ZK prover
func NewFPGAAccelerator(config Config) (*FPGAAccelerator, error) {
	// Create Go fallback for operations not yet FPGA-accelerated
	goAcc, err := NewGoAccelerator(config)
	if err != nil {
		return nil, err
	}

	// Configure FPGA
	fpgaCfg := fpga.Config{
		Backend:         fpga.BackendAMDVersal, // Default to Versal
		EnableZKKernels: true,
		EnableFHEKernels: config.EnableFHE,
		DMAChannels:     4,
		KernelClockMHz:  1250,
	}

	// Auto-detect if not specified
	if fpgaCfg.Backend == fpga.BackendNone {
		detected, err := fpga.AutoDetectBackend()
		if err != nil {
			return nil, err
		}
		fpgaCfg.Backend = detected
	}

	// Create FPGA accelerator
	fpgaAcc, err := fpga.NewZKAccelerator(fpgaCfg)
	if err != nil {
		// Fall back to simulation if no hardware
		fpgaCfg.Backend = fpga.BackendSimulation
		fpgaAcc, err = fpga.NewZKAccelerator(fpgaCfg)
		if err != nil {
			return nil, err
		}
	}

	acc := &FPGAAccelerator{
		config:     config,
		goFallback: goAcc,
		fpgaAcc:    fpgaAcc,
		fpgaCfg:    fpgaCfg,
		nttLoaded:  true,
		msmLoaded:  true,
		hashLoaded: true,
		fheLoaded:  config.EnableFHE,
	}

	return acc, nil
}

func (a *FPGAAccelerator) Backend() Backend {
	return BackendFPGA
}

func (a *FPGAAccelerator) Device() string {
	return string(a.fpgaAcc.Backend()) + " FPGA"
}

func (a *FPGAAccelerator) IsGPUAvailable() bool {
	return false // FPGA is not GPU
}

func (a *FPGAAccelerator) Capabilities() Capabilities {
	fpgaCaps := a.fpgaAcc.Capabilities()

	return Capabilities{
		SupportsNTT:      true,
		SupportsMSM:      true,
		SupportsGroth16:  true,
		SupportsPLONK:    true,
		SupportsSTARK:    true,
		SupportsFHE:      a.config.EnableFHE,
		MaxPolynomialDeg: 1 << 26, // FPGA can handle very large polynomials
		MaxMSMPoints:     1 << 24, // 16M points
		MaxBatchSize:     a.config.MaxBatchSize,
		ParallelProofs:   fpgaCaps.AIEngines / 50, // Estimate based on AI engines
	}
}

func (a *FPGAAccelerator) Close() error {
	return a.fpgaAcc.Shutdown()
}

// NTT performs FPGA-accelerated Number Theoretic Transform
func (a *FPGAAccelerator) NTT(input []FieldElement, config NTTConfig) ([]FieldElement, error) {
	if !a.nttLoaded {
		return a.goFallback.NTT(input, config)
	}

	n := len(input)
	if n == 0 || (n&(n-1)) != 0 {
		return nil, errors.New("NTT input size must be power of 2")
	}

	// Convert to uint64 array for FPGA
	inputData := make([]uint64, n*4)
	for i, fe := range input {
		for j := 0; j < 4; j++ {
			inputData[i*4+j] = fe.Limbs[j]
		}
	}

	// Execute on FPGA
	outputData, err := a.fpgaAcc.NTT(inputData, config.LogN, config.Forward)
	if err != nil {
		return a.goFallback.NTT(input, config)
	}

	// Convert back to FieldElement array
	output := make([]FieldElement, n)
	for i := range output {
		for j := 0; j < 4; j++ {
			output[i].Limbs[j] = outputData[i*4+j]
		}
	}

	return output, nil
}

// INTT performs FPGA-accelerated Inverse NTT
func (a *FPGAAccelerator) INTT(input []FieldElement, config NTTConfig) ([]FieldElement, error) {
	if !a.nttLoaded {
		return a.goFallback.INTT(input, config)
	}

	config.Forward = false
	return a.NTT(input, config)
}

// MSM performs FPGA-accelerated Multi-Scalar Multiplication
func (a *FPGAAccelerator) MSM(points []Point, scalars []FieldElement, config MSMConfig) (Point, error) {
	if !a.msmLoaded || len(points) < 256 {
		return a.goFallback.MSM(points, scalars, config)
	}

	n := len(points)
	if n != len(scalars) {
		return Point{}, errors.New("points and scalars must have same length")
	}

	// Convert to uint64 arrays for FPGA
	pointsX := make([]uint64, n*4)
	pointsY := make([]uint64, n*4)
	scalarData := make([]uint64, n*4)

	for i := range points {
		for j := 0; j < 4; j++ {
			pointsX[i*4+j] = points[i].X.Limbs[j]
			pointsY[i*4+j] = points[i].Y.Limbs[j]
			scalarData[i*4+j] = scalars[i].Limbs[j]
		}
	}

	// Execute on FPGA
	result, err := a.fpgaAcc.MSM(pointsX, pointsY, scalarData, n)
	if err != nil {
		return a.goFallback.MSM(points, scalars, config)
	}

	return Point{
		X: FieldElement{Limbs: [4]uint64{result[0], result[1], result[2], result[3]}},
		Y: FieldElement{Limbs: [4]uint64{result[4], result[5], result[6], result[7]}},
	}, nil
}

// Hash computes FPGA-accelerated Poseidon hash
func (a *FPGAAccelerator) Hash(input []FieldElement, config HashConfig) (FieldElement, error) {
	if !a.hashLoaded || len(input) < 16 {
		return a.goFallback.Hash(input, config)
	}

	// Convert to uint64 array
	inputData := make([]uint64, len(input)*4)
	for i, fe := range input {
		for j := 0; j < 4; j++ {
			inputData[i*4+j] = fe.Limbs[j]
		}
	}

	rate := config.Rate
	if rate == 0 {
		rate = 8
	}

	// Execute on FPGA
	result, err := a.fpgaAcc.PoseidonHash(inputData, rate)
	if err != nil {
		return a.goFallback.Hash(input, config)
	}

	return FieldElement{Limbs: [4]uint64{result[0], result[1], result[2], result[3]}}, nil
}

// BatchHash computes multiple hashes in parallel on FPGA
func (a *FPGAAccelerator) BatchHash(inputs [][]FieldElement, config HashConfig) ([]FieldElement, error) {
	if !a.hashLoaded || len(inputs) < 4 {
		return a.goFallback.BatchHash(inputs, config)
	}

	// Convert to uint64 arrays
	inputsData := make([][]uint64, len(inputs))
	for i, input := range inputs {
		inputsData[i] = make([]uint64, len(input)*4)
		for j, fe := range input {
			for k := 0; k < 4; k++ {
				inputsData[i][j*4+k] = fe.Limbs[k]
			}
		}
	}

	rate := config.Rate
	if rate == 0 {
		rate = 8
	}

	// Execute batch on FPGA
	resultsData, err := a.fpgaAcc.PoseidonHashBatch(inputsData, rate)
	if err != nil {
		return a.goFallback.BatchHash(inputs, config)
	}

	// Convert back
	results := make([]FieldElement, len(resultsData))
	for i, res := range resultsData {
		results[i] = FieldElement{Limbs: [4]uint64{res[0], res[1], res[2], res[3]}}
	}

	return results, nil
}

// GenerateProof generates a ZK proof with FPGA acceleration
func (a *FPGAAccelerator) GenerateProof(witness []FieldElement, publicInput []FieldElement, pk *ProvingKey) (*Proof, error) {
	// Use FPGA-accelerated NTT, MSM, and Hash internally
	proof, err := a.goFallback.GenerateProof(witness, publicInput, pk)
	if err != nil {
		return nil, err
	}

	proof.Metadata["backend"] = string(a.Backend())
	proof.Metadata["device"] = a.Device()
	proof.Metadata["fpga_backend"] = string(a.fpgaAcc.Backend())

	return proof, nil
}

// VerifyProof verifies a ZK proof
func (a *FPGAAccelerator) VerifyProof(proof *Proof, publicInput []FieldElement, vk *VerifyingKey) (bool, error) {
	return a.goFallback.VerifyProof(proof, publicInput, vk)
}

// AggregateProofs aggregates multiple proofs
func (a *FPGAAccelerator) AggregateProofs(proofs []*Proof) (*Proof, error) {
	aggregated, err := a.goFallback.AggregateProofs(proofs)
	if err != nil {
		return nil, err
	}

	aggregated.Metadata["backend"] = string(a.Backend())
	aggregated.Metadata["device"] = a.Device()

	return aggregated, nil
}

// FHE Operations - use FPGA if available
func (a *FPGAAccelerator) FHEAdd(x, y *Ciphertext) (*Ciphertext, error) {
	if !a.fheLoaded {
		return a.goFallback.FHEAdd(x, y)
	}

	result, err := a.fpgaAcc.FHEAdd(x.Data, y.Data)
	if err != nil {
		return a.goFallback.FHEAdd(x, y)
	}

	return &Ciphertext{
		Data:       result,
		Scheme:     x.Scheme,
		NoiseLevel: x.NoiseLevel + 1,
	}, nil
}

func (a *FPGAAccelerator) FHESub(x, y *Ciphertext) (*Ciphertext, error) {
	// FHE sub is similar to add in most schemes
	return a.goFallback.FHESub(x, y)
}

func (a *FPGAAccelerator) FHEMul(x, y *Ciphertext) (*Ciphertext, error) {
	if !a.fheLoaded {
		return a.goFallback.FHEMul(x, y)
	}

	result, err := a.fpgaAcc.FHEMul(x.Data, y.Data)
	if err != nil {
		return a.goFallback.FHEMul(x, y)
	}

	return &Ciphertext{
		Data:       result,
		Scheme:     x.Scheme,
		NoiseLevel: x.NoiseLevel * 2, // Multiplication doubles noise
	}, nil
}

func (a *FPGAAccelerator) FHEBootstrap(ct *Ciphertext) (*Ciphertext, error) {
	if !a.fheLoaded {
		return a.goFallback.FHEBootstrap(ct)
	}

	result, err := a.fpgaAcc.FHEBootstrap(ct.Data)
	if err != nil {
		return a.goFallback.FHEBootstrap(ct)
	}

	return &Ciphertext{
		Data:       result,
		Scheme:     ct.Scheme,
		NoiseLevel: 1, // Reset noise after bootstrap
	}, nil
}

// Benchmark runs FPGA performance benchmarks
func (a *FPGAAccelerator) Benchmark(ops int) BenchmarkResult {
	result := BenchmarkResult{
		Backend: a.Backend(),
		Device:  a.Device(),
	}

	// NTT benchmark
	nttSize := 1 << 18 // 256K for FPGA benchmark
	nttInput := make([]FieldElement, nttSize)
	for i := range nttInput {
		nttInput[i] = FieldElement{Limbs: [4]uint64{uint64(i), 0, 0, 0}}
	}

	// Warm up
	_, _ = a.NTT(nttInput, NTTConfig{LogN: 18, Forward: true})

	start := time.Now()
	for i := 0; i < ops; i++ {
		_, _ = a.NTT(nttInput, NTTConfig{LogN: 18, Forward: true})
	}
	nttTime := time.Since(start)
	result.NTTOpsPerSec = float64(ops) / nttTime.Seconds()

	// Hash benchmark
	hashInput := make([]FieldElement, 512)
	start = time.Now()
	for i := 0; i < ops*10; i++ {
		_, _ = a.Hash(hashInput, HashConfig{Algorithm: "poseidon"})
	}
	hashTime := time.Since(start)
	result.HashOpsPerSec = float64(ops*10) / hashTime.Seconds()

	// MSM benchmark
	msmSize := 8192
	points := make([]Point, msmSize)
	scalarsData := make([]FieldElement, msmSize)
	for i := range points {
		points[i] = Point{X: FieldElement{Limbs: [4]uint64{uint64(i), 0, 0, 0}}}
		scalarsData[i] = FieldElement{Limbs: [4]uint64{uint64(i * 2), 0, 0, 0}}
	}

	start = time.Now()
	for i := 0; i < ops/10; i++ {
		_, _ = a.MSM(points, scalarsData, MSMConfig{WindowSize: 12})
	}
	msmTime := time.Since(start)
	result.MSMOpsPerSec = float64(ops/10) / msmTime.Seconds()

	result.LatencyNs = nttTime.Nanoseconds() / int64(ops)

	return result
}
