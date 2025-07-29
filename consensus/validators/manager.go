// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/version"
)

var (
	ErrValidatorNotFound = errors.New("validator not found")
	ErrInvalidWeight     = errors.New("invalid weight")
)

// Validator represents a network validator
type Validator struct {
	NodeID    ids.NodeID
	PublicKey []byte
	Weight    uint64
	TxID      ids.ID // Transaction ID that added this validator
}

// State allows getting a weighted validator set on a given subnet
// for a given P-chain height.
type State interface {
	// GetMinimumHeight returns the minimum height of the P-chain.
	GetMinimumHeight(ctx context.Context) (uint64, error)

	// GetCurrentHeight returns the current height of the P-chain.
	GetCurrentHeight(ctx context.Context) (uint64, error)

	// GetSubnetID returns the subnet ID for the given chain ID.
	GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error)

	// GetValidatorSet returns the validators of the given subnet at the
	// given P-chain height.
	// Returns [database.ErrNotFound] if the validator set doesn't exist.
	GetValidatorSet(
		ctx context.Context,
		height uint64,
		subnetID ids.ID,
	) (map[ids.NodeID]*GetValidatorOutput, error)
}

// GetValidatorOutput is a struct that contains the publicly relevant
// values of a validator of the Lux primary network or a Lux
// subnet.
type GetValidatorOutput struct {
	NodeID    ids.NodeID
	PublicKey []byte
	Weight    uint64
}

// Set is a set of validators with each validator having a weight.
type Set interface {
	// Add adds a validator to the set.
	Add(nodeID ids.NodeID, pk []byte, weight uint64) error

	// AddWeight adds weight to a validator in the set.
	AddWeight(nodeID ids.NodeID, weight uint64) error

	// RemoveWeight removes weight from a validator in the set.
	RemoveWeight(nodeID ids.NodeID, weight uint64) error

	// Remove removes a validator from the set.
	Remove(nodeID ids.NodeID) error

	// Contains returns true if the set contains the given nodeID.
	Contains(nodeID ids.NodeID) bool

	// Get returns the validator with the given nodeID.
	Get(nodeID ids.NodeID) (*Validator, bool)

	// Len returns the number of validators in the set.
	Len() int

	// List returns the validators in this set
	List() []*Validator

	// Weight returns the total weight of the validator set.
	Weight() uint64

	// Sample returns a random validator from the set, weighted by stake.
	Sample(seed uint64) (ids.NodeID, error)

	// String returns a string representation of the set.
	String() string
}

// Manager manages validator sets for different subnets.
type Manager interface {
	fmt.Stringer

	// Add a subnet's validator set to the manager.
	Add(subnetID ids.ID, validators Set) error

	// Remove a subnet's validator set from the manager.
	Remove(subnetID ids.ID) error

	// Contains returns true if the manager is tracking the validator set for the given subnet.
	Contains(subnetID ids.ID) bool

	// Get returns the validator set for the given subnet.
	Get(subnetID ids.ID) (Set, bool)

	// GetByWeight returns a new validator set for the given subnet that contains all
	// validators with at least the given weight.
	GetByWeight(subnetID ids.ID, minWeight uint64) (Set, bool)

	// GetWeight returns the weight of a specific validator for the given subnet.
	GetWeight(subnetID ids.ID, nodeID ids.NodeID) uint64

	// TotalWeight returns the total weight of all validators for the given subnet.
	TotalWeight(subnetID ids.ID) (uint64, error)

	// RecalculateStakes recalculates the stakes of all validators in the validator set.
	RecalculateStakes(subnetID ids.ID) error

	// Count returns the number of subnets that have validator sets.
	Count() int

	// GetValidator returns information about a validator
	GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*Validator, error)
}

// TestState is a test validator state
type TestState struct {
	GetCurrentHeightF func(ctx context.Context) (uint64, error)
	GetValidatorSetF  func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*GetValidatorOutput, error)
}

func (ts *TestState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	if ts.GetCurrentHeightF != nil {
		return ts.GetCurrentHeightF(ctx)
	}
	return 0, nil
}

func (ts *TestState) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*GetValidatorOutput, error) {
	if ts.GetValidatorSetF != nil {
		return ts.GetValidatorSetF(ctx, height, subnetID)
	}
	return nil, nil
}

// NewSet returns a new validator set
func NewSet() Set {
	return &set{
		validators: make(map[ids.NodeID]*Validator),
	}
}

type set struct {
	validators   map[ids.NodeID]*Validator
	totalWeight  uint64
}

func (s *set) Add(nodeID ids.NodeID, pk []byte, weight uint64) error {
	if weight == 0 {
		return ErrInvalidWeight
	}
	
	if _, exists := s.validators[nodeID]; exists {
		return fmt.Errorf("validator %s already exists", nodeID)
	}
	
	s.validators[nodeID] = &Validator{
		NodeID:    nodeID,
		PublicKey: pk,
		Weight:    weight,
	}
	s.totalWeight += weight
	return nil
}

func (s *set) AddWeight(nodeID ids.NodeID, weight uint64) error {
	validator, exists := s.validators[nodeID]
	if !exists {
		return ErrValidatorNotFound
	}
	
	validator.Weight += weight
	s.totalWeight += weight
	return nil
}

