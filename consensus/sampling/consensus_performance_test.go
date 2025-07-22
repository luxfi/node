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

// testNnary is a stub implementation for testing
type testNnary struct {
	params     Parameters
	choice     ids.ID
	preference ids.ID
	finalized  bool
}

func (t *testNnary) Add(newChoice ids.ID) {
	if t.preference == ids.Empty {
		t.preference = newChoice
	}
}

func (t *testNnary) Preference() ids.ID {
	if t.preference == ids.Empty {
		return t.choice
	}
	return t.preference
}

func (t *testNnary) RecordPoll(count int, choice ids.ID) {
	if count >= t.params.AlphaPreference {
		t.preference = choice
	}
}

func (t *testNnary) RecordUnsuccessfulPoll() {}

func (t *testNnary) Finalized() bool {
	return t.finalized
}

func (t *testNnary) String() string {
	return fmt.Sprintf("testNnary{preference: %s, finalized: %v}", t.preference, t.finalized)
}

// testUnary is a stub implementation for testing
type testUnary struct {
	params    Parameters
	finalized bool
}

func (t *testUnary) RecordPoll(count int) {}

func (t *testUnary) RecordUnsuccessfulPoll() {}

func (t *testUnary) Finalized() bool {
	return t.finalized
}

func (t *testUnary) Extend(originalPreference int) Binary {
	return &testBinary{}
}

func (t *testUnary) Clone() Unary {
	return &testUnary{params: t.params, finalized: t.finalized}
}

func (t *testUnary) String() string {
	return fmt.Sprintf("testUnary{finalized: %v}", t.finalized)
}

// testBinary is a stub implementation for testing
type testBinary struct {
	preference int
	finalized  bool
}

func (t *testBinary) Preference() int {
	return t.preference
}

func (t *testBinary) RecordPoll(count, choice int) {
	t.preference = choice
}

func (t *testBinary) RecordUnsuccessfulPoll() {}

func (t *testBinary) Finalized() bool {
	return t.finalized
}

func (t *testBinary) String() string {
	return fmt.Sprintf("testBinary{preference: %d, finalized: %v}", t.preference, t.finalized)
}

// confidenceTestFactory is a test factory for confidence consensus
type confidenceTestFactory struct{}

func (confidenceTestFactory) NewNnary(params Parameters, choice ids.ID) Nnary {
	return &testNnary{params: params, choice: choice, preference: choice}
}

func (confidenceTestFactory) NewUnary(params Parameters) Unary {
	return &testUnary{params: params}
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
		seed   uint64 = 0
		source        = prng.NewMT19937()
		factory       = confidenceTestFactory{}
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
