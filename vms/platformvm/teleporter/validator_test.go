// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"context"
	"math"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
<<<<<<< HEAD
<<<<<<< HEAD
	"github.com/ava-labs/avalanchego/utils/set"
=======
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
=======
	"github.com/ava-labs/avalanchego/utils/set"
>>>>>>> 483d9bd18 (Move bit sets to the set package (#2365))
)

func TestGetCanonicalValidatorSet(t *testing.T) {
	type test struct {
		name           string
		stateF         func(*gomock.Controller) validators.State
		expectedVdrs   []*Validator
		expectedWeight uint64
		expectedErr    error
	}

	tests := []test{
		{
			name: "can't get validator set",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
<<<<<<< HEAD
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(nil, errTest)
				return state
			},
			expectedErr: errTest,
=======
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(nil, errMock)
				return state
			},
			expectedErr: errMock,
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
		},
		{
			name: "all validators have public keys; no duplicate pub keys",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(
					map[ids.NodeID]*validators.GetValidatorOutput{
						testVdrs[0].nodeID: {
							NodeID:    testVdrs[0].nodeID,
							PublicKey: testVdrs[0].vdr.PublicKey,
							Weight:    testVdrs[0].vdr.Weight,
						},
						testVdrs[1].nodeID: {
							NodeID:    testVdrs[1].nodeID,
							PublicKey: testVdrs[1].vdr.PublicKey,
							Weight:    testVdrs[1].vdr.Weight,
						},
					},
					nil,
				)
				return state
			},
			expectedVdrs:   []*Validator{testVdrs[0].vdr, testVdrs[1].vdr},
			expectedWeight: 6,
			expectedErr:    nil,
		},
		{
			name: "all validators have public keys; duplicate pub keys",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(
					map[ids.NodeID]*validators.GetValidatorOutput{
						testVdrs[0].nodeID: {
							NodeID:    testVdrs[0].nodeID,
							PublicKey: testVdrs[0].vdr.PublicKey,
							Weight:    testVdrs[0].vdr.Weight,
						},
						testVdrs[1].nodeID: {
							NodeID:    testVdrs[1].nodeID,
							PublicKey: testVdrs[1].vdr.PublicKey,
							Weight:    testVdrs[1].vdr.Weight,
						},
						testVdrs[2].nodeID: {
							NodeID:    testVdrs[2].nodeID,
							PublicKey: testVdrs[0].vdr.PublicKey,
							Weight:    testVdrs[0].vdr.Weight,
						},
					},
					nil,
				)
				return state
			},
			expectedVdrs: []*Validator{
				{
					PublicKey:      testVdrs[0].vdr.PublicKey,
					PublicKeyBytes: testVdrs[0].vdr.PublicKeyBytes,
					Weight:         testVdrs[0].vdr.Weight * 2,
					NodeIDs: []ids.NodeID{
						testVdrs[0].nodeID,
						testVdrs[2].nodeID,
					},
				},
				testVdrs[1].vdr,
			},
			expectedWeight: 9,
			expectedErr:    nil,
		},
		{
			name: "validator without public key; no duplicate pub keys",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(
					map[ids.NodeID]*validators.GetValidatorOutput{
						testVdrs[0].nodeID: {
							NodeID:    testVdrs[0].nodeID,
							PublicKey: nil,
							Weight:    testVdrs[0].vdr.Weight,
						},
						testVdrs[1].nodeID: {
							NodeID:    testVdrs[1].nodeID,
							PublicKey: testVdrs[1].vdr.PublicKey,
							Weight:    testVdrs[1].vdr.Weight,
						},
					},
					nil,
				)
				return state
			},
			expectedVdrs:   []*Validator{testVdrs[1].vdr},
			expectedWeight: 6,
			expectedErr:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			state := tt.stateF(ctrl)

			vdrs, weight, err := GetCanonicalValidatorSet(context.Background(), state, pChainHeight, subnetID)
			require.ErrorIs(tt.expectedErr, err)
			if err != nil {
				return
			}
			require.Equal(tt.expectedWeight, weight)

			// These are pointers so have to test equality like this
			require.Equal(len(tt.expectedVdrs), len(vdrs))
			for i, expectedVdr := range tt.expectedVdrs {
				gotVdr := vdrs[i]
				expectedPKBytes := bls.PublicKeyToBytes(expectedVdr.PublicKey)
				gotPKBytes := bls.PublicKeyToBytes(gotVdr.PublicKey)
				require.Equal(expectedPKBytes, gotPKBytes)
				require.Equal(expectedVdr.PublicKeyBytes, gotVdr.PublicKeyBytes)
				require.Equal(expectedVdr.Weight, gotVdr.Weight)
				require.ElementsMatch(expectedVdr.NodeIDs, gotVdr.NodeIDs)
			}
		})
	}
}

