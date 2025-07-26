// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampling

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/bag"
)

var _ Consensus = (*Flat)(nil)

func NewFlat(factory Factory, params Parameters, choice ids.ID) Consensus {
	return &Flat{
		Nnary:  factory.NewNnary(params, choice),
		params: params,
	}
}

// Flat is a naive implementation of a multi-choice snow instance
type Flat struct {
	// wraps the n-nary snow logic
	Nnary

	// params contains all the configurations of a snow instance
	params Parameters
}

func (f *Flat) RecordPoll(votes bag.Bag[ids.ID]) bool {
	pollMode, numVotes := votes.Mode()
	f.Nnary.RecordPoll(numVotes, pollMode)
	return numVotes >= f.params.AlphaPreference
}
