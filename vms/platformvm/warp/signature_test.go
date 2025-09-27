// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
)

const pChainHeight uint64 = 1337

var (
	errTest       = errors.New("non-nil error")
	sourceChainID = ids.GenerateTestID()
)

// mockValidatorState is a mock implementation of ValidatorState
type mockValidatorState struct {
	getValidatorSetF func(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*ValidatorData, error)
	getNetIDF        func(ctx context.Context, chainID ids.ID) (ids.ID, error)
}

func (m *mockValidatorState) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*ValidatorData, error) {
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

	sk1, err := bls.NewSecretKey()
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
					getValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]*ValidatorData, error) {
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
					getValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]*ValidatorData, error) {
						return map[ids.NodeID]*ValidatorData{
							nodeID0: {NodeID: nodeID0, Weight: math.MaxUint64},
							nodeID1: {NodeID: nodeID1, Weight: math.MaxUint64},
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
					getValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]*ValidatorData, error) {
						pk0 := bls.PublicFromSecretKey(sk0)
						return map[ids.NodeID]*ValidatorData{
							nodeID0: {NodeID: nodeID0, PublicKey: bls.PublicKeyToUncompressedBytes(pk0), Weight: 50},
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
			err: ErrUnknownValidator, // Index out of bounds
		},
		{
			name:      "valid signature",
			networkID: constants.UnitTestID,
			stateF: func() ValidatorState {
				return &mockValidatorState{
					getNetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
						return netID, nil
					},
					getValidatorSetF: func(ctx context.Context, height uint64, sID ids.ID) (map[ids.NodeID]*ValidatorData, error) {
						pk0 := bls.PublicFromSecretKey(sk0)
						pk1 := bls.PublicFromSecretKey(sk1)
						pk2 := bls.PublicFromSecretKey(sk2)
						return map[ids.NodeID]*ValidatorData{
							nodeID0: {NodeID: nodeID0, PublicKey: bls.PublicKeyToUncompressedBytes(pk0), Weight: 50},
							nodeID1: {NodeID: nodeID1, PublicKey: bls.PublicKeyToUncompressedBytes(pk1), Weight: 50},
							nodeID2: {NodeID: nodeID2, PublicKey: bls.PublicKeyToUncompressedBytes(pk2), Weight: 50},
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

				// Create the sorted validator list to determine indices
				pk0 := bls.PublicFromSecretKey(sk0)
				pk1 := bls.PublicFromSecretKey(sk1)
				pk2 := bls.PublicFromSecretKey(sk2)
				
				// Create validators in the same way FlattenValidatorSet does
				vdrs := []*Validator{
					{PublicKey: pk0, PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0)},
					{PublicKey: pk1, PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1)},
					{PublicKey: pk2, PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk2)},
				}
				// Sort to get canonical order
				utils.Sort(vdrs)
				
				// Find which indices correspond to pk0 and pk2
				var idx0, idx2 int
				pk0Bytes := bls.PublicKeyToUncompressedBytes(pk0)
				pk2Bytes := bls.PublicKeyToUncompressedBytes(pk2)
				for i, v := range vdrs {
					if bytes.Equal(v.PublicKeyBytes, pk0Bytes) {
						idx0 = i
					}
					if bytes.Equal(v.PublicKeyBytes, pk2Bytes) {
						idx2 = i
					}
				}

				signers := set.NewBits()
				signers.Add(idx0)
				signers.Add(idx2)

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
