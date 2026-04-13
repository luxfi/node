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

// --- Additional inversion tests ---

// TestSlashEvidence_ForgedSignature_WrongKey verifies that evidence signed
// by a different BLS key fails structural verification at the signature level.
// Note: SlashEvidence.Verify() only checks structure, not BLS verification.
// But the signature bytes from a different key are still well-formed, so
// Verify() passes. The BLS-level check happens in the executor.
// This test documents that the structural check is NOT a crypto check.
func TestSlashEvidence_ForgedSignature_WrongKey(t *testing.T) {
	// Generate two distinct key pairs
	skReal, err := bls.NewSecretKey()
	require.NoError(t, err)

	skForger, err := bls.NewSecretKey()
	require.NoError(t, err)
	pkReal := bls.PublicFromSecretKey(skReal)

	msgA := []byte("real-block-A")
	msgB := []byte("real-block-B")

	// Forger signs with their own key
	forgedSigA := bls.Sign(skForger, msgA)
	forgedSigB := bls.Sign(skForger, msgB)

	ev := SlashEvidence{
		Height:     100,
		Type:       DoubleVoteEvidence,
		MessageA:   msgA,
		SignatureA: bls.SignatureToBytes(forgedSigA),
		MessageB:   msgB,
		SignatureB: bls.SignatureToBytes(forgedSigB),
	}

	// Structural verification passes (bytes are valid BLS signature length)
	require.NoError(t, ev.Verify())

	// But BLS verification against the REAL key must fail
	sigA, err := bls.SignatureFromBytes(ev.SignatureA)
	require.NoError(t, err)
	require.False(t, bls.Verify(pkReal, sigA, msgA),
		"forged signature must not verify against real public key")
}

// TestSlashValidatorTx_SlashPercentBoundary verifies that 1_000_001 (>100%)
// is rejected by the switch-case BEFORE evidence verification or BaseTx.
// We also verify that 1_000_000 (exactly 100%) does NOT hit errSlashPercentTooLarge.
func TestSlashValidatorTx_SlashPercentBoundary(t *testing.T) {
	// 1_000_001 -- exceeds 100%, must be caught by the switch-case
	tx101 := &SlashValidatorTx{
		NodeID:          ids.GenerateTestNodeID(),
		Evidence:        SlashEvidence{}, // doesn't matter, rejected before evidence check
		SlashPercentage: 1_000_001,
	}
	err := tx101.SyntacticVerify(nil)
	require.ErrorIs(t, err, errSlashPercentTooLarge)

	// 1_000_000 = exactly at limit. The switch checks > 1_000_000, so this passes.
	// It will fail LATER at evidence.Verify() or BaseTx level, but NOT at the
	// slash percentage check. We verify the specific error is NOT errSlashPercentTooLarge.
	tx100 := &SlashValidatorTx{
		NodeID:          ids.GenerateTestNodeID(),
		Evidence:        SlashEvidence{Type: 0}, // will fail at evidence type check
		SlashPercentage: 1_000_000,
	}
	err = tx100.SyntacticVerify(nil)
	require.Error(t, err) // fails at evidence, not at percentage
	require.NotErrorIs(t, err, errSlashPercentTooLarge)

	// 0 -- zero slash is rejected as "no evidence"
	tx0 := &SlashValidatorTx{
		NodeID:          ids.GenerateTestNodeID(),
		Evidence:        SlashEvidence{},
		SlashPercentage: 0,
	}
	err = tx0.SyntacticVerify(nil)
	require.ErrorIs(t, err, errNoEvidence)
}

// TestSlashEvidence_BothMessagesEmpty verifies that empty messages are rejected.
func TestSlashEvidence_BothMessagesEmpty(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	sig := bls.SignatureToBytes(bls.Sign(sk, []byte("x")))

	ev := SlashEvidence{
		Height:     100,
		Type:       DoubleVoteEvidence,
		MessageA:   nil,
		SignatureA: sig,
		MessageB:   nil,
		SignatureB: sig,
	}
	require.ErrorIs(t, ev.Verify(), errEmptyMessage)
}

// TestSlashEvidence_MessageBEmpty verifies that only MessageB being empty
// is still rejected.
func TestSlashEvidence_MessageBEmpty(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	sig := bls.SignatureToBytes(bls.Sign(sk, []byte("x")))

	ev := SlashEvidence{
		Height:     100,
		Type:       DoubleVoteEvidence,
		MessageA:   []byte("valid"),
		SignatureA: sig,
		MessageB:   nil,
		SignatureB: sig,
	}
	require.ErrorIs(t, ev.Verify(), errEmptyMessage)
}

// TestSlashEvidence_SignatureTooShort verifies rejection of truncated sigs.
func TestSlashEvidence_SignatureTooShort(t *testing.T) {
	ev := SlashEvidence{
		Height:     100,
		Type:       DoubleVoteEvidence,
		MessageA:   []byte("A"),
		SignatureA: make([]byte, bls.SignatureLen-1), // one byte too short
		MessageB:   []byte("B"),
		SignatureB: make([]byte, bls.SignatureLen),
	}
	require.ErrorIs(t, ev.Verify(), errEmptySignature)
}

// TestSlashEvidence_SignatureTooLong verifies rejection of oversized sigs.
func TestSlashEvidence_SignatureTooLong(t *testing.T) {
	ev := SlashEvidence{
		Height:     100,
		Type:       DoubleVoteEvidence,
		MessageA:   []byte("A"),
		SignatureA: make([]byte, bls.SignatureLen+1), // one byte too long
		MessageB:   []byte("B"),
		SignatureB: make([]byte, bls.SignatureLen),
	}
	require.ErrorIs(t, ev.Verify(), errEmptySignature)
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
