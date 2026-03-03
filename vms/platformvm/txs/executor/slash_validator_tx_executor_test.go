// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/stretchr/testify/require"
)

func TestVerifyEvidenceSignatures(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk := bls.PublicFromSecretKey(sk)

	msgA := []byte("block-hash-A-at-height-100")
	msgB := []byte("block-hash-B-at-height-100")
	sigA := bls.Sign(sk, msgA)
	sigB := bls.Sign(sk, msgB)

	validEvidence := &txs.SlashEvidence{
		Height:     100,
		Type:       txs.DoubleVoteEvidence,
		MessageA:   msgA,
		SignatureA: bls.SignatureToBytes(sigA),
		MessageB:   msgB,
		SignatureB: bls.SignatureToBytes(sigB),
	}

	t.Run("valid signatures", func(t *testing.T) {
		require.NoError(t, verifyEvidenceSignatures(pk, validEvidence))
	})

	t.Run("wrong public key", func(t *testing.T) {
		otherSK, err := bls.NewSecretKey()
		require.NoError(t, err)
		otherPK := bls.PublicFromSecretKey(otherSK)

		err = verifyEvidenceSignatures(otherPK, validEvidence)
		require.ErrorIs(t, err, errInvalidEvidenceSignature)
	})

	t.Run("swapped signatures", func(t *testing.T) {
		swappedEvidence := &txs.SlashEvidence{
			Height:     100,
			Type:       txs.DoubleVoteEvidence,
			MessageA:   msgA,
			SignatureA: bls.SignatureToBytes(sigB), // sig for msgB applied to msgA
			MessageB:   msgB,
			SignatureB: bls.SignatureToBytes(sigA),
		}
		err := verifyEvidenceSignatures(pk, swappedEvidence)
		require.ErrorIs(t, err, errInvalidEvidenceSignature)
	})

	t.Run("corrupted signature bytes", func(t *testing.T) {
		corruptedSigA := make([]byte, bls.SignatureLen)
		copy(corruptedSigA, bls.SignatureToBytes(sigA))
		corruptedSigA[0] ^= 0xFF

		corruptedEvidence := &txs.SlashEvidence{
			Height:     100,
			Type:       txs.DoubleVoteEvidence,
			MessageA:   msgA,
			SignatureA: corruptedSigA,
			MessageB:   msgB,
			SignatureB: bls.SignatureToBytes(sigB),
		}
		err := verifyEvidenceSignatures(pk, corruptedEvidence)
		require.ErrorIs(t, err, errInvalidEvidenceSignature)
	})
}

func TestSlashPercentages(t *testing.T) {
	// Verify the constants match expected values
	require.Equal(t, uint32(100_000), uint32(DoubleVoteSlashPercent))   // 10%
	require.Equal(t, uint32(500_000), uint32(DoubleSignSlashPercent))   // 50%
}
