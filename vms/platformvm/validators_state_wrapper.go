// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/validators"
	"github.com/luxfi/node/v2/vms/platformvm/state"
	"github.com/luxfi/node/v2/vms/platformvm/block"
	"github.com/luxfi/node/v2/vms/platformvm/status"
	"github.com/luxfi/node/v2/vms/platformvm/txs"
	pvalidators "github.com/luxfi/node/v2/vms/platformvm/validators"
)

// stateWrapper wraps state.State to implement validators.State
type stateWrapper struct {
	state.State
}

// NewStateWrapper creates a new state wrapper that implements validators.State
func NewStateWrapper(s state.State) pvalidators.State {
	return &stateWrapper{State: s}
}

// GetSubnetID implements validators.State
// This returns the subnet ID for a given chain ID
func (s *stateWrapper) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	// For now, return the primary network ID
	// In a real implementation, this would look up the chain's subnet
	return ids.Empty, nil
}

// GetMinimumHeight implements validators.State
func (s *stateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// Return 0 for now
	return 0, nil
}

// GetCurrentHeight implements validators.State
func (s *stateWrapper) GetCurrentHeight(ctx context.Context) (uint64, error) {
	// Get the last accepted block and return its height
	lastAcceptedID := s.State.GetLastAccepted()
	blk, err := s.State.GetStatelessBlock(lastAcceptedID)
	if err != nil {
		return 0, err
	}
	return blk.Height(), nil
}

// GetValidatorSet implements validators.State
func (s *stateWrapper) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// For now, return an empty validator set
	// In a real implementation, this would fetch validators at the given height
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

// ApplyValidatorWeightDiffs implements both validators.State and the local State interface
func (s *stateWrapper) ApplyValidatorWeightDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	subnetID ids.ID,
) error {
	// Delegate to the underlying state
	return s.State.ApplyValidatorWeightDiffs(ctx, validators, startHeight, endHeight, subnetID)
}

// ApplyValidatorPublicKeyDiffs implements both validators.State and the local State interface
func (s *stateWrapper) ApplyValidatorPublicKeyDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	subnetID ids.ID,
) error {
	// Delegate to the underlying state
	return s.State.ApplyValidatorPublicKeyDiffs(ctx, validators, startHeight, endHeight, subnetID)
}

// GetCurrentValidatorSet implements validators.State
func (s *stateWrapper) GetCurrentValidatorSet(
	ctx context.Context,
	subnetID ids.ID,
) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	// For now, return empty set
	return make(map[ids.ID]*validators.GetCurrentValidatorOutput), 0, nil
}

// GetTx implements the local State interface
func (s *stateWrapper) GetTx(txID ids.ID) (*txs.Tx, status.Status, error) {
	return s.State.GetTx(txID)
}

// GetLastAccepted implements the local State interface
func (s *stateWrapper) GetLastAccepted() ids.ID {
	return s.State.GetLastAccepted()
}

// GetStatelessBlock implements the local State interface
func (s *stateWrapper) GetStatelessBlock(blockID ids.ID) (block.Block, error) {
	return s.State.GetStatelessBlock(blockID)
}