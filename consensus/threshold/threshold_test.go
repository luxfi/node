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
		{name: "ConfidenceCorrectness", f: ErrorDrivenSnowflakeSingleChoiceConfidenceCorrectnessTest[T]},
		{name: "Unanimity", f: ErrorDrivenSnowflakeSingleChoiceUnanimityTest[T]},
		{name: "Finalization", f: ErrorDrivenSnowflakeSingleChoiceFinalizationTest[T]},
	}
}

func getErrorDrivenSnowflakeMultiChoiceSuite[T comparable]() []testCase[T] {
	return []testCase[T]{
		{name: "ConfidenceCorrectness", f: ErrorDrivenSnowflakeMultiChoiceConfidenceCorrectnessTest[T]},
		{name: "NoFEvidenceNoProgress", f: ErrorDrivenSnowflakeMultiChoiceNoFEvidenceNoProgressTest[T]},
		{name: "ProgressWithFEvidence", f: ErrorDrivenSnowflakeMultiChoiceProgressWithFEvidenceTest[T]},
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