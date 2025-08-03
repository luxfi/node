// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package validatorsmock provides mock implementations for validators
package validatorsmock

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow/validators"
)

// State is a mock implementation of validators.State
type State struct {
	GetValidatorSetF func(pChainHeight uint64, subnetID ids.ID) (map[ids.NodeID]*validators.Validator, error)
}

// GetValidatorSet implements validators.State
func (s *State) GetValidatorSet(pChainHeight uint64, subnetID ids.ID) (map[ids.NodeID]*validators.Validator, error) {
	if s.GetValidatorSetF != nil {
		return s.GetValidatorSetF(pChainHeight, subnetID)
	}
	return nil, nil
}