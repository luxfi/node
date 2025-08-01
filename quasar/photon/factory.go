// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package photon

import (
	"time"
)

// Factory creates photon samplers with specific configurations
// (Previously PollFactory in Avalanche)
type Factory struct {
	k             int
	seed          int64
	termThreshold int
}

// NewFactory creates a new photon sampler factory
func NewFactory(k int) *Factory {
	return &Factory{
		k:             k,
		seed:          time.Now().UnixNano(),
		termThreshold: k * 3, // Default: 3 rounds of early termination
	}
}

// WithSeed sets a specific random seed
func (f *Factory) WithSeed(seed int64) *Factory {
	f.seed = seed
	return f
}

// WithTerminationThreshold sets the early termination threshold
func (f *Factory) WithTerminationThreshold(threshold int) *Factory {
	f.termThreshold = threshold
	return f
}

// NewBinary creates a binary photon sampler
func (f *Factory) NewBinary() Sampler {
	return NewBinaryPhotonSampler(f.k, f.seed)
}

// NewUnary creates a unary photon sampler
func (f *Factory) NewUnary() Sampler {
	return NewUnaryPhotonSampler(f.k, f.seed, f.termThreshold)
}

// SamplerType identifies the type of photon sampler
type SamplerType int

const (
	Binary SamplerType = iota
	Unary
)

// New creates a sampler of the specified type
func (f *Factory) New(samplerType SamplerType) Sampler {
	switch samplerType {
	case Binary:
		return f.NewBinary()
	case Unary:
		return f.NewUnary()
	default:
		return f.NewBinary()
	}
}