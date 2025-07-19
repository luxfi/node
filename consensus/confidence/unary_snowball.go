// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"fmt"

	"github.com/luxfi/node/consensus/sampling"
)

var _ sampling.Unary = (*unarySnowball)(nil)

func newUnarySnowball(alphaPreference int, terminationConditions []terminationCondition) unarySnowball {
	return unarySnowball{
		unaryThreshold: newUnaryThreshold(alphaPreference, terminationConditions),
	}
}

// unarySnowball is the implementation of a unary snowball instance
type unarySnowball struct {
	// wrap the unary threshold logic
	unaryThreshold

	// preferenceStrength tracks the total number of polls with a preference
	preferenceStrength int
}

func (sb *unarySnowball) RecordPoll(count int) {
	if count >= sb.alphaPreference {
		sb.preferenceStrength++
	}
	sb.unaryThreshold.RecordPoll(count)
}

func (sb *unarySnowball) Extend(choice int) sampling.Binary {
	bs := &binarySnowball{
		binaryThreshold: sb.unaryThreshold.Extend(choice),
		preference:      choice,
	}
	bs.preferenceStrength[choice] = sb.preferenceStrength
	return bs
}

func (sb *unarySnowball) Clone() sampling.Unary {
	newSnowball := *sb
	newSnowball.unaryThreshold = sb.unaryThreshold.Clone()
	return &newSnowball
}

func (sb *unarySnowball) String() string {
	return fmt.Sprintf("SB(PreferenceStrength = %d, %s)",
		sb.preferenceStrength,
		&sb.unaryThreshold)
}
