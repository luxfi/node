// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"
)

// thresholdTest defines the interface for testing threshold instances
type thresholdTest[T comparable] interface {
	RecordPoll(count int, choice T)
	RecordUnsuccessfulPoll()
	AssertEqual(expectedConfidences []int, expectedFinalized bool, expectedPreference T)
	Finalized() bool
	Preference() T
}

type testCase[T comparable] struct {
	name string
	f    func(*testing.T, flatTestConstructor[T], ...T)
}

type flatTestConstructor[T comparable] func(t *testing.T, alphaPreference int, terminationConditions []terminationCondition) thresholdTest[T]

func getErrorDrivenThresholdSingleChoiceSuite[T comparable]() []testCase[T] {
	return []testCase[T]{
		{name: "ConfidenceCorrectness", f: func(t *testing.T, constructor flatTestConstructor[T], choices ...T) {
			if len(choices) > 0 {
				ErrorDrivenThresholdSingleChoiceConfidenceCorrectnessTest(t, constructor, choices[0])
			}
		}},
		{name: "Unanimity", f: func(t *testing.T, constructor flatTestConstructor[T], choices ...T) {
			if len(choices) > 0 {
				ErrorDrivenThresholdSingleChoiceUnanimityTest(t, constructor, choices[0])
			}
		}},
		{name: "Finalization", f: func(t *testing.T, constructor flatTestConstructor[T], choices ...T) {
			if len(choices) > 0 {
				ErrorDrivenThresholdSingleChoiceFinalizationTest(t, constructor, choices[0])
			}
		}},
	}
}

func getErrorDrivenThresholdMultiChoiceSuite[T comparable]() []testCase[T] {
	return []testCase[T]{
		{name: "ConfidenceCorrectness", f: func(t *testing.T, constructor flatTestConstructor[T], choices ...T) {
			if len(choices) >= 2 {
				ErrorDrivenThresholdMultiChoiceConfidenceCorrectnessTest(t, constructor, choices[0], choices[1])
			}
		}},
		{name: "NoFEvidenceNoProgress", f: func(t *testing.T, constructor flatTestConstructor[T], choices ...T) {
			if len(choices) >= 2 {
				ErrorDrivenThresholdMultiChoiceNoFEvidenceNoProgressTest(t, constructor, choices[0], choices[1])
			}
		}},
		{name: "ProgressWithFEvidence", f: func(t *testing.T, constructor flatTestConstructor[T], choices ...T) {
			if len(choices) >= 2 {
				ErrorDrivenThresholdMultiChoiceProgressWithFEvidenceTest(t, constructor, choices[0], choices[1])
			}
		}},
	}
}

// Test implementations
func ErrorDrivenThresholdSingleChoiceConfidenceCorrectnessTest[T comparable](t *testing.T, newThresholdTest flatTestConstructor[T], choice T) {
	// Test that confidence values are correctly maintained during voting

	// Create a threshold with alpha_preference = 2
	sf := newThresholdTest(t, 2, []terminationCondition{
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
	sf2 := newThresholdTest(t, 2, []terminationCondition{
		{alphaConfidence: 3, beta: 2},
	})
	sf2.RecordPoll(2, choice)
	sf2.AssertEqual([]int{0}, false, choice) // Below alphaConfidence, no progress
	sf2.RecordPoll(3, choice)
	sf2.AssertEqual([]int{1}, false, choice) // Reached alphaConfidence
	sf2.RecordUnsuccessfulPoll()
	sf2.AssertEqual([]int{0}, false, choice) // Reset on unsuccessful poll
}

func ErrorDrivenThresholdSingleChoiceUnanimityTest[T comparable](t *testing.T, newThresholdTest flatTestConstructor[T], choice T) {
	// Test that unanimous votes always increase confidence

	// Create a threshold with high alpha values
	sf := newThresholdTest(t, 5, []terminationCondition{
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

func ErrorDrivenThresholdSingleChoiceFinalizationTest[T comparable](t *testing.T, newThresholdTest flatTestConstructor[T], choice T) {
	// Test that finalization occurs exactly when beta consecutive successful rounds happen

	// Create a threshold with beta = 4
	sf := newThresholdTest(t, 2, []terminationCondition{
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
	sf2 := newThresholdTest(t, 2, []terminationCondition{
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

func ErrorDrivenThresholdMultiChoiceConfidenceCorrectnessTest[T comparable](t *testing.T, newThresholdTest flatTestConstructor[T], choice0, choice1 T) {
	// Test that confidence is maintained correctly when switching between choices

	// Create a threshold starting with choice0
	sf := newThresholdTest(t, 2, []terminationCondition{
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

func ErrorDrivenThresholdMultiChoiceNoFEvidenceNoProgressTest[T comparable](t *testing.T, newThresholdTest flatTestConstructor[T], choice0, choice1 T) {
	// Test that without sufficient evidence (below alpha), no progress is made

	// Create a threshold with alpha_preference = 3
	sf := newThresholdTest(t, 3, []terminationCondition{
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

func ErrorDrivenThresholdMultiChoiceProgressWithFEvidenceTest[T comparable](t *testing.T, newThresholdTest flatTestConstructor[T], choice0, choice1 T) {
	// Test that with sufficient evidence (F+1 votes), progress is made

	// Create a threshold with reasonable parameters
	sf := newThresholdTest(t, 2, []terminationCondition{
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
	sf2 := newThresholdTest(t, 2, []terminationCondition{
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
