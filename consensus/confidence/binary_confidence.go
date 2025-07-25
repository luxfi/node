// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"fmt"

	"github.com/luxfi/node/consensus/sampling"
)

var _ sampling.Binary = (*binaryConfidence)(nil)

func newBinaryConfidence(alphaPreference int, terminationConditions []terminationCondition, choice int) binaryConfidence {
	return binaryConfidence{
		binaryThreshold: newBinaryThreshold(alphaPreference, terminationConditions, choice),
		preference:      choice,
	}
}

// binaryConfidence is the implementation of a binary confidence instance
type binaryConfidence struct {
	// wrap the binary threshold logic
	binaryThreshold

	// preference is the choice with the largest number of polls which preferred
	// the color. Ties are broken by switching choice lazily
	preference int

	// preferenceStrength tracks the total number of network polls which
	// preferred each choice
	preferenceStrength [2]int
}

func (sb *binaryConfidence) Preference() int {
	// It is possible, with low probability, that the threshold preference is
	// not equal to the confidence preference when threshold finalizes. However,
	// this case is handled for completion. Therefore, if threshold is
	// finalized, then our finalized threshold choice should be preferred.
	if sb.Finalized() {
		return sb.binaryThreshold.Preference()
	}
	return sb.preference
}

func (sb *binaryConfidence) RecordPoll(count, choice int) {
	if count >= sb.alphaPreference {
		sb.preferenceStrength[choice]++
		if sb.preferenceStrength[choice] > sb.preferenceStrength[1-choice] {
			sb.preference = choice
		}
	}
	sb.binaryThreshold.RecordPoll(count, choice)
}

func (sb *binaryConfidence) String() string {
	return fmt.Sprintf(
		"SB(Preference = %d, PreferenceStrength[0] = %d, PreferenceStrength[1] = %d, %s)",
		sb.preference,
		sb.preferenceStrength[0],
		sb.preferenceStrength[1],
		&sb.binaryThreshold)
}
