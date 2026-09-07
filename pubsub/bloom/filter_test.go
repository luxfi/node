// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
)

func TestNew(t *testing.T) {
	var (
		require  = require.New(t)
		maxN     = 10000
		p        = 0.1
		maxBytes = 1 * constants.MiB // 1 MiB
	)
	f, err := New(maxN, p, maxBytes)
	require.NoError(err)
	require.NotNil(f)

	f.Add([]byte("hello"))

	checked := f.Check([]byte("hello"))
	require.True(checked, "should have contained the key")

	checked = f.Check([]byte("bye"))
	require.False(checked, "shouldn't have contained the key")
}

// TestNewRejectsSaturatedEntries pins the size cap against the saturating value
// bloom.OptimalEntries returns for an extreme request. Forming the byte total as
// 1+numHashes*bytesPerHash+numEntries wraps past math.MaxInt into a negative
// number, which clears the cap and reaches make([]byte, math.MaxInt).
func TestNewRejectsSaturatedEntries(t *testing.T) {
	require := require.New(t)

	// A collision probability of 0 saturates OptimalEntries at math.MaxInt.
	f, err := New(1, 0, 1*constants.MiB)
	require.ErrorIs(err, errMaxBytes)
	require.Nil(f)

	// A large element count with a small collision probability lands on the
	// same clamp by way of the entries-per-element multiplier.
	f, err = New(math.MaxInt, 1e-10, 1*constants.MiB)
	require.ErrorIs(err, errMaxBytes)
	require.Nil(f)
}
