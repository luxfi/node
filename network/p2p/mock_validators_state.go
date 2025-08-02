// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/validators"
)

// mockValidatorsState is a test implementation of validators.State
type mockValidatorsState struct {
	GetMinimumHeightF             func(context.Context) (uint64, error)
	GetCurrentHeightF             func(context.Context) (uint64, error)
	GetSubnetIDF                  func(context.Context, ids.ID) (ids.ID, error)
	GetValidatorSetF              func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error)
	ApplyValidatorWeightDiffsF    func(context.Context, map[ids.NodeID]*validators.GetValidatorOutput, uint64, uint64, ids.ID) error
	ApplyValidatorPublicKeyDiffsF func(context.Context, map[ids.NodeID]*validators.GetValidatorOutput, uint64, uint64, ids.ID) error
	GetCurrentValidatorSetF       func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error)
}

func (m *mockValidatorsState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	if m.GetMinimumHeightF != nil {
		return m.GetMinimumHeightF(ctx)
	}
	return 0, nil
}

func (m *mockValidatorsState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	if m.GetCurrentHeightF != nil {
		return m.GetCurrentHeightF(ctx)
	}
	return 0, nil
}

func (m *mockValidatorsState) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	if m.GetSubnetIDF != nil {
		return m.GetSubnetIDF(ctx, chainID)
	}
	return ids.Empty, nil
}

func (m *mockValidatorsState) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	if m.GetValidatorSetF != nil {
		return m.GetValidatorSetF(ctx, height, subnetID)
	}
	return nil, nil
}

func (m *mockValidatorsState) ApplyValidatorWeightDiffs(ctx context.Context, validatorSet map[ids.NodeID]*validators.GetValidatorOutput, startHeight uint64, endHeight uint64, subnetID ids.ID) error {
	if m.ApplyValidatorWeightDiffsF != nil {
		return m.ApplyValidatorWeightDiffsF(ctx, validatorSet, startHeight, endHeight, subnetID)
	}
	return nil
}

func (m *mockValidatorsState) ApplyValidatorPublicKeyDiffs(ctx context.Context, validatorSet map[ids.NodeID]*validators.GetValidatorOutput, startHeight uint64, endHeight uint64, subnetID ids.ID) error {
	if m.ApplyValidatorPublicKeyDiffsF != nil {
		return m.ApplyValidatorPublicKeyDiffsF(ctx, validatorSet, startHeight, endHeight, subnetID)
	}
	return nil
}

func (m *mockValidatorsState) GetCurrentValidatorSet(ctx context.Context, subnetID ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	if m.GetCurrentValidatorSetF != nil {
		return m.GetCurrentValidatorSetF(ctx, subnetID)
	}
	return nil, 0, nil
}