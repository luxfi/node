// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build noblst
// +build noblst

package bls

import "errors"

const PublicKeyLen = 48

var (
	ErrNoPublicKeys               = errors.New("no public keys")
	ErrFailedPublicKeyDecompress  = errors.New("couldn't decompress public key")
	errInvalidPublicKey           = errors.New("invalid public key")
	errFailedPublicKeyAggregation = errors.New("couldn't aggregate public keys")
)

type PublicKey struct{}
type AggregatePublicKey struct{}

func PublicKeyToCompressedBytes(pk *PublicKey) []byte {
	return make([]byte, PublicKeyLen)
}

func PublicKeyFromCompressedBytes(pkBytes []byte) (*PublicKey, error) {
	if len(pkBytes) != PublicKeyLen {
		return nil, ErrFailedPublicKeyDecompress
	}
	return &PublicKey{}, nil
}

func PublicKeyToUncompressedBytes(key *PublicKey) []byte {
	return make([]byte, PublicKeyLen*2)
}

func PublicKeyFromValidUncompressedBytes(pkBytes []byte) *PublicKey {
	if len(pkBytes) != PublicKeyLen*2 {
		return nil
	}
	return &PublicKey{}
}

func AggregatePublicKeys(pks []*PublicKey) (*PublicKey, error) {
	if len(pks) == 0 {
		return nil, ErrNoPublicKeys
	}
	return &PublicKey{}, nil
}

func (pk *PublicKey) KeyValidate() bool {
	return true
}

func (pk *PublicKey) Serialize() []byte {
	return make([]byte, PublicKeyLen*2)
}

func (pk *PublicKey) Compress() []byte {
	return make([]byte, PublicKeyLen)
}

func (pk *PublicKey) Uncompress(data []byte) *PublicKey {
	if len(data) == PublicKeyLen {
		return &PublicKey{}
	}
	return nil
}
