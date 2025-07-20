// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"
)

// snowflakeTest defines the interface for testing snowflake instances
type snowflakeTest[T comparable] interface {
	RecordPoll(count int, choice T)
	RecordUnsuccessfulPoll()
	AssertEqual(expectedConfidences []int, expectedFinalized bool, expectedPreference T)
	Finalized() bool
	Preference() T
}

type testCase[T comparable] struct {
	name string
	f    func(*testing.T, snowflakeTestConstructor[T], ...T)
}

type snowflakeTestConstructor[T comparable] func(t *testing.T, alphaPreference int, terminationConditions []terminationCondition) snowflakeTest[T]

func getErrorDrivenSnowflakeSingleChoiceSuite[T comparable]() []testCase[T] {
	return []testCase[T]{
		{name: "ConfidenceCorrectness", f: func(t *testing.T, constructor snowflakeTestConstructor[T], choices ...T) {
			if len(choices) > 0 {
				ErrorDrivenSnowflakeSingleChoiceConfidenceCorrectnessTest(t, constructor, choices[0])
			}
		}},
		{name: "Unanimity", f: func(t *testing.T, constructor snowflakeTestConstructor[T], choices ...T) {
			if len(choices) > 0 {
				ErrorDrivenSnowflakeSingleChoiceUnanimityTest(t, constructor, choices[0])
			}
		}},
		{name: "Finalization", f: func(t *testing.T, constructor snowflakeTestConstructor[T], choices ...T) {
			if len(choices) > 0 {
				ErrorDrivenSnowflakeSingleChoiceFinalizationTest(t, constructor, choices[0])
			}
		}},
	}
}

func getErrorDrivenSnowflakeMultiChoiceSuite[T comparable]() []testCase[T] {
	return []testCase[T]{
		{name: "ConfidenceCorrectness", f: func(t *testing.T, constructor snowflakeTestConstructor[T], choices ...T) {
			if len(choices) >= 2 {
				ErrorDrivenSnowflakeMultiChoiceConfidenceCorrectnessTest(t, constructor, choices[0], choices[1])
			}
		}},
		{name: "NoFEvidenceNoProgress", f: func(t *testing.T, constructor snowflakeTestConstructor[T], choices ...T) {
			if len(choices) >= 2 {
				ErrorDrivenSnowflakeMultiChoiceNoFEvidenceNoProgressTest(t, constructor, choices[0], choices[1])
			}
		}},
		{name: "ProgressWithFEvidence", f: func(t *testing.T, constructor snowflakeTestConstructor[T], choices ...T) {
			if len(choices) >= 2 {
				ErrorDrivenSnowflakeMultiChoiceProgressWithFEvidenceTest(t, constructor, choices[0], choices[1])
			}
		}},
	}
}

// Test implementations
func ErrorDrivenSnowflakeSingleChoiceConfidenceCorrectnessTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice T) {
	t.Skip("Test not implemented")
}

func ErrorDrivenSnowflakeSingleChoiceUnanimityTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice T) {
	t.Skip("Test not implemented")
}

func ErrorDrivenSnowflakeSingleChoiceFinalizationTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice T) {
	t.Skip("Test not implemented")
}

func ErrorDrivenSnowflakeMultiChoiceConfidenceCorrectnessTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice0, choice1 T) {
	t.Skip("Test not implemented")
}

func ErrorDrivenSnowflakeMultiChoiceNoFEvidenceNoProgressTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice0, choice1 T) {
	t.Skip("Test not implemented")
}

func ErrorDrivenSnowflakeMultiChoiceProgressWithFEvidenceTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice0, choice1 T) {
	t.Skip("Test not implemented")
}