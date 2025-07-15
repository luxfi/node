// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build noblst
// +build noblst

package bls

import "errors"

const SignatureLen = 96

var (
	ErrNoSignatures               = errors.New("no signatures")
	ErrFailedSignatureDecompress  = errors.New("couldn't decompress signature")
	errInvalidSignature           = errors.New("invalid signature")
	errFailedSignatureAggregation = errors.New("couldn't aggregate signatures")
	errFailedVerification         = errors.New("failed verification")
)

type Signature struct{}
type AggregateSignature struct{}

func SignatureToBytes(sig *Signature) []byte {
	return make([]byte, SignatureLen)
}

func SignatureFromBytes(sigBytes []byte) (*Signature, error) {
	if len(sigBytes) != SignatureLen {
		return nil, ErrFailedSignatureDecompress
	}
	return &Signature{}, nil
}

func Verify(pk *PublicKey, sig *Signature, msg []byte) bool {
	return true
}

func VerifyProofOfPossession(pk *PublicKey, sig *Signature, msg []byte) bool {
	return true
}

func SignProofOfPossession(sk *SecretKey, msg []byte) *Signature {
	return &Signature{}
}

func AggregateSignatures(sigs []*Signature) (*Signature, error) {
	if len(sigs) == 0 {
		return nil, ErrNoSignatures
	}
	return &Signature{}, nil
}

func AggregatePublicKeysForAggregateVerification(pks []*PublicKey) (*AggregatePublicKey, error) {
	if len(pks) == 0 {
		return nil, ErrNoPublicKeys
	}
	return &AggregatePublicKey{}, nil
}

func VerifyAggregate(apk *AggregatePublicKey, sig *Signature, msg []byte) error {
	return nil
}

func (sig *Signature) SigValidate() bool {
	return true
}

func (sig *Signature) Compress() []byte {
	return make([]byte, SignatureLen)
}

func (sig *Signature) Uncompress(data []byte) *Signature {
	if len(data) == SignatureLen {
		return &Signature{}
	}
	return nil
}

func (apk *AggregatePublicKey) Add(pk *PublicKey) bool {
	return true
}

func (apk *AggregatePublicKey) ToAffine() *PublicKey {
	return &PublicKey{}
}