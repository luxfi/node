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

// --- Additional inversion tests for the executor ---

// TestVerifyEvidenceSignatures_ForgedByDifferentKey verifies that evidence
// signed by a completely different BLS key is rejected.
func TestVerifyEvidenceSignatures_ForgedByDifferentKey(t *testing.T) {
	// Real validator key
	realSK, err := bls.NewSecretKey()
	require.NoError(t, err)
	realPK := bls.PublicFromSecretKey(realSK)

	// Forger key
	forgerSK, err := bls.NewSecretKey()
	require.NoError(t, err)

	msgA := []byte("block-A-height-100")
	msgB := []byte("block-B-height-100")

	// Forger signs with their own key
	ev := &txs.SlashEvidence{
		Height:     100,
		Type:       txs.DoubleVoteEvidence,
		MessageA:   msgA,
		SignatureA: bls.SignatureToBytes(bls.Sign(forgerSK, msgA)),
		MessageB:   msgB,
		SignatureB: bls.SignatureToBytes(bls.Sign(forgerSK, msgB)),
	}

	// Must fail: signatures don't verify against the real validator's key
	err = verifyEvidenceSignatures(realPK, ev)
	require.ErrorIs(t, err, errInvalidEvidenceSignature)
}

// TestVerifyEvidenceSignatures_PartialForge_SigAReal_SigBForged verifies
// that even if one signature is valid, the other being forged causes rejection.
func TestVerifyEvidenceSignatures_PartialForge_SigAReal_SigBForged(t *testing.T) {
	realSK, err := bls.NewSecretKey()
	require.NoError(t, err)
	realPK := bls.PublicFromSecretKey(realSK)

	forgerSK, err := bls.NewSecretKey()
	require.NoError(t, err)

	msgA := []byte("legitimate-block-A")
	msgB := []byte("forged-block-B")

	ev := &txs.SlashEvidence{
		Height:     100,
		Type:       txs.DoubleVoteEvidence,
		MessageA:   msgA,
		SignatureA: bls.SignatureToBytes(bls.Sign(realSK, msgA)),  // valid
		MessageB:   msgB,
		SignatureB: bls.SignatureToBytes(bls.Sign(forgerSK, msgB)), // forged
	}

	err = verifyEvidenceSignatures(realPK, ev)
	require.ErrorIs(t, err, errInvalidEvidenceSignature)
}

// TestVerifyEvidenceSignatures_ZeroLengthSig verifies that a zero-length
// signature fails parsing.
func TestVerifyEvidenceSignatures_ZeroLengthSig(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk := bls.PublicFromSecretKey(sk)

	msgA := []byte("msg-A")
	msgB := []byte("msg-B")

	ev := &txs.SlashEvidence{
		Height:     100,
		Type:       txs.DoubleVoteEvidence,
		MessageA:   msgA,
		SignatureA: []byte{}, // empty
		MessageB:   msgB,
		SignatureB: bls.SignatureToBytes(bls.Sign(sk, msgB)),
	}

	err = verifyEvidenceSignatures(pk, ev)
	require.ErrorIs(t, err, errInvalidEvidenceSignature)
}

// TestVerifyEvidenceSignatures_CorruptedSignatureB verifies that corrupted
// bytes in signature B are caught.
func TestVerifyEvidenceSignatures_CorruptedSignatureB(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk := bls.PublicFromSecretKey(sk)

	msgA := []byte("block-A")
	msgB := []byte("block-B")
	sigA := bls.Sign(sk, msgA)
	sigB := bls.Sign(sk, msgB)

	// Corrupt signature B
	corruptedB := make([]byte, bls.SignatureLen)
	copy(corruptedB, bls.SignatureToBytes(sigB))
	corruptedB[len(corruptedB)-1] ^= 0xFF

	ev := &txs.SlashEvidence{
		Height:     100,
		Type:       txs.DoubleVoteEvidence,
		MessageA:   msgA,
		SignatureA: bls.SignatureToBytes(sigA),
		MessageB:   msgB,
		SignatureB: corruptedB,
	}

	err = verifyEvidenceSignatures(pk, ev)
	require.ErrorIs(t, err, errInvalidEvidenceSignature)
}

// TestSlashPercentMismatch verifies the executor constants are consistent:
// DoubleVote gets 10%, DoubleSign gets 50%.
func TestSlashPercentMismatch(t *testing.T) {
	require.Equal(t, uint32(100_000), uint32(DoubleVoteSlashPercent))
	require.Equal(t, uint32(500_000), uint32(DoubleSignSlashPercent))
	require.Less(t, uint32(DoubleVoteSlashPercent), uint32(DoubleSignSlashPercent),
		"double-sign must be penalized more severely than double-vote")
}
