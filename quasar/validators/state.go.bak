// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"errors"
	"fmt"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

// State manages the validator set state
type State interface {
	// GetValidatorSet returns the validator set for a subnet at a given height
	GetValidatorSet(
		ctx interface{},
		height uint64,
		subnetID ids.ID,
	) (map[ids.NodeID]*GetValidatorOutput, error)

	// GetValidator returns a validator's info
	GetValidator(
		ctx interface{},
		subnetID ids.ID,
		nodeID ids.NodeID,
	) (*GetValidatorOutput, error)
}

// GetValidatorOutput is the output of GetValidator
type GetValidatorOutput struct {
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	Weight    uint64
}

// Set manages a set of validators
type Set interface {
	// Add adds a validator
	Add(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error

	// AddWeight adds weight to a validator
	AddWeight(nodeID ids.NodeID, weight uint64) error

	// Get returns a validator's weight
	Get(nodeID ids.NodeID) (uint64, bool)

	// GetWeight returns a validator's weight
	GetWeight(nodeID ids.NodeID) uint64

	// SubsetWeight returns the total weight of a subset
	SubsetWeight(subset []ids.NodeID) uint64

	// Remove removes a validator
	Remove(nodeID ids.NodeID) error

	// RemoveWeight removes weight from a validator
	RemoveWeight(nodeID ids.NodeID, weight uint64) error

	// Contains returns true if the validator is in the set
	Contains(nodeID ids.NodeID) bool

	// Len returns the number of validators
	Len() int

	// List returns all validator IDs
	List() []ids.NodeID

	// Weight returns the total weight
	Weight() uint64

	// Sample returns a sample of validators
	Sample(size int) ([]ids.NodeID, error)

	// String returns a string representation
	String() string
}

// NewSet creates a new validator set
func NewSet() Set {
	return &set{
		vdrs:   make(map[ids.NodeID]*validator),
		weight: 0,
	}
}

// validator represents a validator
type validator struct {
	nodeID ids.NodeID
	pk     *bls.PublicKey
	txID   ids.ID
	weight uint64
}

// set is an implementation of Set
type set struct {
	vdrs   map[ids.NodeID]*validator
	weight uint64
}

// Add adds a validator
func (s *set) Add(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
	if weight == 0 {
		return errors.New("validator weight must be positive")
	}

	if _, exists := s.vdrs[nodeID]; exists {
		return fmt.Errorf("validator %s already exists", nodeID)
	}

	s.vdrs[nodeID] = &validator{
		nodeID: nodeID,
		pk:     pk,
		txID:   txID,
		weight: weight,
	}
	s.weight += weight

	return nil
}

// AddWeight adds weight to a validator
func (s *set) AddWeight(nodeID ids.NodeID, weight uint64) error {
	vdr, exists := s.vdrs[nodeID]
	if !exists {
		return fmt.Errorf("validator %s not found", nodeID)
	}

	vdr.weight += weight
	s.weight += weight
	return nil
}

// Get returns a validator's weight
func (s *set) Get(nodeID ids.NodeID) (uint64, bool) {
	vdr, exists := s.vdrs[nodeID]
	if !exists {
		return 0, false
	}
	return vdr.weight, true
}

// GetWeight returns a validator's weight
func (s *set) GetWeight(nodeID ids.NodeID) uint64 {
	weight, _ := s.Get(nodeID)
	return weight
}

// SubsetWeight returns the total weight of a subset
func (s *set) SubsetWeight(subset []ids.NodeID) uint64 {
	var weight uint64
	for _, nodeID := range subset {
		weight += s.GetWeight(nodeID)
	}
	return weight
}

// Remove removes a validator
func (s *set) Remove(nodeID ids.NodeID) error {
	vdr, exists := s.vdrs[nodeID]
	if !exists {
		return fmt.Errorf("validator %s not found", nodeID)
	}

	s.weight -= vdr.weight
	delete(s.vdrs, nodeID)
	return nil
}

// RemoveWeight removes weight from a validator
func (s *set) RemoveWeight(nodeID ids.NodeID, weight uint64) error {
	vdr, exists := s.vdrs[nodeID]
	if !exists {
		return fmt.Errorf("validator %s not found", nodeID)
	}

	if vdr.weight < weight {
		return fmt.Errorf("validator %s weight %d < %d", nodeID, vdr.weight, weight)
	}

	vdr.weight -= weight
	s.weight -= weight

	if vdr.weight == 0 {
		delete(s.vdrs, nodeID)
	}

	return nil
}

// Contains returns true if the validator is in the set
func (s *set) Contains(nodeID ids.NodeID) bool {
	_, exists := s.vdrs[nodeID]
	return exists
}

// Len returns the number of validators
func (s *set) Len() int {
	return len(s.vdrs)
}

// List returns all validator IDs
func (s *set) List() []ids.NodeID {
	list := make([]ids.NodeID, 0, len(s.vdrs))
	for nodeID := range s.vdrs {
		list = append(list, nodeID)
	}
	return list
}

// Weight returns the total weight
func (s *set) Weight() uint64 {
	return s.weight
}

// Sample returns a sample of validators
func (s *set) Sample(size int) ([]ids.NodeID, error) {
	if size > len(s.vdrs) {
		return nil, fmt.Errorf("sample size %d > set size %d", size, len(s.vdrs))
	}

	// Simple sampling - in production use weighted sampling
	sample := make([]ids.NodeID, 0, size)
	for nodeID := range s.vdrs {
		sample = append(sample, nodeID)
		if len(sample) >= size {
			break
		}
	}

	return sample, nil
}

// String returns a string representation
func (s *set) String() string {
	return fmt.Sprintf("ValidatorSet{count=%d, weight=%d}", len(s.vdrs), s.weight)
}