// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

// Weighted defines how to sample a specified valued based on a provided
// weighted distribution
type Weighted interface {
	Initialize(weights []uint64) error
	Sample(sampleValue uint64) (int, bool)
}

func NewWeighted() Weighted {
	return &weightedHeap{}
}
