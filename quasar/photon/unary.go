// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package photon

import (
	"context"
	"math/rand"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/set"
)

// UnaryPhotonSampler implements unary consensus sampling
// (Previously UnaryPoll in Avalanche)
//
// Unary photon sampling is used when there's only one option,
// like measuring photon presence/absence rather than direction.
type UnaryPhotonSampler struct {
	k           int
	random      *rand.Rand
	earlyTerms  map[ids.ID]int // Track early termination counts
	termThreshold int
}

// NewUnaryPhotonSampler creates a new unary photon sampler
func NewUnaryPhotonSampler(k int, seed int64, termThreshold int) *UnaryPhotonSampler {
	return &UnaryPhotonSampler{
		k:             k,
		random:        rand.New(rand.NewSource(seed)),
		earlyTerms:    make(map[ids.ID]int),
		termThreshold: termThreshold,
	}
}

// Sample selects K validators, with possible early termination
func (u *UnaryPhotonSampler) Sample(ctx context.Context, validatorSet set.Set[ids.NodeID], k int) (set.Set[ids.NodeID], error) {
	// For unary sampling, we may use fewer than K validators
	// if we've reached early termination threshold
	effectiveK := k
	if u.shouldReduceSample() {
		effectiveK = k / 2 // Reduce sample size for efficiency
		if effectiveK < 1 {
			effectiveK = 1
		}
	}

	if effectiveK > validatorSet.Len() {
		effectiveK = validatorSet.Len()
	}

	// Convert to slice for random sampling
	validators := validatorSet.List()
	
	// Fisher-Yates shuffle for uniform sampling
	selected := set.NewSet[ids.NodeID](effectiveK)
	for i := 0; i < effectiveK; i++ {
		j := u.random.Intn(len(validators) - i)
		validators[i], validators[j+i] = validators[j+i], validators[i]
		selected.Add(validators[i])
	}

	return selected, nil
}

// GetK returns the sample size
func (u *UnaryPhotonSampler) GetK() int {
	return u.k
}

// Reset clears the sampler state
func (u *UnaryPhotonSampler) Reset() {
	u.earlyTerms = make(map[ids.ID]int)
}

// RecordEarlyTermination records when a unary query terminates early
func (u *UnaryPhotonSampler) RecordEarlyTermination(containerID ids.ID) {
	u.earlyTerms[containerID]++
}

// shouldReduceSample determines if we should use a smaller sample
func (u *UnaryPhotonSampler) shouldReduceSample() bool {
	// If we've had many early terminations, reduce sample size
	totalTerms := 0
	for _, count := range u.earlyTerms {
		totalTerms += count
	}
	return totalTerms > u.termThreshold
}