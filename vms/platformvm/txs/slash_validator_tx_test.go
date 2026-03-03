// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"testing"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

func TestSlashEvidenceVerify(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)

	msgA := []byte("block-hash-A")
	msgB := []byte("block-hash-B")
	sigA := bls.Sign(sk, msgA)
	sigB := bls.Sign(sk, msgB)

	sigABytes := bls.SignatureToBytes(sigA)
	sigBBytes := bls.SignatureToBytes(sigB)

	tests := []struct {
		name    string
		ev      SlashEvidence
		wantErr error
	}{
		{
			name: "valid double-vote evidence",
			ev: SlashEvidence{
				Height:     100,
				Type:       DoubleVoteEvidence,
				MessageA:   msgA,
				SignatureA: sigABytes,
				MessageB:   msgB,
				SignatureB: sigBBytes,
			},
			wantErr: nil,
		},
		{
			name: "valid double-sign evidence",
			ev: SlashEvidence{
				Height:     100,
				Type:       DoubleSignEvidence,
				MessageA:   msgA,
				SignatureA: sigABytes,
				MessageB:   msgB,
				SignatureB: sigBBytes,
			},
			wantErr: nil,
		},
		{
			name: "invalid evidence type",
			ev: SlashEvidence{
				Height:     100,
				Type:       0,
				MessageA:   msgA,
				SignatureA: sigABytes,
				MessageB:   msgB,
				SignatureB: sigBBytes,
			},
			wantErr: errInvalidEvidenceType,
		},
		{
			name: "same messages",
			ev: SlashEvidence{
				Height:     100,
				Type:       DoubleVoteEvidence,
				MessageA:   msgA,
				SignatureA: sigABytes,
				MessageB:   msgA,
				SignatureB: sigABytes,
			},
			wantErr: errSameContent,
		},
		{
			name: "empty message A",
			ev: SlashEvidence{
				Height:     100,
				Type:       DoubleVoteEvidence,
				MessageA:   nil,
				SignatureA: sigABytes,
				MessageB:   msgB,
				SignatureB: sigBBytes,
			},
			wantErr: errEmptyMessage,
		},
		{
			name: "empty signature A",
			ev: SlashEvidence{
				Height:     100,
				Type:       DoubleVoteEvidence,
				MessageA:   msgA,
				SignatureA: nil,
				MessageB:   msgB,
				SignatureB: sigBBytes,
			},
			wantErr: errEmptySignature,
		},
		{
			name: "wrong signature length",
			ev: SlashEvidence{
				Height:     100,
				Type:       DoubleVoteEvidence,
				MessageA:   msgA,
				SignatureA: []byte("short"),
				MessageB:   msgB,
				SignatureB: sigBBytes,
			},
			wantErr: errEmptySignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ev.Verify()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSlashValidatorTxSyntacticVerify(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)

	msgA := []byte("block-hash-A")
	msgB := []byte("block-hash-B")
	sigA := bls.SignatureToBytes(bls.Sign(sk, msgA))
	sigB := bls.SignatureToBytes(bls.Sign(sk, msgB))

	validEvidence := SlashEvidence{
		Height:     100,
		Type:       DoubleVoteEvidence,
		MessageA:   msgA,
		SignatureA: sigA,
		MessageB:   msgB,
		SignatureB: sigB,
	}

	tests := []struct {
		name    string
		tx      *SlashValidatorTx
		wantErr error
	}{
		{
			name:    "nil tx",
			tx:      nil,
			wantErr: ErrNilTx,
		},
		{
			name: "empty node ID",
			tx: &SlashValidatorTx{
				NodeID:          ids.EmptyNodeID,
				Evidence:        validEvidence,
				SlashPercentage: 100_000,
			},
			wantErr: errEmptyNodeID,
		},
		{
			name: "zero slash percentage",
			tx: &SlashValidatorTx{
				NodeID:          ids.GenerateTestNodeID(),
				Evidence:        validEvidence,
				SlashPercentage: 0,
			},
			wantErr: errNoEvidence,
		},
		{
			name: "slash percentage too large",
			tx: &SlashValidatorTx{
				NodeID:          ids.GenerateTestNodeID(),
				Evidence:        validEvidence,
				SlashPercentage: 1_000_001,
			},
			wantErr: errSlashPercentTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tx.SyntacticVerify(nil)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestSlashEvidenceVerifySignatures(t *testing.T) {
	// Verify that the evidence round-trips through BLS correctly
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk := bls.PublicFromSecretKey(sk)

	msgA := []byte("vote-for-block-A-at-height-100")
	msgB := []byte("vote-for-block-B-at-height-100")
	sigA := bls.Sign(sk, msgA)
	sigB := bls.Sign(sk, msgB)

	// Both signatures should verify against the public key
	require.True(t, bls.Verify(pk, sigA, msgA))
	require.True(t, bls.Verify(pk, sigB, msgB))

	// Cross-verification should fail
	require.False(t, bls.Verify(pk, sigA, msgB))
	require.False(t, bls.Verify(pk, sigB, msgA))

	// Evidence struct should pass structural verification
	ev := SlashEvidence{
		Height:     100,
		Type:       DoubleVoteEvidence,
		MessageA:   msgA,
		SignatureA: bls.SignatureToBytes(sigA),
		MessageB:   msgB,
		SignatureB: bls.SignatureToBytes(sigB),
	}
	require.NoError(t, ev.Verify())

	// Verify messages are actually different
	require.False(t, bytes.Equal(msgA, msgB))
}
