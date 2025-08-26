// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

// validatorsWrapper wraps validators.Manager to match the expected interface
type validatorsWrapper struct {
	manager   validators.Manager
	callbacks []validators.SetCallbackListener
}

// NewValidatorsWrapper creates a new validators wrapper
func NewValidatorsWrapper(manager validators.Manager) *validatorsWrapper {
	return &validatorsWrapper{
		manager:   manager,
		callbacks: make([]validators.SetCallbackListener, 0),
	}
}

// GetWeight returns the weight of a validator
func (v *validatorsWrapper) GetWeight(netID ids.ID, nodeID ids.NodeID) uint64 {
	return v.manager.GetWeight(netID, nodeID)
}

// GetValidator returns validator info
func (v *validatorsWrapper) GetValidator(netID ids.ID, nodeID ids.NodeID) (*validators.Validator, bool) {
	return v.manager.GetValidator(netID, nodeID)
}

// GetValidatorIDs returns all validator IDs for a subnet
func (v *validatorsWrapper) GetValidatorIDs(netID ids.ID) []ids.NodeID {
	return v.manager.GetValidatorIDs(netID)
}

// TotalWeight returns the total weight of all validators
func (v *validatorsWrapper) TotalWeight(netID ids.ID) (uint64, error) {
	return v.manager.TotalWeight(netID)
}

// RegisterSetCallbackListener registers a callback listener
func (v *validatorsWrapper) RegisterSetCallbackListener(listener validators.SetCallbackListener) {
	v.callbacks = append(v.callbacks, listener)
}

// SetCallbackListener for validator changes
type SetCallbackListener interface {
	OnValidatorAdded(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64)
	OnValidatorRemoved(nodeID ids.NodeID, weight uint64)
	OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64)
}
