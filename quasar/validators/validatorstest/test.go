// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package validatorstest provides test utilities for validators
package validatorstest

import (
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/validators"
)

// TestState is a test implementation of validators.State
type TestState struct {
	T          testing.TB
	validators map[ids.ID]map[ids.NodeID]*validators.Validator
}

// NewTestState creates a new test state
func NewTestState(t testing.TB) *TestState {
	return &TestState{
		T:          t,
		validators: make(map[ids.ID]map[ids.NodeID]*validators.Validator),
	}
}

// GetValidatorSet returns the validator set for the given subnet at the given height
func (ts *TestState) GetValidatorSet(pChainHeight uint64, subnetID ids.ID) (map[ids.NodeID]*validators.Validator, error) {
	subnet, ok := ts.validators[subnetID]
	if !ok {
		return nil, nil
	}
	return subnet, nil
}

// SetValidator sets a validator for the given subnet
func (ts *TestState) SetValidator(pChainHeight uint64, subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	if ts.validators[subnetID] == nil {
		ts.validators[subnetID] = make(map[ids.NodeID]*validators.Validator)
	}
	
	ts.validators[subnetID][nodeID] = &validators.Validator{
		NodeID: nodeID,
		Weight: weight,
		TxID:   ids.GenerateTestID(),
	}
	return nil
}