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
	// Test that confidence values are correctly maintained during voting
	
	// Create a snowflake with alpha_preference = 2
	sf := newSnowflakeTest(t, 2, []terminationCondition{
		{alphaConfidence: 2, beta: 3}, // Changed beta from 1 to 3 to avoid early finalization
	})
	
	// Initial state should have zero confidence
	sf.AssertEqual([]int{0}, false, choice)
	
	// Single vote shouldn't change confidence (below alpha_preference)
	sf.RecordPoll(1, choice)
	sf.AssertEqual([]int{0}, false, choice)
	
	// Two votes should increase confidence
	sf.RecordPoll(2, choice)
	sf.AssertEqual([]int{1}, false, choice)
	
	// Three votes should further increase confidence
	sf.RecordPoll(3, choice)
	sf.AssertEqual([]int{2}, false, choice) // Not yet finalized (beta=3)
	
	// Fourth vote should finalize
	sf.RecordPoll(2, choice)
	sf.AssertEqual([]int{3}, true, choice)
	
	// Unsuccessful poll should reset confidence
	sf2 := newSnowflakeTest(t, 2, []terminationCondition{
		{alphaConfidence: 3, beta: 2},
	})
	sf2.RecordPoll(2, choice)
	sf2.AssertEqual([]int{0}, false, choice) // Below alphaConfidence, no progress
	sf2.RecordPoll(3, choice)
	sf2.AssertEqual([]int{1}, false, choice) // Reached alphaConfidence
	sf2.RecordUnsuccessfulPoll()
	sf2.AssertEqual([]int{0}, false, choice) // Reset on unsuccessful poll
}

func ErrorDrivenSnowflakeSingleChoiceUnanimityTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice T) {
	// Test that unanimous votes always increase confidence
	
	// Create a snowflake with high alpha values
	sf := newSnowflakeTest(t, 5, []terminationCondition{
		{alphaConfidence: 5, beta: 3},
	})
	
	// Initial state
	sf.AssertEqual([]int{0}, false, choice)
	
	// Unanimous vote with 10 nodes (above alpha)
	sf.RecordPoll(10, choice)
	sf.AssertEqual([]int{1}, false, choice)
	
	// Another unanimous vote
	sf.RecordPoll(10, choice)
	sf.AssertEqual([]int{2}, false, choice)
	
	// Third unanimous vote should finalize
	sf.RecordPoll(10, choice)
	sf.AssertEqual([]int{3}, true, choice)
}

func ErrorDrivenSnowflakeSingleChoiceFinalizationTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice T) {
	// Test that finalization occurs exactly when beta consecutive successful rounds happen
	
	// Create a snowflake with beta = 4
	sf := newSnowflakeTest(t, 2, []terminationCondition{
		{alphaConfidence: 2, beta: 4},
	})
	
	// Initial state
	sf.AssertEqual([]int{0}, false, choice)
	
	// Build up confidence over 3 rounds (not yet beta)
	for i := 0; i < 3; i++ {
		sf.RecordPoll(2, choice)
		sf.AssertEqual([]int{i + 1}, false, choice)
	}
	
	// 4th round should finalize (beta = 4)
	sf.RecordPoll(2, choice)
	sf.AssertEqual([]int{4}, true, choice)
	
	// Test that unsuccessful poll prevents finalization
	sf2 := newSnowflakeTest(t, 2, []terminationCondition{
		{alphaConfidence: 2, beta: 3},
	})
	
	// Build confidence
	sf2.RecordPoll(2, choice)
	sf2.AssertEqual([]int{1}, false, choice)
	sf2.RecordPoll(2, choice)
	sf2.AssertEqual([]int{2}, false, choice)
	
	// Unsuccessful poll resets
	sf2.RecordUnsuccessfulPoll()
	sf2.AssertEqual([]int{0}, false, choice)
	
	// Need to build up beta rounds again
	sf2.RecordPoll(2, choice)
	sf2.AssertEqual([]int{1}, false, choice)
	sf2.RecordPoll(2, choice)
	sf2.AssertEqual([]int{2}, false, choice)
	sf2.RecordPoll(2, choice)
	sf2.AssertEqual([]int{3}, true, choice)
}

