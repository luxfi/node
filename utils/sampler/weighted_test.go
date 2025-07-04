// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	safemath "github.com/luxfi/node/utils/math"
)

func TestWeightedInitializeOverflow(t *testing.T) {
	s := &weightedHeap{}
	err := s.Initialize([]uint64{1, math.MaxUint64})
	require.ErrorIs(t, err, safemath.ErrOverflow)
}

func WeightedOutOfRangeTest(t *testing.T, s Weighted) {
	require := require.New(t)

	require.NoError(s.Initialize([]uint64{1}))

	_, ok := s.Sample(1)
	require.False(ok)
}

func WeightedSingletonTest(t *testing.T, s Weighted) {
	require := require.New(t)

	require.NoError(s.Initialize([]uint64{1}))

	index, ok := s.Sample(0)
	require.True(ok)
	require.Zero(index)
}

func WeightedWithZeroTest(t *testing.T, s Weighted) {
	require := require.New(t)

	require.NoError(s.Initialize([]uint64{0, 1}))

	index, ok := s.Sample(0)
	require.True(ok)
	require.Equal(1, index)
}

func WeightedDistributionTest(t *testing.T, s Weighted) {
	require := require.New(t)

	require.NoError(s.Initialize([]uint64{1, 1, 2, 3, 4}))

	counts := make([]int, 5)
	for i := uint64(0); i < 11; i++ {
		index, ok := s.Sample(i)
		require.True(ok)
		counts[index]++
	}
	require.Equal([]int{1, 1, 2, 3, 4}, counts)
}
