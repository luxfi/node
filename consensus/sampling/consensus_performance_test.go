// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampling

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/mathext/prng"

	"github.com/luxfi/node/ids"
)

// confidenceTestFactory is a test factory for confidence consensus
type confidenceTestFactory struct{}

func (confidenceTestFactory) NewNnary(params Parameters, choice ids.ID) Nnary {
	return &nnaryConfidence{
		params:             params,
		preference:         choice,
		confidence:         make([]int, 1<<params.K),
		preferenceCount:    make([]int, 1<<params.K),
		consecutivePolls:   0,
		preferenceStrength: make(map[ids.ID]int),
	}
}

func (confidenceTestFactory) NewUnary(params Parameters) Unary {
	return &unaryConfidence{
		params: params,
	}
}

// nnaryConfidence implements a simplified confidence algorithm for testing
type nnaryConfidence struct {
	params             Parameters
	preference         ids.ID
	confidence         []int
	preferenceCount    []int
	consecutivePolls   int
	finalized          bool
	preferenceStrength map[ids.ID]int
	maxPrefStrength    int
}

func (n *nnaryConfidence) Add(choice ids.ID) {
	// No-op in confidence
}

func (n *nnaryConfidence) Preference() ids.ID {
	return n.preference
}

func (n *nnaryConfidence) RecordPoll(count int, choice ids.ID) {
	// Always update preference strength for successful polls
	if count >= n.params.AlphaPreference {
		n.preferenceStrength[choice]++
		if n.preferenceStrength[choice] > n.maxPrefStrength {
			n.preference = choice
			n.maxPrefStrength = n.preferenceStrength[choice]
		}
	}

	// Update confidence for finalization
	if count >= n.params.AlphaConfidence {
		if choice == n.preference {
			n.consecutivePolls++
			if n.consecutivePolls >= n.params.Beta {
				n.finalized = true
			}
		} else {
			n.consecutivePolls = 1
		}
	} else {
		n.consecutivePolls = 0
	}
}

func (n *nnaryConfidence) RecordUnsuccessfulPoll() {
	n.consecutivePolls = 0
}

func (n *nnaryConfidence) Finalized() bool {
	return n.finalized
}

func (n *nnaryConfidence) String() string {
	// Mimic the real confidence output format
	confidenceStr := fmt.Sprintf("[%d]", n.consecutivePolls)
	innerStr := fmt.Sprintf("SF(Confidence = %s, Finalized = %v, SL(Preference = %s))",
		confidenceStr, n.finalized, n.preference)

	return fmt.Sprintf("SB(Preference = %s, PreferenceStrength = %d, %s)",
		n.preference, n.maxPrefStrength, innerStr)
}

// unaryConfidence implements a simplified unary confidence for testing
type unaryConfidence struct {
	params             Parameters
	consecutivePolls   int
	finalized          bool
	preferenceStrength int
}

func (u *unaryConfidence) RecordPoll(count int) {
	if count >= u.params.AlphaPreference {
		u.preferenceStrength++
	}

	if count >= u.params.AlphaConfidence {
		u.consecutivePolls++
		if u.consecutivePolls >= u.params.Beta {
			u.finalized = true
		}
	} else {
		u.consecutivePolls = 0
	}
}

func (u *unaryConfidence) RecordUnsuccessfulPoll() {
	u.consecutivePolls = 0
}

func (u *unaryConfidence) Finalized() bool {
	return u.finalized
}

func (u *unaryConfidence) Extend(choice int) Binary {
	return &binaryConfidence{
		params:             u.params,
		preference:         choice,
		preferenceStrength: map[int]int{choice: u.preferenceStrength},
		consecutivePolls:   u.consecutivePolls,
		finalized:          u.finalized,
		innerPreference:    choice, // Inner preference starts as the extended choice
	}
}

func (u *unaryConfidence) Clone() Unary {
	return &unaryConfidence{
		params:             u.params,
		consecutivePolls:   u.consecutivePolls,
		finalized:          u.finalized,
		preferenceStrength: u.preferenceStrength,
	}
}

func (u *unaryConfidence) String() string {
	// Mimic the real unary confidence output format
	confidenceStr := fmt.Sprintf("[%d]", u.consecutivePolls)
	innerStr := fmt.Sprintf("SF(Confidence = %s, Finalized = %v)",
		confidenceStr, u.finalized)

	return fmt.Sprintf("SB(PreferenceStrength = %d, %s)",
		u.preferenceStrength, innerStr)
}

// binaryConfidence implements a simplified binary confidence for testing
type binaryConfidence struct {
	params             Parameters
	preference         int // Confidence preference (based on preference strength)
	consecutivePolls   int
	finalized          bool
	preferenceStrength map[int]int
	innerPreference    int // Inner sampler preference (updated on every successful poll)
}

func (b *binaryConfidence) Preference() int {
	return b.preference
}

