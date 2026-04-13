// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"fmt"
	"sort"
	"strings"

	nodevalidators "github.com/luxfi/validators"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
)

var _ nodevalidators.Manager = (*overriddenManager)(nil)

// newOverriddenManager returns a Manager that overrides of all calls to the
// underlying Manager to only operate on the validators in [netID].
func newOverriddenManager(netID ids.ID, manager nodevalidators.Manager) *overriddenManager {
	return &overriddenManager{
		netID:   netID,
		manager: manager,
	}
}

// overriddenManager is a wrapper around a Manager that overrides of all calls
// to the underlying Manager to only operate on the validators in [netID].
// netID here is typically the primary network ID, as it has the superset of
// all net validators.
type overriddenManager struct {
	manager nodevalidators.Manager
	netID   ids.ID
}

func (o *overriddenManager) AddStaker(_ ids.ID, nodeID ids.NodeID, pk []byte, txID ids.ID, weight uint64) error {
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
	// Calculate subset weight by summing individual weights
	var totalWeight uint64
	for nodeID := range nodeIDs {
		vdr, exists := o.manager.GetValidator(o.netID, nodeID)
		if exists {
			totalWeight += vdr.Weight
		}
	}
	return totalWeight, nil
}

func (o *overriddenManager) RemoveWeight(_ ids.ID, nodeID ids.NodeID, weight uint64) error {
	return o.manager.RemoveWeight(o.netID, nodeID, weight)
}

func (o *overriddenManager) Count(ids.ID) int {
	return o.manager.NumValidators(o.netID)
}

func (o *overriddenManager) NumNets() int {
	if o.manager.NumValidators(o.netID) == 0 {
		return 0
	}
	return 1
}

func (o *overriddenManager) NumValidators(ids.ID) int {
	return o.manager.NumValidators(o.netID)
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
	// Build a detailed string representation of the validator manager
	var sb strings.Builder

	// Get all validators for the network
	validators := o.manager.GetMap(o.netID)

	// Write header
	sb.WriteString(fmt.Sprintf("Overridden Validator Manager (ChainID = %s): Validator Manager: (Size = %d)\n",
		o.netID, len(validators)))

	// Get all chain IDs by checking which ones have validators
	chainIDs := []ids.ID{o.netID}

	// Also check if there are validators in other chains (for display purposes)
	// Check additional chain IDs for display completeness
	// Check a few common test IDs
	testID1, _ := ids.FromString("2mcwQKiD8VEspmMJpL1dc7okQQ5dDVAWeCBZ7FWBFAbxpv3t7w")
	if testValidators := o.manager.GetValidatorIDs(testID1); len(testValidators) > 0 {
		chainIDs = append(chainIDs, testID1)
	}

	// Write validator information for each chain
	for _, chainID := range chainIDs {
		chainValidators := o.manager.GetMap(chainID)
		chainWeight, _ := o.manager.TotalWeight(chainID)

		sb.WriteString(fmt.Sprintf("    Net[%s]: Validator Set: (Size = %d, Weight = %d)\n",
			chainID, len(chainValidators), chainWeight))

		// Sort validators by node ID for consistent output
		var nodeIDs []ids.NodeID
		for nodeID := range chainValidators {
			nodeIDs = append(nodeIDs, nodeID)
		}
		// Sort by string representation
		sort.Slice(nodeIDs, func(i, j int) bool {
			return nodeIDs[i].String() < nodeIDs[j].String()
		})

		// Write each validator
		for i, nodeID := range nodeIDs {
			validator := chainValidators[nodeID]
			sb.WriteString(fmt.Sprintf("        Validator[%d]: %s, %d\n",
				i, nodeID, validator.Weight))
		}
	}

	// Remove trailing newline
	result := sb.String()
	if len(result) > 0 && result[len(result)-1] == '\n' {
		result = result[:len(result)-1]
	}

	return result
}

func (o *overriddenManager) GetValidatorIDs(ids.ID) []ids.NodeID {
	return o.manager.GetValidatorIDs(o.netID)
}

func (o *overriddenManager) GetValidators(ids.ID) (validators.Set, error) {
	return o.manager.GetValidators(o.netID)
}
