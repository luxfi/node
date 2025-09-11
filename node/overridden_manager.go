// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"fmt"

	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

var _ validators.Manager = (*overriddenManager)(nil)

// newOverriddenManager returns a Manager that overrides of all calls to the
// underlying Manager to only operate on the validators in [netID].
func newOverriddenManager(netID ids.ID, manager validators.Manager) *overriddenManager {
	return &overriddenManager{
		netID: netID,
		manager:  manager,
	}
}

// overriddenManager is a wrapper around a Manager that overrides of all calls
// to the underlying Manager to only operate on the validators in [netID].
// netID here is typically the primary network ID, as it has the superset of
// all net validators.
type overriddenManager struct {
	manager  validators.Manager
	netID ids.ID
}

func (o *overriddenManager) AddStaker(_ ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
	return o.manager.AddStaker(o.netID, nodeID, pk, txID, weight)
}

func (o *overriddenManager) AddWeight(_ ids.ID, nodeID ids.NodeID, weight uint64) error {
	return o.manager.AddWeight(o.netID, nodeID, weight)
}

func (o *overriddenManager) GetWeight(_ ids.ID, nodeID ids.NodeID) uint64 {
	return o.manager.GetWeight(o.netID, nodeID)
}

func (o *overriddenManager) GetValidator(_ ids.ID, nodeID ids.NodeID) (*validators.GetValidatorOutput, bool) {
	return o.manager.GetValidator(o.netID, nodeID)
}

func (o *overriddenManager) GetLight(_ ids.ID, nodeID ids.NodeID) uint64 {
	vdr, exists := o.manager.GetValidator(o.netID, nodeID)
	if !exists {
		return 0
	}
	return vdr.Weight
}

func (o *overriddenManager) SubsetWeight(_ ids.ID, nodeIDs set.Set[ids.NodeID]) (uint64, error) {
	return o.manager.SubsetWeight(o.netID, nodeIDs)
}

func (o *overriddenManager) RemoveWeight(_ ids.ID, nodeID ids.NodeID, weight uint64) error {
	return o.manager.RemoveWeight(o.netID, nodeID, weight)
}

func (o *overriddenManager) Count(ids.ID) int {
	return len(o.manager.GetValidatorIDs(o.netID))
}

func (o *overriddenManager) TotalWeight(ids.ID) (uint64, error) {
	return o.manager.TotalWeight(o.netID)
}

func (o *overriddenManager) TotalLight(ids.ID) (uint64, error) {
	return o.manager.TotalLight(o.netID)
}

func (o *overriddenManager) Sample(_ ids.ID, size int) ([]ids.NodeID, error) {
	return o.manager.Sample(o.netID, size)
}

func (o *overriddenManager) GetMap(ids.ID) map[ids.NodeID]*validators.GetValidatorOutput {
	return o.manager.GetMap(o.netID)
}

func (o *overriddenManager) RegisterCallbackListener(listener validators.ManagerCallbackListener) {
	o.manager.RegisterCallbackListener(listener)
}

func (o *overriddenManager) RegisterSetCallbackListener(_ ids.ID, listener validators.SetCallbackListener) {
	o.manager.RegisterSetCallbackListener(o.netID, listener)
}

func (o *overriddenManager) String() string {
	return fmt.Sprintf("Overridden Validator Manager (NetID = %s): %s", o.netID, o.manager)
}

func (o *overriddenManager) GetValidatorIDs(ids.ID) []ids.NodeID {
	return o.manager.GetValidatorIDs(o.netID)
}

func (o *overriddenManager) GetValidators(ids.ID) (validators.Set, error) {
	return o.manager.GetValidators(o.netID)
}

func (o *overriddenManager) NumSubnets() int {
	return o.manager.NumSubnets()
}

func (o *overriddenManager) NumValidators(netID ids.ID) int {
	return o.manager.NumValidators(netID)
}
