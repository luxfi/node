// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus"
	consensuscontext "github.com/luxfi/consensus/context"
	"github.com/luxfi/ids"
)

// testValidatorState is a test implementation of ValidatorState
type testValidatorState struct {
	height     uint64
	validators map[ids.ID]map[ids.NodeID]uint64
	subnets    map[ids.ID]ids.ID // chainID -> subnetID
	err        error
}

func (s *testValidatorState) GetCurrentHeight() (uint64, error) {
	return s.height, s.err
}

func (s *testValidatorState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (s *testValidatorState) GetValidatorSet(height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.validators[netID], nil
}

func (s *testValidatorState) GetNetID(chainID ids.ID) (ids.ID, error) {
	if s.err != nil {
		return ids.Empty, s.err
	}
	if subnet, ok := s.subnets[chainID]; ok {
		return subnet, nil
	}
	return ids.Empty, errMissing
}

func (s *testValidatorState) GetChainID(blockID ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (s *testValidatorState) GetCurrentValidators(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*consensuscontext.GetValidatorOutput, error) {
	return nil, nil
}

func (s *testValidatorState) GetSubnetID(chainID ids.ID) (ids.ID, error) {
	return s.GetNetID(chainID)
}

var errMissing = errors.New("missing")

func TestSameSubnet(t *testing.T) {
	netID0 := ids.GenerateTestID()
	netID1 := ids.GenerateTestID()
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
				state := &testValidatorState{
					subnets: make(map[ids.ID]ids.ID),
				}
				ctx := context.Background()
				ids := consensus.IDs{
					NetID:   netID0,
					ChainID: chainID0,
				}
				ctx = consensus.WithIDs(ctx, ids)
				ctx = consensus.WithValidatorState(ctx, state)
				return ctx
			},
			chainID: chainID0,
			result:  ErrSameChainID,
		},
		{
			name: "unknown chain",
			ctxF: func(t *testing.T) context.Context {
				state := &testValidatorState{
					subnets: make(map[ids.ID]ids.ID),
				}
				ctx := context.Background()
				ids := consensus.IDs{
					NetID:   netID0,
					ChainID: chainID0,
				}
				ctx = consensus.WithIDs(ctx, ids)
				ctx = consensus.WithValidatorState(ctx, state)
				return ctx
			},
			chainID: chainID1,
			result:  errMissing,
		},
		{
			name: "wrong subnet",
			ctxF: func(t *testing.T) context.Context {
				state := &testValidatorState{
					subnets: map[ids.ID]ids.ID{
						chainID1: netID1,
					},
				}
				ctx := context.Background()
				ids := consensus.IDs{
					NetID:   netID0,
					ChainID: chainID0,
				}
				ctx = consensus.WithIDs(ctx, ids)
				ctx = consensus.WithValidatorState(ctx, state)
				return ctx
			},
			chainID: chainID1,
			result:  ErrMismatchedNetIDs,
		},
		{
			name: "same subnet",
			ctxF: func(t *testing.T) context.Context {
				state := &testValidatorState{
					subnets: map[ids.ID]ids.ID{
						chainID1: netID0,
					},
				}
				ctx := context.Background()
				ids := consensus.IDs{
					NetID:   netID0,
					ChainID: chainID0,
				}
				ctx = consensus.WithIDs(ctx, ids)
				ctx = consensus.WithValidatorState(ctx, state)
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
