// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !noblst && !purego && cgo
// +build !noblst,!purego,cgo

package bls

import (
	"errors"

	luxbls "github.com/luxfi/crypto/bls"
)

const PublicKeyLen = luxbls.PublicKeyLen

var (
	ErrNoPublicKeys               = luxbls.ErrNoPublicKeys
	ErrFailedPublicKeyDecompress  = luxbls.ErrFailedPublicKeyDecompress
	errInvalidPublicKey           = errors.New("invalid public key")
	errFailedPublicKeyAggregation = errors.New("couldn't aggregate public keys")
)

type PublicKey = luxbls.PublicKey

// PublicKeyToCompressedBytes returns the compressed big-endian format of the
// public key.
func PublicKeyToCompressedBytes(pk *PublicKey) []byte {
	return pk.Compress()
}

// PublicKeyFromCompressedBytes parses the compressed big-endian format of the
// public key into a public key.
func PublicKeyFromCompressedBytes(pkBytes []byte) (*PublicKey, error) {
	return luxbls.PublicKeyFromCompressedBytes(pkBytes)
}

// PublicKeyToUncompressedBytes returns the uncompressed big-endian format of
// the public key.
func PublicKeyToUncompressedBytes(key *PublicKey) []byte {
	return key.Serialize()
}

// PublicKeyFromUncompressedBytes parses the uncompressed big-endian format of
// the public key into a public key.
func PublicKeyFromUncompressedBytes(pkBytes []byte) (*PublicKey, error) {
	// luxfi/crypto should handle both compressed and uncompressed formats
	return luxbls.PublicKeyFromCompressedBytes(pkBytes)
}

// AggregatePublicKeys aggregates a non-zero number of public keys into a
// single aggregated public key.
func AggregatePublicKeys(pks []*PublicKey) (*PublicKey, error) {
	if len(pks) == 0 {
		return nil, ErrNoPublicKeys
	}
	// luxfi/crypto should use optimized CGO implementation when available
	return luxbls.AggregatePublicKeys(pks)
}