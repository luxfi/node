// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

// TestHIGH1_SyntacticVerifyNilRuntimeDoesNotPanic verifies that calling
// SyntacticVerify with a nil runtime returns an error instead of panicking.
//
// Before the fix, SyntacticVerify(nil) would dereference the nil runtime
// in BaseTx.SyntacticVerify, causing a panic.
func TestHIGH1_SyntacticVerifyNilRuntimeDoesNotPanic(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)

	msgA := []byte("block-A")
	msgB := []byte("block-B")
	sigA := bls.SignatureToBytes(bls.Sign(sk, msgA))
	sigB := bls.SignatureToBytes(bls.Sign(sk, msgB))

	tx := &SlashValidatorTx{
		NodeID: ids.GenerateTestNodeID(),
		Evidence: SlashEvidence{
			Height:     100,
			Type:       DoubleVoteEvidence,
			MessageA:   msgA,
			SignatureA: sigA,
			MessageB:   msgB,
			SignatureB: sigB,
		},
		SlashPercentage: 100_000,
	}

	// Must not panic, must return errNilRuntime
	err = tx.SyntacticVerify(nil)
	require.ErrorIs(t, err, errNilRuntime,
		"SyntacticVerify(nil) must return errNilRuntime, not panic")
}

// TestHIGH1_SyntacticVerifyNilTxStillWorks verifies that nil tx check
// still takes priority over nil runtime.
func TestHIGH1_SyntacticVerifyNilTxStillWorks(t *testing.T) {
	var tx *SlashValidatorTx
	err := tx.SyntacticVerify(nil)
	require.ErrorIs(t, err, ErrNilTx)
}

// TestHIGH1_StructuralChecksStillWorkWithNilRuntime verifies that
// structural checks (NodeID, slash percentage, evidence) execute before
// the nil runtime guard, so they still produce the correct errors.
func TestHIGH1_StructuralChecksStillWorkWithNilRuntime(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)

	sigBytes := bls.SignatureToBytes(bls.Sign(sk, []byte("x")))

	tests := []struct {
		name    string
		tx      *SlashValidatorTx
		wantErr error
	}{
		{
			name: "empty node ID",
			tx: &SlashValidatorTx{
				NodeID:          ids.EmptyNodeID,
				SlashPercentage: 100_000,
			},
			wantErr: errEmptyNodeID,
		},
		{
			name: "zero slash percentage",
			tx: &SlashValidatorTx{
				NodeID:          ids.GenerateTestNodeID(),
				SlashPercentage: 0,
			},
			wantErr: errNoEvidence,
		},
		{
			name: "slash percentage too large",
			tx: &SlashValidatorTx{
				NodeID:          ids.GenerateTestNodeID(),
				SlashPercentage: 1_000_001,
			},
			wantErr: errSlashPercentTooLarge,
		},
		{
			name: "invalid evidence type with nil runtime",
			tx: &SlashValidatorTx{
				NodeID: ids.GenerateTestNodeID(),
				Evidence: SlashEvidence{
					Type:       0, // invalid
					MessageA:   []byte("a"),
					SignatureA: sigBytes,
					MessageB:   []byte("b"),
					SignatureB: sigBytes,
				},
				SlashPercentage: 100_000,
			},
			wantErr: errInvalidEvidenceType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tx.SyntacticVerify(nil)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
