// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

// testValidatorState is a test implementation of ValidatorState
type testValidatorState struct {
	chains map[ids.ID]ids.ID // chainID -> netID
	err    error
}

func (s *testValidatorState) GetChainID(chainID ids.ID) (ids.ID, error) {
	if s.err != nil {
		return ids.Empty, s.err
	}
	if netID, ok := s.chains[chainID]; ok {
		return netID, nil
	}
	return ids.Empty, errMissing
}

var errMissing = errors.New("missing")

func TestSameNet(t *testing.T) {
	netID0 := ids.GenerateTestID()
	netID1 := ids.GenerateTestID()
	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	tests := []struct {
		name     string
		chainRuntime *ChainContext
		chainID  ids.ID
		result   error
	}{
		{
			name: "same chain",
			chainRuntime: &ChainContext{
				ChainID:        chainID0,
				NetID:          netID0,
				ValidatorState: &testValidatorState{},
			},
			chainID: chainID0,
			result:  ErrSameChainID,
		},
		{
			name: "unknown chain",
			chainRuntime: &ChainContext{
				ChainID: chainID0,
				NetID:   netID0,
				ValidatorState: &testValidatorState{
					chains: map[ids.ID]ids.ID{},
					err:    errMissing,
				},
			},
			chainID: chainID1,
			result:  errMissing,
		},
		{
			name: "wrong chain",
			chainRuntime: &ChainContext{
				ChainID: chainID0,
				NetID:   netID0,
				ValidatorState: &testValidatorState{
					chains: map[ids.ID]ids.ID{
						chainID1: netID1,
					},
				},
			},
			chainID: chainID1,
			result:  ErrMismatchedNetIDs,
		},
		{
			name: "same chain",
			chainRuntime: &ChainContext{
				ChainID: chainID0,
				NetID:   netID0,
				ValidatorState: &testValidatorState{
					chains: map[ids.ID]ids.ID{
						chainID1: netID0,
					},
				},
			},
			chainID: chainID1,
			result:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := SameNet(context.Background(), test.chainRuntime, test.chainID)
			require.ErrorIs(t, result, test.result)
		})
	}
}
