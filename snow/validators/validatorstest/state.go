// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validatorstest

import (
	"context"
	"errors"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/validators"
)

// State is a test implementation of validators.State
type State struct {
	GetCurrentHeightF       func(context.Context) (uint64, error)
	GetSubnetIDF            func(context.Context, ids.ID) (ids.ID, error)
	GetValidatorSetF        func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error)
	GetCurrentValidatorSetF func(context.Context, ids.ID) (map[ids.NodeID]interface{}, uint64, error)
}

func (s *State) GetCurrentHeight(ctx context.Context) (uint64, error) {
	if s.GetCurrentHeightF != nil {
		return s.GetCurrentHeightF(ctx)
	}
	return 0, errors.New("not implemented")
}

func (s *State) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	if s.GetSubnetIDF != nil {
		return s.GetSubnetIDF(ctx, chainID)
	}
	return ids.Empty, errors.New("not implemented")
}

func (s *State) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	if s.GetValidatorSetF != nil {
		return s.GetValidatorSetF(ctx, height, subnetID)
	}
	return nil, errors.New("not implemented")
}

func (s *State) GetCurrentValidatorSet(ctx context.Context, subnetID ids.ID) (map[ids.NodeID]interface{}, uint64, error) {
	if s.GetCurrentValidatorSetF != nil {
		return nil, 0, errors.New("not implemented") // Simplified for now
	}
	return nil, 0, errors.New("not implemented")
}