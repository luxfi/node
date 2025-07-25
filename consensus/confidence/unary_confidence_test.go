// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func UnaryConfidenceStateTest(t *testing.T, sb *unaryConfidence, expectedPreferenceStrength int, expectedConfidence []int, expectedFinalized bool) {
	require := require.New(t)

	require.Equal(expectedPreferenceStrength, sb.preferenceStrength)
	require.Equal(expectedConfidence, sb.confidence)
	require.Equal(expectedFinalized, sb.Finalized())
}

func TestUnaryConfidence(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sb := newUnaryConfidence(alphaPreference, terminationConditions)

	sb.RecordPoll(alphaConfidence)
	UnaryConfidenceStateTest(t, &sb, 1, []int{1}, false)

	sb.RecordPoll(alphaPreference)
	UnaryConfidenceStateTest(t, &sb, 2, []int{0}, false)

	sb.RecordPoll(alphaConfidence)
	UnaryConfidenceStateTest(t, &sb, 3, []int{1}, false)

	sb.RecordUnsuccessfulPoll()
	UnaryConfidenceStateTest(t, &sb, 3, []int{0}, false)

	sb.RecordPoll(alphaConfidence)
	UnaryConfidenceStateTest(t, &sb, 4, []int{1}, false)

	sbCloneIntf := sb.Clone()
	require.IsType(&unaryConfidence{}, sbCloneIntf)
	sbClone := sbCloneIntf.(*unaryConfidence)

	UnaryConfidenceStateTest(t, sbClone, 4, []int{1}, false)

	binaryConfidence := sbClone.Extend(0)

	expected := "SB(Preference = 0, PreferenceStrength[0] = 4, PreferenceStrength[1] = 0, BT(Confidence = [1], Finalized = false, SL(Preference = 0)))"
	require.Equal(expected, binaryConfidence.String())

	binaryConfidence.RecordUnsuccessfulPoll()
	for i := 0; i < 5; i++ {
		require.Zero(binaryConfidence.Preference())
		require.False(binaryConfidence.Finalized())
		binaryConfidence.RecordPoll(alphaConfidence, 1)
		binaryConfidence.RecordUnsuccessfulPoll()
	}

	require.Equal(1, binaryConfidence.Preference())
	require.False(binaryConfidence.Finalized())

	binaryConfidence.RecordPoll(alphaConfidence, 1)
	require.Equal(1, binaryConfidence.Preference())
	require.False(binaryConfidence.Finalized())

	binaryConfidence.RecordPoll(alphaConfidence, 1)
	require.Equal(1, binaryConfidence.Preference())
	require.True(binaryConfidence.Finalized())

	expected = "SB(PreferenceStrength = 4, UT(Confidence = [1], Finalized = false))"
	require.Equal(expected, sb.String())
}
