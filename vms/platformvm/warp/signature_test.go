// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/constants"
)

const pChainHeight uint64 = 1337

var (
	errTest       = errors.New("non-nil error")
	sourceChainID = ids.GenerateTestID()
)

// mockValidatorState is a mock implementation of ValidatorState
type mockValidatorState struct {
	getValidatorSetF func(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]uint64, error)
	getNetIDF        func(ctx context.Context, chainID ids.ID) (ids.ID, error)
}

func (m *mockValidatorState) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	if m.getValidatorSetF != nil {
		return m.getValidatorSetF(ctx, height, netID)
	}
	return nil, nil
}

func (m *mockValidatorState) GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	if m.getNetIDF != nil {
		return m.getNetIDF(ctx, chainID)
	}
	return ids.Empty, nil
}

func TestSignatureVerification(t *testing.T) {
	netID := ids.GenerateTestID()
	sk0, err := bls.NewSecretKey()
	require.NoError(t, err)
	nodeID0 := ids.GenerateTestNodeID()

	_, err = bls.NewSecretKey()
	require.NoError(t, err)
	nodeID1 := ids.GenerateTestNodeID()

	sk2, err := bls.NewSecretKey()
	require.NoError(t, err)
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
			name:      "can't get netID",
			networkID: constants.UnitTestID,
			stateF: func() ValidatorState {
				return &mockValidatorState{
					getNetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						if chainID == sourceChainID {
							return netID, errTest
						}
						return ids.Empty, errTest
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
				return &mockValidatorState{
					getNetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						return netID, nil
					},
					getValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]uint64, error) {
						if height == pChainHeight && sID == netID {
							return nil, errTest
						}
						return nil, errTest
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
				return &mockValidatorState{
					getNetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						return netID, nil
					},
					getValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]uint64, error) {
						return map[ids.NodeID]uint64{
							nodeID0: math.MaxUint64,
							nodeID1: math.MaxUint64,
						}, nil
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
				return &mockValidatorState{
					getNetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						return netID, nil
					},
					getValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]uint64, error) {
						return map[ids.NodeID]uint64{
							nodeID0: 50,
						}, nil
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
				return &mockValidatorState{
					getNetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						return netID, nil
					},
					getValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]uint64, error) {
						return map[ids.NodeID]uint64{
							nodeID0: 50,
							nodeID1: 50,
							nodeID2: 50,
						}, nil
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