func (b *binaryConfidence) RecordPoll(count, choice int) {
	// Check if we're changing the inner preference
	changingPreference := false
	if count >= b.params.AlphaPreference {
		if b.preferenceStrength == nil {
			b.preferenceStrength = make(map[int]int)
		}

		// Check if inner preference will change
		if choice != b.innerPreference {
			changingPreference = true
		}

		b.preferenceStrength[choice]++

		// Update inner sampler preference on every successful poll
		b.innerPreference = choice

		// Update confidence preference only if the new choice has strictly greater strength
		if b.preferenceStrength[choice] > b.preferenceStrength[1-choice] {
			b.preference = choice
		}
	}

	// If we're changing preference, clear confidence before proceeding
	if changingPreference && !b.finalized {
		b.consecutivePolls = 0
	}

	if count >= b.params.AlphaConfidence {
		if choice == b.innerPreference {
			if !changingPreference {
				b.consecutivePolls++
			} else {
				// Starting fresh with new preference
				b.consecutivePolls = 1
			}
			if b.consecutivePolls >= b.params.Beta {
				b.finalized = true
			}
		}
	} else {
		b.consecutivePolls = 0
	}
}

func (b *binaryConfidence) RecordUnsuccessfulPoll() {
	b.consecutivePolls = 0
}

func (b *binaryConfidence) Finalized() bool {
	return b.finalized
}

func (b *binaryConfidence) String() string {
	// Mimic the real binary confidence output format
	confidenceStr := fmt.Sprintf("[%d]", b.consecutivePolls)
	// Use inner preference for SL output
	innerStr := fmt.Sprintf("SF(Confidence = %s, Finalized = %v, SL(Preference = %d))",
		confidenceStr, b.finalized, b.innerPreference)

	// Build preference strength string
	if b.preferenceStrength == nil {
		b.preferenceStrength = make(map[int]int)
	}
	strengthStr := ""
	for i := 0; i <= 1; i++ {
		if i > 0 {
			strengthStr += ", "
		}
		strengthStr += fmt.Sprintf("PreferenceStrength[%d] = %d", i, b.preferenceStrength[i])
	}

	return fmt.Sprintf("SB(Preference = %d, %s, %s)",
		b.preference, strengthStr, innerStr)
}

// Test that a network running the lower AlphaPreference converges faster than a
// network running equal Alpha values.
func TestDualAlphaOptimization(t *testing.T) {
	require := require.New(t)

	var (
		numColors = 10
		numNodes  = 100
		params    = Parameters{
			K:               20,
			AlphaPreference: 15,
			AlphaConfidence: 15,
			Beta:            20,
		}
		seed    uint64 = 0
		source         = prng.NewMT19937()
		factory        = confidenceTestFactory{}
	)

	singleAlphaNetwork := NewNetwork(factory, params, numColors, source)

	params.AlphaPreference = params.K/2 + 1
	dualAlphaNetwork := NewNetwork(factory, params, numColors, source)

	source.Seed(seed)
	for i := 0; i < numNodes; i++ {
		dualAlphaNetwork.AddNode(NewTree)
	}

	source.Seed(seed)
	for i := 0; i < numNodes; i++ {
		singleAlphaNetwork.AddNode(NewTree)
	}

	// Although this can theoretically fail with a correct implementation, it
	// shouldn't in practice
	runNetworksInLockstep(require, seed, source, dualAlphaNetwork, singleAlphaNetwork)
}

// Test that a network running the confidence tree converges faster than a network
// running the flat confidence protocol.
func TestTreeConvergenceOptimization(t *testing.T) {
	require := require.New(t)

	var (
		numColors        = 10
		numNodes         = 100
		params           = DefaultParameters
		seed      uint64 = 0
		source           = prng.NewMT19937()
	)

	factory := confidenceTestFactory{}
	treeNetwork := NewNetwork(factory, params, numColors, source)
	flatNetwork := NewNetwork(factory, params, numColors, source)

	source.Seed(seed)
	for i := 0; i < numNodes; i++ {
		treeNetwork.AddNode(NewTree)
	}

	source.Seed(seed)
	for i := 0; i < numNodes; i++ {
		flatNetwork.AddNode(NewFlat)
	}

	// Although this can theoretically fail with a correct implementation, it
	// shouldn't in practice
	runNetworksInLockstep(require, seed, source, treeNetwork, flatNetwork)
}

func runNetworksInLockstep(require *require.Assertions, seed uint64, source *prng.MT19937, fast *Network, slow *Network) {
	numRounds := 0
	for !fast.Finalized() && !fast.Disagreement() && !slow.Finalized() && !slow.Disagreement() {
		source.Seed(uint64(numRounds) + seed)
		fast.Round()

		source.Seed(uint64(numRounds) + seed)
		slow.Round()
		numRounds++
	}

	require.False(fast.Disagreement())
	require.False(slow.Disagreement())
	require.True(fast.Finalized())
	require.True(fast.Agreement())
}
