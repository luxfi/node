// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	
	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/validators"
)

// mockValidatorsState is a simple mock implementation of validators.State for testing
type mockValidatorsState struct {
	GetCurrentHeightF func(context.Context) (uint64, error)
	GetValidatorSetF  func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error)
}

func (m *mockValidatorsState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockValidatorsState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	if m.GetCurrentHeightF != nil {
		return m.GetCurrentHeightF(ctx)
	}
	return 0, nil
}

func (m *mockValidatorsState) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (m *mockValidatorsState) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	if m.GetValidatorSetF != nil {
		return m.GetValidatorSetF(ctx, height, subnetID)
	}
	return nil, nil
}

func (m *mockValidatorsState) ApplyValidatorWeightDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	subnetID ids.ID,
) error {
	return nil
}

func (m *mockValidatorsState) ApplyValidatorPublicKeyDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	subnetID ids.ID,
) error {
	return nil
}

func (m *mockValidatorsState) GetCurrentValidatorSet(ctx context.Context, subnetID ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	return nil, 0, nil
}