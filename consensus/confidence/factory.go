// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package confidence

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/sampling"
)

// NewNnaryConfidence returns a new nnary confidence instance
func NewNnaryConfidence(alphaPreference, alphaConfidence, beta int, choice ids.ID) sampling.Nnary {
	sb := newNnaryConfidence(alphaPreference, newSingleTerminationCondition(alphaConfidence, beta), choice)
	return &sb
}

// NewUnaryConfidence returns a new unary confidence instance
func NewUnaryConfidence(alphaPreference, alphaConfidence, beta int) sampling.Unary {
	sb := newUnaryConfidence(alphaPreference, newSingleTerminationCondition(alphaConfidence, beta))
	return &sb
}
