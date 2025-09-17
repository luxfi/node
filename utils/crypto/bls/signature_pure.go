// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build noblst || purego
// +build noblst purego

package bls

import (
	luxbls "github.com/luxfi/crypto/bls"
)

const SignatureLen = luxbls.SignatureLen

var (
	ErrFailedSignatureDecompress  = luxbls.ErrFailedSignatureDecompress
	ErrInvalidSignature           = luxbls.ErrInvalidSignature
	ErrNoSignatures               = luxbls.ErrNoSignatures
	ErrFailedSignatureAggregation = luxbls.ErrFailedSignatureAggregation
)

type Signature = luxbls.Signature

// SignatureToBytes returns the compressed big-endian format of the signature.
func SignatureToBytes(sig *Signature) []byte {
	return sig.Compress()
}

// SignatureFromBytes parses the compressed big-endian format of the signature
// into a signature.
func SignatureFromBytes(sigBytes []byte) (*Signature, error) {
	return luxbls.SignatureFromCompressedBytes(sigBytes)
}

// Verify the signature against the provided message and public key.
func Verify(pk *PublicKey, sig *Signature, msg []byte) bool {
	return luxbls.Verify(pk, sig, msg)
}

// VerifyProofOfPossession verifies that signature is a valid proof of
// possession of the provided public key.
func VerifyProofOfPossession(pk *PublicKey, sig *Signature) bool {
	// In the pure Go version, we can use the public key bytes as the message
	return Verify(pk, sig, PublicKeyToCompressedBytes(pk))
}

// AggregateSignatures aggregates a non-zero number of signatures into a single
// aggregated signature.
func AggregateSignatures(sigs []*Signature) (*Signature, error) {
	if len(sigs) == 0 {
		return nil, ErrNoSignatures
	}
	return luxbls.AggregateSignatures(sigs)
}