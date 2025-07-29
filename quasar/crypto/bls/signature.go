// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"github.com/luxfi/crypto/bls"
)

// Re-export types from the crypto package
type (
	PublicKey          = bls.PublicKey
	Signature          = bls.Signature
	Signer             = bls.Signer
)

// Re-export key functions
var (
	PublicKeyFromCompressedBytes = bls.PublicKeyFromCompressedBytes
	SignatureFromBytes           = bls.SignatureFromBytes
	AggregatePublicKeys          = bls.AggregatePublicKeys
	AggregateSignatures          = bls.AggregateSignatures
	Verify                       = bls.Verify
	VerifyProofOfPossession      = bls.VerifyProofOfPossession
	PublicKeyToCompressedBytes   = bls.PublicKeyToCompressedBytes
	PublicKeyToUncompressedBytes = bls.PublicKeyToUncompressedBytes
	SignatureToBytes             = bls.SignatureToBytes
)