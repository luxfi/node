// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"errors"

	luxbls "github.com/luxfi/crypto/bls"
)

const PublicKeyLen = 48

var (
	ErrNoPublicKeys               = errors.New("no public keys")
	ErrFailedPublicKeyDecompress  = errors.New("couldn't decompress public key")
	errInvalidPublicKey           = errors.New("invalid public key")
	errFailedPublicKeyAggregation = errors.New("couldn't aggregate public keys")
)

type PublicKey struct {
	pk *luxbls.PublicKey
}

type AggregatePublicKey struct {
	pks []*luxbls.PublicKey
}

// PublicKeyToCompressedBytes returns the compressed big-endian format of the
// public key.
func PublicKeyToCompressedBytes(pk *PublicKey) []byte {
	if pk == nil || pk.pk == nil {
		return make([]byte, PublicKeyLen)
	}
	return luxbls.PublicKeyToCompressedBytes(pk.pk)
}

// PublicKeyFromCompressedBytes parses the compressed big-endian format of the
// public key into a public key.
func PublicKeyFromCompressedBytes(pkBytes []byte) (*PublicKey, error) {
	pk, err := luxbls.PublicKeyFromCompressedBytes(pkBytes)
	if err != nil {
		return nil, ErrFailedPublicKeyDecompress
	}
	return &PublicKey{pk: pk}, nil
}

// PublicKeyToUncompressedBytes returns the uncompressed big-endian format of
// the public key.
func PublicKeyToUncompressedBytes(key *PublicKey) []byte {
	if key == nil || key.pk == nil {
		return make([]byte, PublicKeyLen*2)
	}
	return luxbls.PublicKeyToUncompressedBytes(key.pk)
}

// PublicKeyFromValidUncompressedBytes parses the uncompressed big-endian format
// of the public key into a public key. It is assumed that the provided bytes
// are valid.
func PublicKeyFromValidUncompressedBytes(pkBytes []byte) *PublicKey {
	pk := luxbls.PublicKeyFromValidUncompressedBytes(pkBytes)
	if pk == nil {
		return nil
	}
	return &PublicKey{pk: pk}
}

// AggregatePublicKeys aggregates a non-zero number of public keys into a single
// aggregated public key.
// Invariant: all [pks] have been validated.
func AggregatePublicKeys(pks []*PublicKey) (*PublicKey, error) {
	if len(pks) == 0 {
		return nil, ErrNoPublicKeys
	}

	luxPks := make([]*luxbls.PublicKey, len(pks))
	for i, pk := range pks {
		if pk == nil || pk.pk == nil {
			return nil, errInvalidPublicKey
		}
		luxPks[i] = pk.pk
	}

	aggPk, err := luxbls.AggregatePublicKeys(luxPks)
	if err != nil {
		return nil, errFailedPublicKeyAggregation
	}
	return &PublicKey{pk: aggPk}, nil
}

// Verify the [sig] of [msg] against the [pk].
// The [sig] and [pk] may have been an aggregation of other signatures and keys.
// Invariant: [pk] and [sig] have both been validated.
func Verify(pk *PublicKey, sig *Signature, msg []byte) bool {
	if pk == nil || pk.pk == nil || sig == nil || sig.sig == nil {
		return false
	}
	return luxbls.Verify(pk.pk, sig.sig, msg)
}

// Verify the possession of the secret pre-image of [sk] by verifying a [sig] of
// [msg] against the [pk].
// The [sig] and [pk] may have been an aggregation of other signatures and keys.
// Invariant: [pk] and [sig] have both been validated.
func VerifyProofOfPossession(pk *PublicKey, sig *Signature, msg []byte) bool {
	if pk == nil || pk.pk == nil || sig == nil || sig.sig == nil {
		return false
	}
	return luxbls.VerifyProofOfPossession(pk.pk, sig.sig, msg)
}

// Methods for blst compatibility

func (pk *PublicKey) KeyValidate() bool {
	return pk != nil && pk.pk != nil
}

func (pk *PublicKey) Serialize() []byte {
	return PublicKeyToUncompressedBytes(pk)
}

func (pk *PublicKey) Compress() []byte {
	return PublicKeyToCompressedBytes(pk)
}

func (pk *PublicKey) Uncompress(data []byte) *PublicKey {
	newPk, err := PublicKeyFromCompressedBytes(data)
	if err != nil {
		return nil
	}
	return newPk
}

func (pk *PublicKey) Deserialize(data []byte) *PublicKey {
	return PublicKeyFromValidUncompressedBytes(data)
}

func (pk *PublicKey) From(sk *SecretKey) *PublicKey {
	if sk == nil || sk.sk == nil {
		return nil
	}
	return &PublicKey{pk: sk.sk.PublicKey()}
}

// AggregatePublicKey methods

func (apk *AggregatePublicKey) Aggregate(pks []*PublicKey, groupcheck bool) bool {
	apk.pks = make([]*luxbls.PublicKey, 0, len(pks))
	for _, pk := range pks {
		if pk == nil || pk.pk == nil {
			return false
		}
		apk.pks = append(apk.pks, pk.pk)
	}
	return true
}

func (apk *AggregatePublicKey) ToAffine() *PublicKey {
	if len(apk.pks) == 0 {
		return nil
	}
	aggPk, err := luxbls.AggregatePublicKeys(apk.pks)
	if err != nil {
		return nil
	}
	return &PublicKey{pk: aggPk}
}