func (s *set) RemoveWeight(nodeID ids.NodeID, weight uint64) error {
	validator, exists := s.validators[nodeID]
	if !exists {
		return ErrValidatorNotFound
	}
	
	if validator.Weight < weight {
		return ErrInvalidWeight
	}
	
	validator.Weight -= weight
	s.totalWeight -= weight
	
	if validator.Weight == 0 {
		delete(s.validators, nodeID)
	}
	
	return nil
}

func (s *set) Remove(nodeID ids.NodeID) error {
	validator, exists := s.validators[nodeID]
	if !exists {
		return ErrValidatorNotFound
	}
	
	s.totalWeight -= validator.Weight
	delete(s.validators, nodeID)
	return nil
}

func (s *set) Contains(nodeID ids.NodeID) bool {
	_, exists := s.validators[nodeID]
	return exists
}

func (s *set) Get(nodeID ids.NodeID) (*Validator, bool) {
	validator, exists := s.validators[nodeID]
	return validator, exists
}

func (s *set) Len() int {
	return len(s.validators)
}

func (s *set) List() []*Validator {
	list := make([]*Validator, 0, len(s.validators))
	for _, v := range s.validators {
		list = append(list, v)
	}
	return list
}

func (s *set) Weight() uint64 {
	return s.totalWeight
}

func (s *set) Sample(seed uint64) (ids.NodeID, error) {
	if s.totalWeight == 0 {
		return ids.NodeID{}, errors.New("no validators")
	}
	
	// Simple sampling implementation
	target := seed % s.totalWeight
	cumulative := uint64(0)
	
	for _, v := range s.validators {
		cumulative += v.Weight
		if cumulative > target {
			return v.NodeID, nil
		}
	}
	
	return ids.NodeID{}, errors.New("sampling failed")
}

func (s *set) String() string {
	return fmt.Sprintf("ValidatorSet{size:%d, weight:%d}", len(s.validators), s.totalWeight)
}

// Connector handles validator connection events
type Connector interface {
	// Connected is called when a validator connects
	Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error

	// Disconnected is called when a validator disconnects
	Disconnected(ctx context.Context, nodeID ids.NodeID) error
}

// For compatibility with node expectations
var (
	// PrimaryNetworkID is the ID of the primary network
	PrimaryNetworkID = constants.PrimaryNetworkID
)

// NewManager returns a new manager
func NewManager() Manager {
	return &manager{
		subnetToValidators: make(map[ids.ID]Set),
	}
}

// manager implements Manager
type manager struct {
	mu                 sync.RWMutex
	subnetToValidators map[ids.ID]Set
}

func (m *manager) Add(subnetID ids.ID, validators Set) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if _, exists := m.subnetToValidators[subnetID]; exists {
		return fmt.Errorf("subnet %s already exists", subnetID)
	}
	m.subnetToValidators[subnetID] = validators
	return nil
}

func (m *manager) Remove(subnetID ids.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if _, exists := m.subnetToValidators[subnetID]; !exists {
		return fmt.Errorf("subnet %s not found", subnetID)
	}
	delete(m.subnetToValidators, subnetID)
	return nil
}

func (m *manager) Contains(subnetID ids.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	_, exists := m.subnetToValidators[subnetID]
	return exists
}

func (m *manager) Get(subnetID ids.ID) (Set, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	validators, exists := m.subnetToValidators[subnetID]
	return validators, exists
}

func (m *manager) GetByWeight(subnetID ids.ID, minWeight uint64) (Set, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	validators, exists := m.subnetToValidators[subnetID]
	if !exists {
		return nil, false
	}
	
	// Create a new set with validators above minimum weight
	filtered := NewSet()
	for _, v := range validators.List() {
		if v.Weight >= minWeight {
			filtered.Add(v.NodeID, v.PublicKey, v.Weight)
		}
	}
	return filtered, true
}

func (m *manager) GetWeight(subnetID ids.ID, nodeID ids.NodeID) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	validators, exists := m.subnetToValidators[subnetID]
	if !exists {
		return 0
	}
	
	if v, ok := validators.Get(nodeID); ok {
		return v.Weight
	}
	return 0
}

func (m *manager) TotalWeight(subnetID ids.ID) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	validators, exists := m.subnetToValidators[subnetID]
	if !exists {
		return 0, fmt.Errorf("subnet %s not found", subnetID)
	}
	
	return validators.Weight(), nil
}

func (m *manager) RecalculateStakes(subnetID ids.ID) error {
	// No-op for now
	return nil
}

func (m *manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return len(m.subnetToValidators)
}

func (m *manager) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*Validator, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	validators, exists := m.subnetToValidators[subnetID]
	if !exists {
		return nil, fmt.Errorf("subnet %s not found", subnetID)
	}
	
	v, ok := validators.Get(nodeID)
	if !ok {
		return nil, ErrValidatorNotFound
	}
	return v, nil
}

func (m *manager) String() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return fmt.Sprintf("ValidatorManager{subnetCount:%d}", len(m.subnetToValidators))
}