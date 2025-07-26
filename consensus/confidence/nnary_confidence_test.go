// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

var (
	Red   = ids.Empty.Prefix(0)
	Blue  = ids.Empty.Prefix(1)
	Green = ids.Empty.Prefix(2)
)

func TestNnaryConfidence(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sb := newNnaryConfidence(alphaPreference, terminationConditions, Red)
	sb.Add(Blue)
	sb.Add(Green)

	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Blue)
	require.Equal(Blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Red)
	require.Equal(Blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaPreference, Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaPreference, Blue)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Blue)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Blue)
	require.Equal(Blue, sb.Preference())
	require.True(sb.Finalized())
}

func TestVirtuousNnaryConfidence(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 1
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sb := newNnaryConfidence(alphaPreference, terminationConditions, Red)

	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sb.Preference())
	require.True(sb.Finalized())
}

func TestNaryConfidenceRecordUnsuccessfulPoll(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sb := newNnaryConfidence(alphaPreference, terminationConditions, Red)
	sb.Add(Blue)

	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Blue)
	require.Equal(Blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordUnsuccessfulPoll()

	sb.RecordPoll(alphaConfidence, Blue)

	require.Equal(Blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Blue)

	require.Equal(Blue, sb.Preference())
	require.True(sb.Finalized())

	expected := "SB(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES, PreferenceStrength = 3, NT(Confidence = [2], Finalized = true, SL(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES)))"
	require.Equal(expected, sb.String())

	for i := 0; i < 4; i++ {
		sb.RecordPoll(alphaConfidence, Red)

		require.Equal(Blue, sb.Preference())
		require.True(sb.Finalized())
	}
}

func TestNaryConfidenceDifferentThresholdColor(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sb := newNnaryConfidence(alphaPreference, terminationConditions, Red)
	sb.Add(Blue)

	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Blue)

	require.Equal(Blue, sb.nnaryThreshold.Preference())

	sb.RecordPoll(alphaConfidence, Red)

	require.Equal(Blue, sb.Preference())
	require.Equal(Red, sb.nnaryThreshold.Preference())
}
