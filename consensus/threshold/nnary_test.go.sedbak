// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"
	

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

func TestNnaryConsensusflake(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := NewNetwork(alphaPreference, terminationConditions, Red)
	sf.Add(Blue)
	sf.Add(Green)

	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, Blue)
	require.Equal(Blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaPreference, Red)
	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())

	sf.RecordPoll(alphaPreference, Blue)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())

	sf.RecordPoll(alphaConfidence, Blue)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())
}

func TestNnaryConsensusflakeConfidenceReset(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 4
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := NewNetwork(alphaPreference, terminationConditions, Red)
	sf.Add(Blue)
	sf.Add(Green)

	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	// Increase Blue's confidence without finalizing
	for i := 0; i < beta-1; i++ {
		sf.RecordPoll(alphaConfidence, Blue)
		require.Equal(Blue, sf.Preference())
		require.False(sf.Finalized())
	}

	// Increase Red's confidence without finalizing
	for i := 0; i < beta-1; i++ {
		sf.RecordPoll(alphaConfidence, Red)
		require.Equal(Red, sf.Preference())
		require.False(sf.Finalized())
	}

	// One more round of voting for Red should accept Red
	sf.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())
}

func TestVirtuousNnaryConsensusflake(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sb := NewNetwork(alphaPreference, terminationConditions, Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sb.Preference())
	require.True(sb.Finalized())
}

type multiThresholdTest struct {
	require *require.Assertions

	multiThreshold
}

func newNnaryConsensusflakeTest(t *testing.T, alphaPreference int, terminationConditions []terminationCondition) consensusflakeTest[ids.ID] {
	require := require.New(t)

	return &multiThresholdTest{
		require:        require,
		multiThreshold: NewNetwork(alphaPreference, terminationConditions, Red),
	}
}

func (sf *multiThresholdTest) RecordPoll(count int, choice ids.ID) {
	sf.multiThreshold.RecordPoll(count, choice)
}

func (sf *multiThresholdTest) AssertEqual(expectedConfidences []int, expectedFinalized bool, expectedPreference ids.ID) {
	sf.require.Equal(expectedPreference, sf.Preference())
	sf.require.Equal(expectedConfidences, sf.multiThreshold.confidence)
	sf.require.Equal(expectedFinalized, sf.Finalized())
}

func TestNnaryConsensusflakeErrorDrivenSingleChoice(t *testing.T) {
	for _, test := range getErrorDrivenConsensusflakeSingleChoiceSuite[ids.ID]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newNnaryConsensusflakeTest, Red)
		})
	}
}

func TestNnaryConsensusflakeErrorDrivenMultiChoice(t *testing.T) {
	for _, test := range getErrorDrivenConsensusflakeMultiChoiceSuite[ids.ID]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newNnaryConsensusflakeTest, Red, Green)
		})
	}
}
