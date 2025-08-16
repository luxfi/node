// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/validators/validatorsmock"
	"github.com/luxfi/ids"
)

// validatorStateAdapter adapts validators.State to consensus.ValidatorState
type validatorStateAdapter struct {
	state *validatorsmock.State
}

func (a *validatorStateAdapter) GetCurrentHeight() (uint64, error) {
	return a.state.GetCurrentHeight(context.Background())
}

func (a *validatorStateAdapter) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return a.state.GetMinimumHeight(ctx)
}

func (a *validatorStateAdapter) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	valSet, err := a.state.GetValidatorSet(context.Background(), height, subnetID)
	if err != nil {
		return nil, err
	}
	result := make(map[ids.NodeID]uint64, len(valSet))
	for nodeID, val := range valSet {
		result[nodeID] = val.Weight
	}
	return result, nil
}

func (a *validatorStateAdapter) GetSubnetID(chainID ids.ID) (ids.ID, error) {
	return a.state.GetSubnetID(context.Background(), chainID)
}

var errMissing = errors.New("missing")

func TestSameSubnet(t *testing.T) {
	subnetID0 := ids.GenerateTestID()
	subnetID1 := ids.GenerateTestID()
	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	tests := []struct {
		name    string
		ctxF    func(*testing.T) context.Context
		chainID ids.ID
		result  error
	}{
		{
			name: "same chain",
			ctxF: func(t *testing.T) context.Context {
				state := validatorsmock.NewState(t)
				adapter := &validatorStateAdapter{state: state}
				ctx := context.Background()
				ids := consensus.IDs{
					SubnetID: subnetID0,
					ChainID:  chainID0,
				}
				ctx = consensus.WithIDs(ctx, ids)
				ctx = consensus.WithValidatorState(ctx, adapter)
				return ctx
			},
			chainID: chainID0,
			result:  ErrSameChainID,
		},
		{
			name: "unknown chain",
			ctxF: func(t *testing.T) context.Context {
				state := validatorsmock.NewState(t)
				state.GetSubnetIDF = func(context.Context, ids.ID) (ids.ID, error) {
					return subnetID1, errMissing
				}
				adapter := &validatorStateAdapter{state: state}
				ctx := context.Background()
				ids := consensus.IDs{
					SubnetID: subnetID0,
					ChainID:  chainID0,
				}
				ctx = consensus.WithIDs(ctx, ids)
				ctx = consensus.WithValidatorState(ctx, adapter)
				return ctx
			},
			chainID: chainID1,
			result:  errMissing,
		},
		{
			name: "wrong subnet",
			ctxF: func(t *testing.T) context.Context {
				state := validatorsmock.NewState(t)
				state.GetSubnetIDF = func(context.Context, ids.ID) (ids.ID, error) {
					return subnetID1, nil
				}
				adapter := &validatorStateAdapter{state: state}
				ctx := context.Background()
				ids := consensus.IDs{
					SubnetID: subnetID0,
					ChainID:  chainID0,
				}
				ctx = consensus.WithIDs(ctx, ids)
				ctx = consensus.WithValidatorState(ctx, adapter)
				return ctx
			},
			chainID: chainID1,
			result:  ErrMismatchedSubnetIDs,
		},
		{
			name: "same subnet",
			ctxF: func(t *testing.T) context.Context {
				state := validatorsmock.NewState(t)
				state.GetSubnetIDF = func(context.Context, ids.ID) (ids.ID, error) {
					return subnetID0, nil
				}
				adapter := &validatorStateAdapter{state: state}
				ctx := context.Background()
				ids := consensus.IDs{
					SubnetID: subnetID0,
					ChainID:  chainID0,
				}
				ctx = consensus.WithIDs(ctx, ids)
				ctx = consensus.WithValidatorState(ctx, adapter)
				return ctx
			},
			chainID: chainID1,
			result:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chainCtx := test.ctxF(t)

			result := SameSubnet(context.Background(), chainCtx, test.chainID)
			require.ErrorIs(t, result, test.result)
		})
	}
}
