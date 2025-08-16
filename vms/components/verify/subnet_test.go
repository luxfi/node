// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/luxfi/consensus/validators/validatorsmock"
	"github.com/luxfi/ids"
)

var errMissing = errors.New("missing")

func TestSameSubnet(t *testing.T) {
	subnetID0 := ids.GenerateTestID()
	subnetID1 := ids.GenerateTestID()
	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	tests := []struct {
		name    string
		ctxF    func(*gomock.Controller) context.Context
		chainID ids.ID
		result  error
	}{
		{
			name: "same chain",
			ctxF: func(ctrl *gomock.Controller) context.Context {
				state := validatorsmock.NewState(ctrl)
				return &context.Context{
					SubnetID:       subnetID0,
					ChainID:        chainID0,
					ValidatorState: state,
				}
			},
			chainID: chainID0,
			result:  ErrSameChainID,
		},
		{
			name: "unknown chain",
			ctxF: func(ctrl *gomock.Controller) context.Context {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), chainID1).Return(subnetID1, errMissing)
				return &context.Context{
					SubnetID:       subnetID0,
					ChainID:        chainID0,
					ValidatorState: state,
				}
			},
			chainID: chainID1,
			result:  errMissing,
		},
		{
			name: "wrong subnet",
			ctxF: func(ctrl *gomock.Controller) context.Context {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), chainID1).Return(subnetID1, nil)
				return &context.Context{
					SubnetID:       subnetID0,
					ChainID:        chainID0,
					ValidatorState: state,
				}
			},
			chainID: chainID1,
			result:  ErrMismatchedSubnetIDs,
		},
		{
			name: "same subnet",
			ctxF: func(ctrl *gomock.Controller) context.Context {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), chainID1).Return(subnetID0, nil)
				return &context.Context{
					SubnetID:       subnetID0,
					ChainID:        chainID0,
					ValidatorState: state,
				}
			},
			chainID: chainID1,
			result:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			ctx := test.ctxF(ctrl)

			result := SameSubnet(context.Background(), ctx, test.chainID)
			require.ErrorIs(t, result, test.result)
		})
	}
}
