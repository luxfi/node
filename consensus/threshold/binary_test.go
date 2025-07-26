// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBinaryThreshold(t *testing.T) {
	require := require.New(t)

	blue := 0
	red := 1

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newBinaryThreshold(alphaPreference, terminationConditions, red)

	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)

	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, red)

	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)

	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaPreference, red)
	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)
	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)
	require.Equal(blue, sf.Preference())
	require.True(sf.Finalized())
}

type binaryThresholdTest struct {
	require *require.Assertions

	binaryThreshold
}

func newBinaryThresholdTest(t *testing.T, alphaPreference int, terminationConditions []terminationCondition) thresholdTest[int] {
	require := require.New(t)

	return &binaryThresholdTest{
		require:         require,
		binaryThreshold: newBinaryThreshold(alphaPreference, terminationConditions, 0),
	}
}

func (sf *binaryThresholdTest) RecordPoll(count int, choice int) {
	sf.binaryThreshold.RecordPoll(count, choice)
}

func (sf *binaryThresholdTest) AssertEqual(expectedConfidences []int, expectedFinalized bool, expectedPreference int) {
	sf.require.Equal(expectedPreference, sf.Preference())
	sf.require.Equal(expectedConfidences, sf.binaryThreshold.confidence)
	sf.require.Equal(expectedFinalized, sf.Finalized())
}

func TestBinaryThresholdErrorDrivenSingleChoice(t *testing.T) {
	for _, test := range getErrorDrivenThresholdSingleChoiceSuite[int]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newBinaryThresholdTest, 0)
		})
	}
}

func TestBinaryThresholdErrorDrivenMultiChoice(t *testing.T) {
	for _, test := range getErrorDrivenThresholdMultiChoiceSuite[int]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newBinaryThresholdTest, 0, 1)
		})
	}
}
