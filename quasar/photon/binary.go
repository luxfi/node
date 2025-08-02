// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package photon

import (
	"context"
	"math/rand"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/set"
)

// BinaryPhotonSampler implements binary consensus sampling
// (Previously BinaryPoll in Avalanche)
//
// Binary photon sampling chooses between exactly two options,
// like a photon passing through a beam splitter.
type BinaryPhotonSampler struct {
	k      int
	random *rand.Rand
}

// NewBinaryPhotonSampler creates a new binary photon sampler
func NewBinaryPhotonSampler(k int, seed int64) *BinaryPhotonSampler {
	return &BinaryPhotonSampler{
		k:      k,
		random: rand.New(rand.NewSource(seed)),
	}
}

// Sample selects K validators uniformly at random
func (b *BinaryPhotonSampler) Sample(ctx context.Context, validatorSet set.Set[ids.NodeID], k int) (set.Set[ids.NodeID], error) {
	if k > validatorSet.Len() {
		k = validatorSet.Len()
	}

	// Convert to slice for random sampling
	validators := validatorSet.List()
	
	// Fisher-Yates shuffle for uniform sampling
	selected := set.NewSet[ids.NodeID](k)
	for i := 0; i < k; i++ {
		j := b.random.Intn(len(validators) - i)
		validators[i], validators[j+i] = validators[j+i], validators[i]
		selected.Add(validators[i])
	}

	return selected, nil
}

// GetK returns the sample size
func (b *BinaryPhotonSampler) GetK() int {
	return b.k
}

// Reset clears the sampler state
func (b *BinaryPhotonSampler) Reset() {
	// Binary sampler is stateless between rounds
}