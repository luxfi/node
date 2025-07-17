// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"

	"github.com/ava-labs/libevm/crypto"
	"go.uber.org/zap"

	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/crypto/bls"
)

// SignatureVerifier verifies threshold signatures
type SignatureVerifier struct {
	threshold int
	log       logging.Logger
}

// NewSignatureVerifier creates a new signature verifier
func NewSignatureVerifier(threshold int, log logging.Logger) *SignatureVerifier {
	return &SignatureVerifier{
		threshold: threshold,
		log:       log,
	}
}

// VerifyThresholdSignature verifies threshold ECDSA signatures (CGGMP21 style)
func (sv *SignatureVerifier) VerifyThresholdSignature(
	message []byte,
	signatures [][]byte,
	signerIDs []string,
) error {
	if len(signatures) < sv.threshold {
		return errors.New("insufficient signatures")
	}
	
	if len(signatures) != len(signerIDs) {
		return errors.New("signatures and signer IDs mismatch")
	}
	
	// Hash the message
	messageHash := sha256.Sum256(message)
	
	// For CGGMP21, we would aggregate partial signatures
	// This is a simplified version - in production, use proper threshold ECDSA
	validSigs := 0
	for i, sig := range signatures {
		if sv.verifyECDSASignature(messageHash[:], sig, signerIDs[i]) {
			validSigs++
		}
	}
	
	if validSigs < sv.threshold {
		return errors.New("insufficient valid signatures")
	}
	
	return nil
}

// verifyECDSASignature verifies a single ECDSA signature
func (sv *SignatureVerifier) verifyECDSASignature(hash []byte, signature []byte, signerID string) bool {
	if len(signature) != 65 {
		return false
	}
	
	// Recover public key from signature
	pubKey, err := crypto.SigToPub(hash, signature)
	if err != nil {
		sv.log.Debug("Failed to recover public key",
			zap.Error(err),
			zap.String("signerID", signerID),
		)
		return false
	}
	
	// Verify signature
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	
	return ecdsa.Verify(pubKey, hash, r, s)
}

// VerifyBLSAggregateSignature verifies BLS aggregate signatures
func (sv *SignatureVerifier) VerifyBLSAggregateSignature(
	message []byte,
	aggregateSignature []byte,
	publicKeys [][]byte,
) error {
	if len(publicKeys) < sv.threshold {
		return errors.New("insufficient public keys")
	}
	
	// Parse aggregate signature
	aggSig, err := bls.SignatureFromBytes(aggregateSignature)
	if err != nil {
		return err
	}
	
	// Parse and aggregate public keys
	var pubKeys []*bls.PublicKey
	for _, pkBytes := range publicKeys {
		pk, err := bls.PublicKeyFromBytes(pkBytes)
		if err != nil {
			return err
		}
		pubKeys = append(pubKeys, pk)
	}
	
	// Aggregate public keys
	aggPubKey, err := bls.AggregatePublicKeys(pubKeys)
	if err != nil {
		return err
	}
	
	// Verify aggregate signature
	return bls.Verify(aggPubKey, aggSig, message)
}

// VerifyTEESignature verifies TEE attestation signatures
func (sv *SignatureVerifier) VerifyTEESignature(
	quote []byte,
	signature []byte,
	trustedKeys []string,
) error {
	// In production, this would verify Intel SGX quotes or similar
	// For now, we simulate by checking against trusted keys
	
	quoteHash := sha256.Sum256(quote)
	
	for _, trustedKey := range trustedKeys {
		// Parse trusted public key
		pubKeyBytes, err := hex.DecodeString(trustedKey)
		if err != nil {
			continue
		}
		
		pubKey, err := crypto.UnmarshalPubkey(pubKeyBytes)
		if err != nil {
			continue
		}
		
		// Verify signature
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:64])
		
		if ecdsa.Verify(pubKey, quoteHash[:], r, s) {
			return nil
		}
	}
	
	return errors.New("TEE signature verification failed")
}