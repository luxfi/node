// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"fmt"

	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/ids"
)

// nnaryThreshold is the implementation of an n-nary threshold instance
// that can be embedded by confidence
type nnaryThreshold struct {
	// wrap the n-nary sampler logic
	sampling.MultiSampler

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

func newNnaryThreshold(alphaPreference int, terminationConditions []terminationCondition, choice ids.ID) nnaryThreshold {
	return nnaryThreshold{
		MultiSampler:          sampling.NewMultiSampler(choice),
		alphaPreference:       alphaPreference,
		terminationConditions: terminationConditions,
		confidence:            make([]int, len(terminationConditions)),
	}
}

func (*nnaryThreshold) Add(_ ids.ID) {}

func (nt *nnaryThreshold) RecordPoll(count int, choice ids.ID) {
	if nt.finalized {
		return // This instance is already decided.
	}

	if count < nt.alphaPreference {
		nt.RecordUnsuccessfulPoll()
		return
	}

	// If I am changing my preference, reset confidence counters
	// before recording a successful poll on the sampler instance.
	if choice != nt.Preference() {
		clear(nt.confidence)
	}
	nt.MultiSampler.RecordSuccessfulPoll(choice)

	for i, terminationCondition := range nt.terminationConditions {
		// If I did not reach this alpha threshold, I did not
		// reach any more alpha thresholds.
		// Clear the remaining confidence counters.
		if count < terminationCondition.alphaConfidence {
			clear(nt.confidence[i:])
			return
		}

		// I reached this alpha threshold, increment the confidence counter
		// and check if I can finalize.
		nt.confidence[i]++
		if nt.confidence[i] >= terminationCondition.beta {
			nt.finalized = true
			return
		}
	}
}

func (nt *nnaryThreshold) RecordUnsuccessfulPoll() {
	clear(nt.confidence)
}

func (nt *nnaryThreshold) Finalized() bool {
	return nt.finalized
}

func (nt *nnaryThreshold) String() string {
	return fmt.Sprintf("NT(Confidence = %v, Finalized = %v, %s)",
		nt.confidence,
		nt.finalized,
		&nt.MultiSampler)
}