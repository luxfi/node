// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/consensus/validators/validatorsmock"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/math/set"
)

const pChainHeight uint64 = 1337

var (
	errTest       = errors.New("non-nil error")
	sourceChainID = ids.GenerateTestID()
)

// validatorStateAdapter adapts validators.State to ValidatorState
type validatorStateAdapter struct {
	state validators.State
}

func (v *validatorStateAdapter) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	validatorSet, err := v.state.GetValidatorSet(ctx, height, subnetID)
	if err != nil {
		return nil, err
	}
	
	result := make(map[ids.NodeID]uint64, len(validatorSet))
	for nodeID, validator := range validatorSet {
		result[nodeID] = validator.Weight
	}
	return result, nil
}

func (v *validatorStateAdapter) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	return v.state.GetSubnetID(ctx, chainID)
}

func TestSignatureVerification(t *testing.T) {
	subnetID := ids.GenerateTestID()
	sk0, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk0 := bls.PublicFromSecretKey(sk0)
	nodeID0 := ids.GenerateTestNodeID()

	sk1, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk1 := bls.PublicFromSecretKey(sk1)
	nodeID1 := ids.GenerateTestNodeID()

	sk2, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk2 := bls.PublicFromSecretKey(sk2)
	nodeID2 := ids.GenerateTestNodeID()

	tests := []struct {
		name      string
		networkID uint32
		stateF    func() ValidatorState
		quorumNum uint64
		quorumDen uint64
		msgF      func(*require.Assertions) *Message
		err       error
	}{
		{
			name:      "can't get subnetID",
			networkID: constants.UnitTestID,
			stateF: func() ValidatorState {
				return &validatorStateAdapter{
					state: &validatorsmock.State{
						GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
							if chainID == sourceChainID {
								return subnetID, errTest
							}
							return ids.Empty, errTest
						},
					},
				}
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					nil,
				)
				require.NoError(err)

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{},
				)
				require.NoError(err)
				return msg
			},
			err: errTest,
		},
		{
			name:      "can't get validator set",
			networkID: constants.UnitTestID,
			stateF: func() ValidatorState {
				return &validatorStateAdapter{
					state: &validatorsmock.State{
					GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						return subnetID, nil
					},
					GetValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						if height == pChainHeight && sID == subnetID {
							return nil, errTest
						}
						return nil, errTest
					},
				},
				}
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					nil,
				)
				require.NoError(err)

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{},
				)
				require.NoError(err)
				return msg
			},
			err: errTest,
		},
		{
			name:      "weight overflow",
			networkID: constants.UnitTestID,
			stateF: func() ValidatorState {
				return &validatorStateAdapter{
					state: &validatorsmock.State{
					GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						return subnetID, nil
					},
					GetValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return map[ids.NodeID]*validators.GetValidatorOutput{
							nodeID0: {
								NodeID:    nodeID0,
								PublicKey: pk0,
								Weight:    math.MaxUint64,
							},
							nodeID1: {
								NodeID:    nodeID1,
								PublicKey: pk1,
								Weight:    math.MaxUint64,
							},
						}, nil
					},
				},
				}
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					nil,
				)
				require.NoError(err)

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{},
				)
				require.NoError(err)
				return msg
			},
			err: ErrWeightOverflow,
		},
		{
			name:      "invalid bit set index",
			networkID: constants.UnitTestID,
			stateF: func() ValidatorState {
				return &validatorStateAdapter{
					state: &validatorsmock.State{
					GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						return subnetID, nil
					},
					GetValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return map[ids.NodeID]*validators.GetValidatorOutput{
							nodeID0: {
								NodeID:    nodeID0,
								PublicKey: pk0,
								Weight:    50,
							},
						}, nil
					},
				},
				}
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					nil,
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(1) // This validator doesn't exist

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers: signers.Bytes(),
					},
				)
				require.NoError(err)
				return msg
			},
			err: ErrInvalidBitSet,
		},
		{
			name:      "valid signature",
			networkID: constants.UnitTestID,
			stateF: func() ValidatorState {
				return &validatorStateAdapter{
					state: &validatorsmock.State{
					GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						return subnetID, nil
					},
					GetValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return map[ids.NodeID]*validators.GetValidatorOutput{
							nodeID0: {
								NodeID:    nodeID0,
								PublicKey: pk0,
								Weight:    50,
							},
							nodeID1: {
								NodeID:    nodeID1,
								PublicKey: pk1,
								Weight:    50,
							},
							nodeID2: {
								NodeID:    nodeID2,
								PublicKey: pk2,
								Weight:    50,
							},
						}, nil
					},
				},
				}
			},
			quorumNum: 1,
			quorumDen: 2,
			msgF: func(require *require.Assertions) *Message {
				unsignedMsg, err := NewUnsignedMessage(
					constants.UnitTestID,
					sourceChainID,
					[]byte("payload"),
				)
				require.NoError(err)

				signers := set.NewBits()
				signers.Add(0) // nodeID0 signs
				signers.Add(2) // nodeID2 signs

				unsignedBytes := unsignedMsg.Bytes()
				sig0 := bls.Sign(sk0, unsignedBytes)
				sig2 := bls.Sign(sk2, unsignedBytes)
				aggSig, err := bls.AggregateSignatures([]*bls.Signature{sig0, sig2})
				require.NoError(err)

				msg, err := NewMessage(
					unsignedMsg,
					&BitSetSignature{
						Signers:   signers.Bytes(),
						Signature: [bls.SignatureLen]byte(bls.SignatureToBytes(aggSig)),
					},
				)
				require.NoError(err)
				return msg
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			msg := tt.msgF(require)
			pChainState := tt.stateF()

			err := msg.Signature.Verify(
				context.Background(),
				&msg.UnsignedMessage,
				tt.networkID,
				pChainState,
				pChainHeight,
				tt.quorumNum,
				tt.quorumDen,
			)
			require.ErrorIs(err, tt.err)
		})
	}
}