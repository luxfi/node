// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/crypto/bls"
)

var (
	// ErrMissingValidators indicates no validators are present
	ErrMissingValidators = errors.New("missing validators")
)

// State allows the lookup of validator sets on specified subnets at the
// requested P-chain height.
type State interface {
	// GetValidatorSet returns the validator set of [subnetID] at [pChainHeight].
	// If the P-chain height is [math.MaxUint64], then the current validator set
	// is returned.
	GetValidatorSet(
		pChainHeight uint64,
		subnetID ids.ID,
	) (map[ids.NodeID]*Validator, error)
}

// Validator contains the base set of fields describing a validator
type Validator struct {
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	TxID      ids.ID
	Weight    uint64
}

// Less returns true if this validator's ID is less than the other's
func (v *Validator) Less(o *Validator) bool {
	return v.NodeID.Compare(o.NodeID) < 0
}

// Manager holds the validator set of each subnet
type Manager interface {
	fmt.Stringer

	// Add a new validator to the subnet.
	Add(nodeID ids.NodeID, publicKey *bls.PublicKey, txID ids.ID, weight uint64) error

	// AddWeight to an existing validator.
	AddWeight(nodeID ids.NodeID, weight uint64) error

	// GetWeight retrieves the validator weight from the subnet.
	GetWeight(nodeID ids.NodeID) uint64

	// GetValidator returns the validator tied to the specified ID.
	GetValidator(nodeID ids.NodeID) (*Validator, bool)

	// GetValidatorIDs returns the validator IDs in this subnet.
	GetValidatorIDs() []ids.NodeID

	// SubsetWeight returns the sum of the weights of the validators in the subset.
	SubsetWeight(nodeIDs []ids.NodeID) (uint64, error)

	// RemoveWeight from a validator.
	RemoveWeight(nodeID ids.NodeID, weight uint64) error

	// Remove a validator from the subnet.
	Remove(nodeID ids.NodeID) error

	// TotalWeight returns the cumulative weight of all validators.
	TotalWeight() uint64

	// Count returns the number of validators currently in the subnet.
	Count() int

	// Sample returns a collection of validatorIDs, potentially with duplicates.
	// If sampling the requested size isn't possible, an error will be returned.
	Sample(size int) ([]ids.NodeID, error)

	// Map of the validators in this subnet
	Map() map[ids.NodeID]*Validator
}

// SetState represents a mutable validator set
type SetState interface {
	Manager

	// GetCurrentHeight returns the current height
	GetCurrentHeight() (uint64, error)

	// GetValidatorSet returns the validator set of the given subnet at the
	// given height
	GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.NodeID]*Validator, error)
}

// TestState is a test validator state
type TestState interface {
	State

	// SetValidator sets the validator for the given subnet and height
	SetValidator(pChainHeight uint64, subnetID ids.ID, nodeID ids.NodeID, weight uint64) error
}

// NewManager returns a new, empty manager
func NewManager() Manager {
	return &manager{
		validators: make(map[ids.NodeID]*Validator),
	}
}

type manager struct {
	validators  map[ids.NodeID]*Validator
	totalWeight uint64
}

func (m *manager) String() string {
	return fmt.Sprintf("ValidatorManager[count=%d, totalWeight=%d]", len(m.validators), m.totalWeight)
}

func (m *manager) Add(nodeID ids.NodeID, publicKey *bls.PublicKey, txID ids.ID, weight uint64) error {
	if weight == 0 {
		return fmt.Errorf("validator weight must be non-zero")
	}
	if _, exists := m.validators[nodeID]; exists {
		return fmt.Errorf("validator %s already exists", nodeID)
	}

	m.validators[nodeID] = &Validator{
		NodeID:    nodeID,
		PublicKey: publicKey,
		TxID:      txID,
		Weight:    weight,
	}
	m.totalWeight += weight
	return nil
}

func (m *manager) AddWeight(nodeID ids.NodeID, weight uint64) error {
	v, exists := m.validators[nodeID]
	if !exists {
		return fmt.Errorf("validator %s does not exist", nodeID)
	}
	v.Weight += weight
	m.totalWeight += weight
	return nil
}

func (m *manager) GetWeight(nodeID ids.NodeID) uint64 {
	v, exists := m.validators[nodeID]
	if !exists {
		return 0
	}
	return v.Weight
}

func (m *manager) GetValidator(nodeID ids.NodeID) (*Validator, bool) {
	v, exists := m.validators[nodeID]
	return v, exists
}

func (m *manager) GetValidatorIDs() []ids.NodeID {
	nodeIDs := make([]ids.NodeID, 0, len(m.validators))
	for nodeID := range m.validators {
		nodeIDs = append(nodeIDs, nodeID)
	}
	return nodeIDs
}

func (m *manager) SubsetWeight(nodeIDs []ids.NodeID) (uint64, error) {
	var weight uint64
	for _, nodeID := range nodeIDs {
		v, exists := m.validators[nodeID]
		if !exists {
			return 0, fmt.Errorf("validator %s does not exist", nodeID)
		}
		weight += v.Weight
	}
	return weight, nil
}

func (m *manager) RemoveWeight(nodeID ids.NodeID, weight uint64) error {
	v, exists := m.validators[nodeID]
	if !exists {
		return fmt.Errorf("validator %s does not exist", nodeID)
	}
	if v.Weight < weight {
		return fmt.Errorf("validator %s weight %d is less than %d", nodeID, v.Weight, weight)
	}
	v.Weight -= weight
	m.totalWeight -= weight
	if v.Weight == 0 {
		delete(m.validators, nodeID)
	}
	return nil
}

func (m *manager) Remove(nodeID ids.NodeID) error {
	v, exists := m.validators[nodeID]
	if !exists {
		return fmt.Errorf("validator %s does not exist", nodeID)
	}
	m.totalWeight -= v.Weight
	delete(m.validators, nodeID)
	return nil
}

func (m *manager) TotalWeight() uint64 {
	return m.totalWeight
}

func (m *manager) Count() int {
	return len(m.validators)
}

func (m *manager) Sample(size int) ([]ids.NodeID, error) {
	if size > len(m.validators) {
		return nil, fmt.Errorf("cannot sample %d validators from set of %d", size, len(m.validators))
	}

	// Simple implementation - just return the first 'size' validators
	result := make([]ids.NodeID, 0, size)
	for nodeID := range m.validators {
		if len(result) >= size {
			break
		}
		result = append(result, nodeID)
	}
	return result, nil
}

func (m *manager) Map() map[ids.NodeID]*Validator {
	// Return a copy to prevent external modification
	result := make(map[ids.NodeID]*Validator, len(m.validators))
	for k, v := range m.validators {
		result[k] = v
	}
	return result
}