func TestFilterValidators(t *testing.T) {
	sk0, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk0 := bls.PublicFromSecretKey(sk0)
	vdr0 := &Validator{
		PublicKey:      pk0,
		PublicKeyBytes: bls.PublicKeyToBytes(pk0),
		Weight:         1,
	}

	sk1, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk1 := bls.PublicFromSecretKey(sk1)
	vdr1 := &Validator{
		PublicKey:      pk1,
		PublicKeyBytes: bls.PublicKeyToBytes(pk1),
		Weight:         2,
	}

	type test struct {
		name         string
<<<<<<< HEAD
<<<<<<< HEAD
		indices      set.Bits
=======
		indices      ids.BigBitSet
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
=======
		indices      set.Bits
>>>>>>> 483d9bd18 (Move bit sets to the set package (#2365))
		vdrs         []*Validator
		expectedVdrs []*Validator
		expectedErr  error
	}

	tests := []test{
		{
			name:         "empty",
<<<<<<< HEAD
<<<<<<< HEAD
			indices:      set.NewBits(),
=======
			indices:      ids.NewBigBitSet(),
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
=======
			indices:      set.NewBits(),
>>>>>>> 483d9bd18 (Move bit sets to the set package (#2365))
			vdrs:         []*Validator{},
			expectedVdrs: []*Validator{},
			expectedErr:  nil,
		},
		{
			name:        "unknown validator",
<<<<<<< HEAD
<<<<<<< HEAD
			indices:     set.NewBits(2),
=======
			indices:     ids.NewBigBitSet(2),
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
=======
			indices:     set.NewBits(2),
>>>>>>> 483d9bd18 (Move bit sets to the set package (#2365))
			vdrs:        []*Validator{vdr0, vdr1},
			expectedErr: ErrUnknownValidator,
		},
		{
			name:    "two filtered out",
<<<<<<< HEAD
<<<<<<< HEAD
			indices: set.NewBits(),
=======
			indices: ids.NewBigBitSet(),
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
=======
			indices: set.NewBits(),
>>>>>>> 483d9bd18 (Move bit sets to the set package (#2365))
			vdrs: []*Validator{
				vdr0,
				vdr1,
			},
			expectedVdrs: []*Validator{},
			expectedErr:  nil,
		},
		{
			name:    "one filtered out",
<<<<<<< HEAD
<<<<<<< HEAD
			indices: set.NewBits(1),
=======
			indices: ids.NewBigBitSet(1),
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
=======
			indices: set.NewBits(1),
>>>>>>> 483d9bd18 (Move bit sets to the set package (#2365))
			vdrs: []*Validator{
				vdr0,
				vdr1,
			},
			expectedVdrs: []*Validator{
				vdr1,
			},
			expectedErr: nil,
		},
		{
			name:    "none filtered out",
<<<<<<< HEAD
<<<<<<< HEAD
			indices: set.NewBits(0, 1),
=======
			indices: ids.NewBigBitSet(0, 1),
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
=======
			indices: set.NewBits(0, 1),
>>>>>>> 483d9bd18 (Move bit sets to the set package (#2365))
			vdrs: []*Validator{
				vdr0,
				vdr1,
			},
			expectedVdrs: []*Validator{
				vdr0,
				vdr1,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			vdrs, err := FilterValidators(tt.indices, tt.vdrs)
			require.ErrorIs(err, tt.expectedErr)
<<<<<<< HEAD
			if err == nil {
				require.Equal(tt.expectedVdrs, vdrs)
			}
=======
			if err != nil {
				return
			}
			require.Equal(tt.expectedVdrs, vdrs)
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
		})
	}
}

func TestSumWeight(t *testing.T) {
	vdr0 := &Validator{
		Weight: 1,
	}
	vdr1 := &Validator{
		Weight: 2,
	}
	vdr2 := &Validator{
		Weight: math.MaxUint64,
	}

	type test struct {
		name        string
		vdrs        []*Validator
		expectedSum uint64
		expectedErr error
	}

	tests := []test{
		{
			name:        "empty",
			vdrs:        []*Validator{},
			expectedSum: 0,
		},
		{
			name:        "one",
			vdrs:        []*Validator{vdr0},
			expectedSum: 1,
		},
		{
			name:        "two",
			vdrs:        []*Validator{vdr0, vdr1},
			expectedSum: 3,
		},
		{
			name:        "overflow",
			vdrs:        []*Validator{vdr0, vdr2},
			expectedErr: ErrWeightOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			sum, err := SumWeight(tt.vdrs)
			require.ErrorIs(err, tt.expectedErr)
<<<<<<< HEAD
			if err == nil {
				require.Equal(tt.expectedSum, sum)
			}
=======
			if err != nil {
				return
			}
			require.Equal(tt.expectedSum, sum)
>>>>>>> 479196a9c (Add Teleporter message verification (#2207))
		})
	}
}
