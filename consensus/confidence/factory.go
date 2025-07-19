// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/ids"
)

// NewNnarySnowball returns a new nnary snowball instance
func NewNnarySnowball(alphaPreference, alphaConfidence, beta int, choice ids.ID) sampling.Nnary {
	sb := newNnarySnowball(alphaPreference, newSingleTerminationCondition(alphaConfidence, beta), choice)
	return &sb
}

// NewUnarySnowball returns a new unary snowball instance
func NewUnarySnowball(alphaPreference, alphaConfidence, beta int) sampling.Unary {
	sb := newUnarySnowball(alphaPreference, newSingleTerminationCondition(alphaConfidence, beta))
	return &sb
}