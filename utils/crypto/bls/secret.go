// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"errors"

	luxbls "github.com/luxfi/crypto/bls"
)

const SecretKeyLen = 32

var (
	ErrFailedSecretKeyDecompress = errors.New("couldn't decompress secret key")
	errInvalidSecretKey          = errors.New("invalid secret key")
)

type SecretKey struct {
	sk *luxbls.SecretKey
}

// NewSecretKey generates a new secret key from the local source of
// cryptographically secure randomness.
func NewSecretKey() (*SecretKey, error) {
	sk, err := luxbls.NewSecretKey()
	if err != nil {
		return nil, err
	}
	return &SecretKey{sk: sk}, nil
}

// SecretKeyToBytes returns the big-endian format of the secret key.
func SecretKeyToBytes(sk *SecretKey) []byte {
	if sk == nil || sk.sk == nil {
		return make([]byte, SecretKeyLen)
	}
	return luxbls.SecretKeyToBytes(sk.sk)
}

// SecretKeyFromBytes parses the big-endian format of the secret key into a
// secret key.
func SecretKeyFromBytes(skBytes []byte) (*SecretKey, error) {
	sk, err := luxbls.SecretKeyFromBytes(skBytes)
	if err != nil {
		return nil, ErrFailedSecretKeyDecompress
	}
	return &SecretKey{sk: sk}, nil
}

// PublicKey returns the public key that corresponds to this secret key.
func (sk *SecretKey) PublicKey() *PublicKey {
	if sk == nil || sk.sk == nil {
		return nil
	}
	return &PublicKey{pk: sk.sk.PublicKey()}
}

// Sign [msg] to authorize this message from this [sk].
func (sk *SecretKey) Sign(msg []byte) *Signature {
	if sk == nil || sk.sk == nil {
		return nil
	}
	return &Signature{sig: sk.sk.Sign(msg)}
}

// SignProofOfPossession signs [msg] to prove the ownership of this [sk].
func (sk *SecretKey) SignProofOfPossession(msg []byte) *Signature {
	if sk == nil || sk.sk == nil {
		return nil
	}
	return &Signature{sig: sk.sk.SignProofOfPossession(msg)}
}

// PublicFromSecretKey returns the public key that corresponds to this secret
// key.
func PublicFromSecretKey(sk *SecretKey) *PublicKey {
	if sk == nil {
		return nil
	}
	return sk.PublicKey()
}

// Sign [msg] to authorize this message from this [sk].
func Sign(sk *SecretKey, msg []byte) *Signature {
	if sk == nil {
		return nil
	}
	return sk.Sign(msg)
}

// SignProofOfPossession signs [msg] to prove the ownership of this [sk].
func SignProofOfPossession(sk *SecretKey, msg []byte) *Signature {
	if sk == nil {
		return nil
	}
	return sk.SignProofOfPossession(msg)
}

// Methods for blst compatibility

func (sk *SecretKey) Serialize() []byte {
	return SecretKeyToBytes(sk)
}

func (sk *SecretKey) Deserialize(data []byte) *SecretKey {
	newSk, err := SecretKeyFromBytes(data)
	if err != nil {
		return nil
	}
	return newSk
}

func (sk *SecretKey) Zeroize() {
	// The luxfi/crypto implementation should handle secure cleanup
	// Nothing to do here as the underlying implementation handles it
}

func (sk *SecretKey) KeyGen(ikm []byte, salt []byte, info []byte) {
	// For compatibility - the luxfi/crypto implementation handles key generation differently
	// This is a no-op since key generation is done via NewSecretKey or SecretKeyFromBytes
}