// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/sampling"
)

// NewNnaryThreshold returns a new nnary threshold instance
func NewNnaryThreshold(alphaPreference, alphaConfidence, beta int, choice ids.ID) sampling.Nnary {
	sf := NewNetwork(alphaPreference, newSingleTerminationCondition(alphaConfidence, beta), choice)
	return &sf
}

// NewUnaryThreshold returns a new unary threshold instance
func NewUnaryThreshold(alphaPreference, alphaConfidence, beta int) sampling.Unary {
	sf := NewFlat(alphaPreference, newSingleTerminationCondition(alphaConfidence, beta))
	return &sf
}
