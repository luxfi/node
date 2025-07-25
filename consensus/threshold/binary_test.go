// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBinarySnowflake(t *testing.T) {
	require := require.New(t)

	blue := 0
	red := 1

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newBinarySnowflake(alphaPreference, terminationConditions, red)

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

func newBinarySnowflakeTest(t *testing.T, alphaPreference int, terminationConditions []terminationCondition) snowflakeTest[int] {
	require := require.New(t)

	return &binaryThresholdTest{
		require:         require,
		binaryThreshold: newBinarySnowflake(alphaPreference, terminationConditions, 0),
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

func TestBinarySnowflakeErrorDrivenSingleChoice(t *testing.T) {
	for _, test := range getErrorDrivenSnowflakeSingleChoiceSuite[int]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newBinarySnowflakeTest, 0)
		})
	}
}

func TestBinarySnowflakeErrorDrivenMultiChoice(t *testing.T) {
	for _, test := range getErrorDrivenSnowflakeMultiChoiceSuite[int]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newBinarySnowflakeTest, 0, 1)
		})
	}
}
