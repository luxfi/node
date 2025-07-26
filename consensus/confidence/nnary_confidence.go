// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/sampling"
)

var _ sampling.Nnary = (*nnaryConfidence)(nil)

func newNnaryConfidence(alphaPreference int, terminationConditions []terminationCondition, choice ids.ID) nnaryConfidence {
	return nnaryConfidence{
		nnaryThreshold:     newNnaryThreshold(alphaPreference, terminationConditions, choice),
		preference:         choice,
		preferenceStrength: make(map[ids.ID]int),
	}
}

// nnaryConfidence is a naive implementation of a multi-color confidence instance
type nnaryConfidence struct {
	// wrap the n-nary threshold logic
	nnaryThreshold

	// preference is the choice with the largest number of polls which preferred
	// it. Ties are broken by switching choice lazily
	preference ids.ID

	// maxPreferenceStrength is the maximum value stored in [preferenceStrength]
	maxPreferenceStrength int

	// preferenceStrength tracks the total number of network polls which
	// preferred that choice
	preferenceStrength map[ids.ID]int
}

func (sb *nnaryConfidence) Preference() ids.ID {
	// It is possible, with low probability, that the threshold preference is
	// not equal to the confidence preference when threshold finalizes. However,
	// this case is handled for completion. Therefore, if threshold is
	// finalized, then our finalized threshold choice should be preferred.
	if sb.Finalized() {
		return sb.nnaryThreshold.Preference()
	}
	return sb.preference
}

func (sb *nnaryConfidence) RecordPoll(count int, choice ids.ID) {
	if count >= sb.alphaPreference {
		preferenceStrength := sb.preferenceStrength[choice] + 1
		sb.preferenceStrength[choice] = preferenceStrength

		if preferenceStrength > sb.maxPreferenceStrength {
			sb.preference = choice
			sb.maxPreferenceStrength = preferenceStrength
		}
	}
	sb.nnaryThreshold.RecordPoll(count, choice)
}

func (sb *nnaryConfidence) String() string {
	return fmt.Sprintf("SB(Preference = %s, PreferenceStrength = %d, %s)",
		sb.preference, sb.maxPreferenceStrength, &sb.nnaryThreshold)
}
