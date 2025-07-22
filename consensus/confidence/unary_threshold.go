// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"fmt"
	"slices"

	"github.com/luxfi/node/consensus/sampling"
)

// unaryThreshold is the implementation of a unary threshold instance
// that can be embedded by confidence
type unaryThreshold struct {
	// alphaPreference is the threshold required to update the preference
	alphaPreference int

	// terminationConditions gives the ascending ordered list of alphaConfidence values
	// required to increment the corresponding confidence counter.
	// The corresponding beta values give the threshold required to finalize this instance.
	terminationConditions []terminationCondition

	// confidence is the number of consecutive successful polls for a given
	// alphaConfidence threshold.
	// This instance finalizes when confidence[i] >= terminationConditions[i].beta for any i
	confidence []int

	// finalized prevents the state from changing after the required number of
	// consecutive polls has been reached
	finalized bool
}

func newUnaryThreshold(alphaPreference int, terminationConditions []terminationCondition) unaryThreshold {
	return unaryThreshold{
		alphaPreference:       alphaPreference,
		terminationConditions: terminationConditions,
		confidence:            make([]int, len(terminationConditions)),
	}
}

func (ut *unaryThreshold) RecordPoll(count int) {
	for i, terminationCondition := range ut.terminationConditions {
		// If I did not reach this alpha threshold, I did not
		// reach any more alpha thresholds.
		// Clear the remaining confidence counters.
		if count < terminationCondition.alphaConfidence {
			clear(ut.confidence[i:])
			return
		}

		// I reached this alpha threshold, increment the confidence counter
		// and check if I can finalize.
		ut.confidence[i]++
		if ut.confidence[i] >= terminationCondition.beta {
			ut.finalized = true
			return
		}
	}
}

func (ut *unaryThreshold) RecordUnsuccessfulPoll() {
	clear(ut.confidence)
}

func (ut *unaryThreshold) Finalized() bool {
	return ut.finalized
}

func (ut *unaryThreshold) Extend(choice int) binaryThreshold {
	return binaryThreshold{
		BinarySampler:         sampling.NewBinarySampler(choice),
		confidence:            slices.Clone(ut.confidence),
		alphaPreference:       ut.alphaPreference,
		terminationConditions: ut.terminationConditions,
		finalized:             ut.finalized,
	}
}

func (ut *unaryThreshold) Clone() unaryThreshold {
	newThreshold := *ut
	newThreshold.confidence = slices.Clone(ut.confidence)
	return newThreshold
}

func (ut *unaryThreshold) String() string {
	return fmt.Sprintf("UT(Confidence = %v, Finalized = %v)",
		ut.confidence,
		ut.finalized)
}