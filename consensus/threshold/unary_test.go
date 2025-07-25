// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func UnarySnowflakeStateTest(t *testing.T, sf *unaryThreshold, expectedConfidences []int, expectedFinalized bool) {
	require := require.New(t)

	require.Equal(expectedConfidences, sf.confidence)
	require.Equal(expectedFinalized, sf.Finalized())
}

func TestUnarySnowflake(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newUnarySnowflake(alphaPreference, terminationConditions)

	sf.RecordPoll(alphaConfidence)
	UnarySnowflakeStateTest(t, &sf, []int{1}, false)

	sf.RecordUnsuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, []int{0}, false)

	sf.RecordPoll(alphaConfidence)
	UnarySnowflakeStateTest(t, &sf, []int{1}, false)

	sfCloneIntf := sf.Clone()
	require.IsType(&unaryThreshold{}, sfCloneIntf)
	sfClone := sfCloneIntf.(*unaryThreshold)

	UnarySnowflakeStateTest(t, sfClone, []int{1}, false)

	binaryThreshold := sfClone.Extend(0)

	binaryThreshold.RecordUnsuccessfulPoll()

	binaryThreshold.RecordPoll(alphaConfidence, 1)

	require.False(binaryThreshold.Finalized())

	binaryThreshold.RecordPoll(alphaConfidence, 1)

	require.Equal(1, binaryThreshold.Preference())
	require.True(binaryThreshold.Finalized())

	sf.RecordPoll(alphaConfidence)
	UnarySnowflakeStateTest(t, &sf, []int{2}, true)

	sf.RecordUnsuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, []int{0}, true)

	sf.RecordPoll(alphaConfidence)
	UnarySnowflakeStateTest(t, &sf, []int{1}, true)
}

type unaryThresholdTest struct {
	require *require.Assertions

	unaryThreshold
}

func newUnarySnowflakeTest(t *testing.T, alphaPreference int, terminationConditions []terminationCondition) snowflakeTest[struct{}] {
	require := require.New(t)

	return &unaryThresholdTest{
		require:        require,
		unaryThreshold: newUnarySnowflake(alphaPreference, terminationConditions),
	}
}

func (sf *unaryThresholdTest) RecordPoll(count int, _ struct{}) {
	sf.unaryThreshold.RecordPoll(count)
}

func (sf *unaryThresholdTest) AssertEqual(expectedConfidences []int, expectedFinalized bool, _ struct{}) {
	sf.require.Equal(expectedConfidences, sf.unaryThreshold.confidence)
	sf.require.Equal(expectedFinalized, sf.Finalized())
}

func (sf *unaryThresholdTest) Preference() struct{} {
	return struct{}{}
}

// TODO: Fix these tests after refactoring is complete
func TestUnarySnowflakeErrorDriven(t *testing.T) {
	for _, test := range getErrorDrivenSnowflakeSingleChoiceSuite[struct{}]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newUnarySnowflakeTest, struct{}{})
		})
	}
}
