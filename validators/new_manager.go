// Copyright (C) 2019-2025, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"sync"

	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/ids"
)

// Manager interface for validator management
type Manager interface {
	validators.Manager
	// Additional methods needed by node (beyond consensus.validators.Manager)
	Count(subnetID ids.ID) int
	Sample(subnetID ids.ID, size int) ([]ids.NodeID, error)
	GetValidatorIDs(subnetID ids.ID) []ids.NodeID
	SubsetWeight(subnetID ids.ID, nodeIDs set.Set[ids.NodeID]) (uint64, error)
	GetMap(subnetID ids.ID) map[ids.NodeID]*validators.GetValidatorOutput
	RegisterCallbackListener(listener validators.ManagerCallbackListener)
	RegisterSetCallbackListener(subnetID ids.ID, listener validators.SetCallbackListener)
	NumValidators(subnetID ids.ID) int
}

// NewManager returns a new validator manager
func NewManager() Manager {
	return &manager{
		validators: make(map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput),
		mu:         &sync.RWMutex{},
	}
}

type manager struct {
	validators map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput
	mu         *sync.RWMutex
}

func (m *manager) AddStaker(subnetID ids.ID, nodeID ids.NodeID, publicKey []byte, txID ids.ID, light uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.validators[subnetID] == nil {
		m.validators[subnetID] = make(map[ids.NodeID]*validators.GetValidatorOutput)
	}

	m.validators[subnetID][nodeID] = &validators.GetValidatorOutput{
		NodeID:    nodeID,
		PublicKey: publicKey,
		Light:     light,
		Weight:    light,
		TxID:      txID,
	}
	return nil
}

func (m *manager) AddWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.validators[subnetID] == nil {
		m.validators[subnetID] = make(map[ids.NodeID]*validators.GetValidatorOutput)
	}

	if vdr, ok := m.validators[subnetID][nodeID]; ok {
		vdr.Weight += weight
	} else {
		m.validators[subnetID][nodeID] = &validators.GetValidatorOutput{
			NodeID: nodeID,
			Weight: weight,
		}
	}
	return nil
}

func (m *manager) RemoveWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if subnet, ok := m.validators[subnetID]; ok {
		if vdr, ok := subnet[nodeID]; ok {
			if vdr.Weight > weight {
				vdr.Weight -= weight
			} else {
				delete(subnet, nodeID)
			}
		}
	}
	return nil
}

func (m *manager) GetWeight(subnetID ids.ID, nodeID ids.NodeID) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if subnet, ok := m.validators[subnetID]; ok {
		if vdr, ok := subnet[nodeID]; ok {
			return vdr.Weight
		}
	}
	return 0
}

func (m *manager) GetLight(subnetID ids.ID, nodeID ids.NodeID) uint64 {
	return m.GetWeight(subnetID, nodeID)
}

func (m *manager) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*validators.GetValidatorOutput, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if subnet, ok := m.validators[subnetID]; ok {
		vdr, ok := subnet[nodeID]
		return vdr, ok
	}
	return nil, false
}

func (m *manager) SubsetWeight(subnetID ids.ID, nodeIDs set.Set[ids.NodeID]) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var totalWeight uint64
	if subnet, ok := m.validators[subnetID]; ok {
		for nodeID := range nodeIDs {
			if vdr, ok := subnet[nodeID]; ok {
				totalWeight += vdr.Weight
			}
		}
	}
	return totalWeight, nil
}

func (m *manager) TotalWeight(subnetID ids.ID) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var totalWeight uint64
	if subnet, ok := m.validators[subnetID]; ok {
		for _, vdr := range subnet {
			totalWeight += vdr.Weight
		}
	}
	return totalWeight, nil
}

func (m *manager) TotalLight(subnetID ids.ID) (uint64, error) {
	return m.TotalWeight(subnetID)
}

func (m *manager) Count(subnetID ids.ID) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if subnet, ok := m.validators[subnetID]; ok {
		return len(subnet)
	}
	return 0
}

func (m *manager) Sample(subnetID ids.ID, size int) ([]ids.NodeID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodeIDs := make([]ids.NodeID, 0, size)
	if subnet, ok := m.validators[subnetID]; ok {
		for nodeID := range subnet {
			if len(nodeIDs) >= size {
				break
			}
			nodeIDs = append(nodeIDs, nodeID)
		}
	}
	return nodeIDs, nil
}

func (m *manager) GetMap(subnetID ids.ID) map[ids.NodeID]*validators.GetValidatorOutput {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if subnet, ok := m.validators[subnetID]; ok {
		// Return a copy
		result := make(map[ids.NodeID]*validators.GetValidatorOutput, len(subnet))
		for k, v := range subnet {
			result[k] = v
		}
		return result
	}
	return make(map[ids.NodeID]*validators.GetValidatorOutput)
}

func (m *manager) GetValidatorIDs(subnetID ids.ID) []ids.NodeID {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if subnet, ok := m.validators[subnetID]; ok {
		nodeIDs := make([]ids.NodeID, 0, len(subnet))
		for nodeID := range subnet {
			nodeIDs = append(nodeIDs, nodeID)
		}
		return nodeIDs
	}
	return nil
}

func (m *manager) RegisterCallbackListener(listener validators.ManagerCallbackListener) {
	// No-op for now
}

func (m *manager) RegisterSetCallbackListener(subnetID ids.ID, listener validators.SetCallbackListener) {
	// No-op for now
}

func (m *manager) NumSubnets() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.validators)
}

func (m *manager) NumValidators(subnetID ids.ID) int {
	return m.Count(subnetID)
}

func (m *manager) GetValidators(subnetID ids.ID) (validators.Set, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return &validatorSet{
		vdrs: m.GetMap(subnetID),
	}, nil
}

func (m *manager) String() string {
	return "validators.Manager"
}

// validatorSet implements validators.Set
type validatorSet struct {
	vdrs map[ids.NodeID]*validators.GetValidatorOutput
}

// validatorImpl implements validators.Validator
type validatorImpl struct {
	nodeID ids.NodeID
	weight uint64
}

func (v *validatorImpl) ID() ids.NodeID {
	return v.nodeID
}

func (v *validatorImpl) Light() uint64 {
	return v.weight
}

func (v *validatorSet) Has(nodeID ids.NodeID) bool {
	_, ok := v.vdrs[nodeID]
	return ok
}

func (v *validatorSet) Len() int {
	return len(v.vdrs)
}

func (v *validatorSet) List() []validators.Validator {
	list := make([]validators.Validator, 0, len(v.vdrs))
	for _, vdr := range v.vdrs {
		list = append(list, &validatorImpl{
			nodeID: vdr.NodeID,
			weight: vdr.Weight,
		})
	}
	return list
}

func (v *validatorSet) Light() uint64 {
	var total uint64
	for _, vdr := range v.vdrs {
		total += vdr.Weight
	}
	return total
}

func (v *validatorSet) Sample(size int) ([]ids.NodeID, error) {
	nodeIDs := make([]ids.NodeID, 0, size)
	for nodeID := range v.vdrs {
		if len(nodeIDs) >= size {
			break
		}
		nodeIDs = append(nodeIDs, nodeID)
	}
	return nodeIDs, nil
}