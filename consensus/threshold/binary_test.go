// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"
	

	"github.com/stretchr/testify/require"
)

func TestBinaryConsensusflake(t *testing.T) {
	require := require.New(t)

	blue := 0
	red := 1

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newBinaryConsensusflake(alphaPreference, terminationConditions, red)

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

func newBinaryConsensusflakeTest(t *testing.T, alphaPreference int, terminationConditions []terminationCondition) consensusflakeTest[int] {
	require := require.New(t)

	return &binaryThresholdTest{
		require:         require,
		binaryThreshold: newBinaryConsensusflake(alphaPreference, terminationConditions, 0),
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

func TestBinaryConsensusflakeErrorDrivenSingleChoice(t *testing.T) {
	for _, test := range getErrorDrivenConsensusflakeSingleChoiceSuite[int]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newBinaryConsensusflakeTest, 0)
		})
	}
}

func TestBinaryConsensusflakeErrorDrivenMultiChoice(t *testing.T) {
	for _, test := range getErrorDrivenConsensusflakeMultiChoiceSuite[int]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newBinaryConsensusflakeTest, 0, 1)
		})
	}
}
