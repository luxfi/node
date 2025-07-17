// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build noblst
// +build noblst

package bls

import (
	"crypto/rand"
	"errors"
)

const SecretKeyLen = 32

var (
	ErrFailedSecretKeyDecompress = errors.New("couldn't decompress secret key")
	errInvalidSecretKey          = errors.New("invalid secret key")
)

type SecretKey struct {
	bytes [SecretKeyLen]byte
}

func NewSecretKey() (*SecretKey, error) {
	sk := &SecretKey{}
	_, err := rand.Read(sk.bytes[:])
	if err != nil {
		return nil, err
	}
	return sk, nil
}

func SecretKeyToBytes(sk *SecretKey) []byte {
	return sk.bytes[:]
}

func SecretKeyFromBytes(skBytes []byte) (*SecretKey, error) {
	if len(skBytes) != SecretKeyLen {
		return nil, ErrFailedSecretKeyDecompress
	}
	sk := &SecretKey{}
	copy(sk.bytes[:], skBytes)
	return sk, nil
}

func (sk *SecretKey) PublicKey() *PublicKey {
	return &PublicKey{}
}

func (sk *SecretKey) Sign(msg []byte) *Signature {
	return &Signature{}
}

func Sign(sk *SecretKey, msg []byte) *Signature {
	return &Signature{}
}

func (sk *SecretKey) KeyGen(ikm []byte, salt []byte, info []byte) {
	// Stub implementation
}

func (sk *SecretKey) Serialize() []byte {
	return sk.bytes[:]
}

func PublicFromSecretKey(sk *SecretKey) *PublicKey {
	return &PublicKey{}
}