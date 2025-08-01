// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/validators"
)

// mockValidatorSet is a simple validator set for testing
type mockValidatorSet struct {
	validators map[ids.NodeID]uint64
}

func NewMockValidatorSet(nodeValidators map[ids.NodeID]uint64) *mockValidatorSet {
	return &mockValidatorSet{
		validators: nodeValidators,
	}
}

func (m *mockValidatorSet) GetValidatorSet(height uint64) (validators.Set, error) {
	// Create a validators.Set from our map
	vdrSet := validators.NewSet()
	for nodeID, weight := range m.validators {
		vdrSet.Add(nodeID, nil, weight)
	}
	return vdrSet, nil
}