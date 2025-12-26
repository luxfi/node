// Package bls provides BLS cryptographic functions.
// This is a proxy package that re-exports from the main crypto/bls package.
package bls

import (
	bls "github.com/luxfi/crypto/bls"
)

// Constants re-exported from the main bls package
const (
	SignatureLen = bls.SignatureLen
	PublicKeyLen = bls.PublicKeyLen
	SecretKeyLen = bls.SecretKeyLen
)

// Re-export types from the main bls package
type (
	PublicKey  = bls.PublicKey
	SecretKey  = bls.SecretKey
	Signature  = bls.Signature
	Signer     = bls.Signer
)

// Re-export functions
var (
	NewSecretKey                     = bls.NewSecretKey
	SecretKeyFromBytes               = bls.SecretKeyFromBytes
	SecretKeyToBytes                 = bls.SecretKeyToBytes
	SecretKeyFromSeed                = bls.SecretKeyFromSeed
	PublicKeyToCompressedBytes       = bls.PublicKeyToCompressedBytes
	PublicKeyFromCompressedBytes     = bls.PublicKeyFromCompressedBytes
	PublicKeyToUncompressedBytes     = bls.PublicKeyToUncompressedBytes
	PublicKeyFromValidUncompressedBytes = bls.PublicKeyFromValidUncompressedBytes
	AggregatePublicKeys              = bls.AggregatePublicKeys
	AggregateSignatures              = bls.AggregateSignatures
	Verify                           = bls.Verify
	VerifyProofOfPossession          = bls.VerifyProofOfPossession
	SignatureToBytes                 = bls.SignatureToBytes
	SignatureFromBytes               = bls.SignatureFromBytes
)
