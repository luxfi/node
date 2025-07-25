// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"fmt"

	"github.com/luxfi/node/consensus/sampling"
)

// binaryThreshold is the implementation of a binary threshold instance
// that can be embedded by confidence
type binaryThreshold struct {
	// wrap the binary sampler logic
	sampling.BinarySampler

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

func newBinaryThreshold(alphaPreference int, terminationConditions []terminationCondition, choice int) binaryThreshold {
	return binaryThreshold{
		BinarySampler:         sampling.NewBinarySampler(choice),
		alphaPreference:       alphaPreference,
		terminationConditions: terminationConditions,
		confidence:            make([]int, len(terminationConditions)),
	}
}

func (bt *binaryThreshold) RecordPoll(count, choice int) {
	if bt.finalized {
		return // This instance is already decided.
	}

	if count < bt.alphaPreference {
		bt.RecordUnsuccessfulPoll()
		return
	}

	// If I am changing my preference, reset confidence counters
	// before recording a successful poll on the sampler instance.
	if choice != bt.Preference() {
		clear(bt.confidence)
	}
	bt.BinarySampler.RecordSuccessfulPoll(choice)

	for i, terminationCondition := range bt.terminationConditions {
		// If I did not reach this alpha threshold, I did not
		// reach any more alpha thresholds.
		// Clear the remaining confidence counters.
		if count < terminationCondition.alphaConfidence {
			clear(bt.confidence[i:])
			return
		}

		// I reached this alpha threshold, increment the confidence counter
		// and check if I can finalize.
		bt.confidence[i]++
		if bt.confidence[i] >= terminationCondition.beta {
			bt.finalized = true
			return
		}
	}
}

func (bt *binaryThreshold) RecordUnsuccessfulPoll() {
	clear(bt.confidence)
}

func (bt *binaryThreshold) Finalized() bool {
	return bt.finalized
}

func (bt *binaryThreshold) String() string {
	return fmt.Sprintf("BT(Confidence = %v, Finalized = %v, %s)",
		bt.confidence,
		bt.finalized,
		&bt.BinarySampler)
}