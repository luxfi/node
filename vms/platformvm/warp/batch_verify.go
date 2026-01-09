// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/lux/accel"
)

// BatchVerifyBLSSignatures verifies multiple BLS signatures using GPU acceleration
// when available. Falls back to sequential CPU verification if GPU is unavailable.
//
// Parameters:
//   - publicKeys: Slice of BLS public keys (48 bytes each, compressed G1 points)
//   - messages: Slice of messages to verify
//   - signatures: Slice of BLS signatures (96 bytes each, G2 points)
//
// Returns:
//   - results: Slice of booleans indicating validity of each signature
//   - error: Error if batch verification setup fails
func BatchVerifyBLSSignatures(publicKeys []*bls.PublicKey, messages [][]byte, signatures []*bls.Signature) ([]bool, error) {
	n := len(publicKeys)
	if n == 0 {
		return nil, nil
	}
	if n != len(messages) || n != len(signatures) {
		return nil, ErrInvalidSignature
	}

	// Try GPU-accelerated batch verification
	if accel.Available() {
		session, err := accel.DefaultSession()
		if err == nil {
			// Convert to byte slices for GPU
			pkBytes := make([][]byte, n)
			sigBytes := make([][]byte, n)
			for i := range publicKeys {
				pkBytes[i] = bls.PublicKeyToCompressedBytes(publicKeys[i])
				sigBytes[i] = bls.SignatureToBytes(signatures[i])
			}

			// Use GPU batch verification
			results, err := batchVerifyWithSession(session, pkBytes, messages, sigBytes)
			if err == nil {
				return results, nil
			}
			// Fall through to CPU on GPU error
		}
	}

	// CPU fallback: verify sequentially
	return batchVerifyCPU(publicKeys, messages, signatures), nil
}

// batchVerifyWithSession performs GPU-accelerated batch verification.
func batchVerifyWithSession(session *accel.Session, publicKeys, messages, signatures [][]byte) ([]bool, error) {
	n := len(publicKeys)

	// Calculate maximum message length for padding
	maxMsgLen := 0
	for _, msg := range messages {
		if len(msg) > maxMsgLen {
			maxMsgLen = len(msg)
		}
	}

	// Create tensors for GPU
	pkTensor, err := accel.NewTensorWithData[byte](session, []int{n, bls.PublicKeyLen}, flattenBytes(publicKeys, bls.PublicKeyLen))
	if err != nil {
		return nil, err
	}
	defer pkTensor.Close()

	sigTensor, err := accel.NewTensorWithData[byte](session, []int{n, bls.SignatureLen}, flattenBytes(signatures, bls.SignatureLen))
	if err != nil {
		return nil, err
	}
	defer sigTensor.Close()

	// Pad messages to uniform length
	msgTensor, err := accel.NewTensorWithData[byte](session, []int{n, maxMsgLen}, flattenBytesWithPadding(messages, maxMsgLen))
	if err != nil {
		return nil, err
	}
	defer msgTensor.Close()

	// Create results tensor
	resultTensor, err := accel.NewTensor[byte](session, []int{n})
	if err != nil {
		return nil, err
	}
	defer resultTensor.Close()

	// Execute batch verification
	crypto := session.Crypto()
	err = crypto.BLSVerifyBatch(msgTensor.Untyped(), sigTensor.Untyped(), pkTensor.Untyped(), resultTensor.Untyped())
	if err != nil {
		return nil, err
	}

	// Sync and read results
	if err := session.Sync(); err != nil {
		return nil, err
	}

	resultBytes, err := resultTensor.ToSlice()
	if err != nil {
		return nil, err
	}

	// Convert to bool slice
	results := make([]bool, n)
	for i, r := range resultBytes {
		results[i] = r == 1
	}

	return results, nil
}

// batchVerifyCPU performs sequential CPU verification as fallback.
func batchVerifyCPU(publicKeys []*bls.PublicKey, messages [][]byte, signatures []*bls.Signature) []bool {
	results := make([]bool, len(publicKeys))
	for i := range publicKeys {
		results[i] = bls.Verify(publicKeys[i], signatures[i], messages[i])
	}
	return results
}

// flattenBytes converts a slice of byte slices to a flat byte slice with fixed element size.
func flattenBytes(data [][]byte, elemSize int) []byte {
	result := make([]byte, len(data)*elemSize)
	for i, d := range data {
		copy(result[i*elemSize:], d)
	}
	return result
}

// flattenBytesWithPadding converts variable-length byte slices to fixed-size with padding.
func flattenBytesWithPadding(data [][]byte, elemSize int) []byte {
	result := make([]byte, len(data)*elemSize)
	for i, d := range data {
		copy(result[i*elemSize:], d)
		// Remaining bytes are zero-padded
	}
	return result
}

// VerifyBLSAggregateWithGPU verifies an aggregate BLS signature with GPU acceleration.
// This is used by BitSetSignature.Verify() for high-throughput scenarios.
func VerifyBLSAggregateWithGPU(aggPubKey *bls.PublicKey, aggSig *bls.Signature, message []byte) bool {
	// For single signature verification, GPU overhead isn't worth it
	// Use standard CPU verification
	return bls.Verify(aggPubKey, aggSig, message)
}

// BatchVerifyAggregateSignatures verifies multiple aggregate signatures in parallel.
// Each signature is an aggregated BLS signature over the same message by different key sets.
func BatchVerifyAggregateSignatures(aggPubKeys []*bls.PublicKey, aggSigs []*bls.Signature, messages [][]byte) ([]bool, error) {
	return BatchVerifyBLSSignatures(aggPubKeys, messages, aggSigs)
}
