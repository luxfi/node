// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validatorstest

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/validators"
)

type State struct {
	validators map[ids.NodeID]*validators.GetValidatorOutput
}

func NewState() *State {
	return &State{
		validators: make(map[ids.NodeID]*validators.GetValidatorOutput),
	}
}

func (s *State) GetValidator(_ context.Context, nodeID ids.NodeID) (*validators.GetValidatorOutput, error) {
	validator, ok := s.validators[nodeID]
	if !ok {
		return nil, validators.ErrValidatorNotFound
	}
	return validator, nil
}

func (s *State) PutValidator(nodeID ids.NodeID, validator *validators.GetValidatorOutput) {
	s.validators[nodeID] = validator
}