func ErrorDrivenSnowflakeMultiChoiceConfidenceCorrectnessTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice0, choice1 T) {
	// Test that confidence is maintained correctly when switching between choices
	
	// Create a snowflake starting with choice0
	sf := newSnowflakeTest(t, 2, []terminationCondition{
		{alphaConfidence: 2, beta: 3},
	})
	
	// Initial preference should be choice0
	sf.AssertEqual([]int{0}, false, choice0)
	
	// Vote for choice0
	sf.RecordPoll(2, choice0)
	sf.AssertEqual([]int{1}, false, choice0)
	
	// Vote for choice1 (should switch preference and reset confidence)
	sf.RecordPoll(2, choice1)
	sf.AssertEqual([]int{1}, false, choice1)
	
	// Continue voting for choice1
	sf.RecordPoll(2, choice1)
	sf.AssertEqual([]int{2}, false, choice1)
	
	// Back to choice0 (should switch and reset)
	sf.RecordPoll(2, choice0)
	sf.AssertEqual([]int{1}, false, choice0)
}

func ErrorDrivenSnowflakeMultiChoiceNoFEvidenceNoProgressTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice0, choice1 T) {
	// Test that without sufficient evidence (below alpha), no progress is made
	
	// Create a snowflake with alpha_preference = 3
	sf := newSnowflakeTest(t, 3, []terminationCondition{
		{alphaConfidence: 3, beta: 2},
	})
	
	// Initial state with choice0
	sf.AssertEqual([]int{0}, false, choice0)
	
	// Vote with only 2 nodes (below alpha_preference of 3)
	sf.RecordPoll(2, choice0)
	sf.AssertEqual([]int{0}, false, choice0) // No progress
	
	// Even multiple rounds with insufficient votes don't progress
	sf.RecordPoll(2, choice0)
	sf.AssertEqual([]int{0}, false, choice0)
	
	// Try with choice1, still no progress
	sf.RecordPoll(2, choice1)
	sf.AssertEqual([]int{0}, false, choice0) // Preference doesn't change
	
	// Now with sufficient votes
	sf.RecordPoll(3, choice1)
	sf.AssertEqual([]int{1}, false, choice1) // Progress made
}

func ErrorDrivenSnowflakeMultiChoiceProgressWithFEvidenceTest[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice0, choice1 T) {
	// Test that with sufficient evidence (F+1 votes), progress is made
	
	// Create a snowflake with reasonable parameters
	sf := newSnowflakeTest(t, 2, []terminationCondition{
		{alphaConfidence: 2, beta: 2},
	})
	
	// Initial state
	sf.AssertEqual([]int{0}, false, choice0)
	
	// Vote for choice0 with sufficient evidence
	sf.RecordPoll(2, choice0)
	sf.AssertEqual([]int{1}, false, choice0)
	
	// Another vote finalizes (beta = 2)
	sf.RecordPoll(2, choice0)
	sf.AssertEqual([]int{2}, true, choice0)
	
	// Test alternating choices with sufficient evidence
	sf2 := newSnowflakeTest(t, 2, []terminationCondition{
		{alphaConfidence: 2, beta: 4},
	})
	
	// Alternate between choices, each with sufficient evidence
	sf2.RecordPoll(2, choice0)
	sf2.AssertEqual([]int{1}, false, choice0)
	
	sf2.RecordPoll(2, choice1)
	sf2.AssertEqual([]int{1}, false, choice1)
	
	sf2.RecordPoll(2, choice0)
	sf2.AssertEqual([]int{1}, false, choice0)
	
	// Stick with choice0 to build confidence
	sf2.RecordPoll(2, choice0)
	sf2.AssertEqual([]int{2}, false, choice0)
	
	sf2.RecordPoll(2, choice0)
	sf2.AssertEqual([]int{3}, false, choice0)
	
	sf2.RecordPoll(2, choice0)
	sf2.AssertEqual([]int{4}, true, choice0)
}