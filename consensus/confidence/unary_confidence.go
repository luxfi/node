// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"fmt"

	"github.com/luxfi/node/consensus/sampling"
)

var _ sampling.Unary = (*unaryConfidence)(nil)

func newUnaryConfidence(alphaPreference int, terminationConditions []terminationCondition) unaryConfidence {
	return unaryConfidence{
		unaryThreshold: newUnaryThreshold(alphaPreference, terminationConditions),
	}
}

// unaryConfidence is the implementation of a unary confidence instance
type unaryConfidence struct {
	// wrap the unary threshold logic
	unaryThreshold

	// preferenceStrength tracks the total number of polls with a preference
	preferenceStrength int
}

func (sb *unaryConfidence) RecordPoll(count int) {
	if count >= sb.alphaPreference {
		sb.preferenceStrength++
	}
	sb.unaryThreshold.RecordPoll(count)
}

func (sb *unaryConfidence) Extend(choice int) sampling.Binary {
	bs := &binaryConfidence{
		binaryThreshold: sb.unaryThreshold.Extend(choice),
		preference:      choice,
	}
	bs.preferenceStrength[choice] = sb.preferenceStrength
	return bs
}

func (sb *unaryConfidence) Clone() sampling.Unary {
	newConfidence := *sb
	newConfidence.unaryThreshold = sb.unaryThreshold.Clone()
	return &newConfidence
}

func (sb *unaryConfidence) String() string {
	return fmt.Sprintf("SB(PreferenceStrength = %d, %s)",
		sb.preferenceStrength,
		&sb.unaryThreshold)
}
