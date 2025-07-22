// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampling

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/utils/bag"
)

func TestFlat(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K:               3,
		AlphaPreference: 2,
		AlphaConfidence: 3,
		Beta:            2,
	}
	factory := confidenceTestFactory{}
	f := NewFlat(factory, params, Red)
	f.Add(Green)
	f.Add(Blue)

	pref := f.Preference()
	t.Logf("Initial preference: %x (Red: %x)", pref[:], Red[:])
	require.Equal(Red, f.Preference())
	require.False(f.Finalized())

	threeBlue := bag.Of(Blue, Blue, Blue)
	require.True(f.RecordPoll(threeBlue))
	pref = f.Preference()
	t.Logf("After threeBlue poll - Preference: %x (Blue: %x)", pref[:], Blue[:])
	require.Equal(Blue, f.Preference())
	require.False(f.Finalized())

	twoGreen := bag.Of(Green, Green)
	require.True(f.RecordPoll(twoGreen))
	pref = f.Preference()
	t.Logf("After twoGreen poll - Preference: %x, Expected: %x (Blue)", pref[:], Blue[:])
	require.Equal(Blue, f.Preference())
	require.False(f.Finalized())

	threeGreen := bag.Of(Green, Green, Green)
	require.True(f.RecordPoll(threeGreen))
	require.Equal(Green, f.Preference())
	require.False(f.Finalized())

	// Reset the confidence from previous round
	oneEach := bag.Of(Red, Green, Blue)
	require.False(f.RecordPoll(oneEach))
	require.Equal(Green, f.Preference())
	require.False(f.Finalized())

	require.True(f.RecordPoll(threeGreen))
	require.Equal(Green, f.Preference())
	require.False(f.Finalized()) // Not finalized before Beta rounds

	require.True(f.RecordPoll(threeGreen))
	require.Equal(Green, f.Preference())
	require.True(f.Finalized())

	expected := "SB(Preference = 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w, PreferenceStrength = 4, SF(Confidence = [2], Finalized = true, SL(Preference = 2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w)))"
	require.Equal(expected, f.String())
}
