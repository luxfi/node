// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"errors"

	luxbls "github.com/luxfi/crypto/bls"
)

const SignatureLen = 96

var (
	ErrFailedSignatureDecompress  = errors.New("couldn't decompress signature")
	errInvalidSignature           = errors.New("invalid signature")
	errNoSignatures               = errors.New("no signatures")
	errFailedSignatureAggregation = errors.New("couldn't aggregate signatures")
)

type Signature struct {
	sig *luxbls.Signature
}

type AggregateSignature struct {
	sigs []*luxbls.Signature
}

// SignatureToBytes returns the compressed big-endian format of the signature.
func SignatureToBytes(sig *Signature) []byte {
	if sig == nil || sig.sig == nil {
		return make([]byte, SignatureLen)
	}
	return luxbls.SignatureToBytes(sig.sig)
}

// SignatureFromBytes parses the compressed big-endian format of the signature
// into a signature.
func SignatureFromBytes(sigBytes []byte) (*Signature, error) {
	sig, err := luxbls.SignatureFromBytes(sigBytes)
	if err != nil {
		return nil, ErrFailedSignatureDecompress
	}
	return &Signature{sig: sig}, nil
}

// AggregateSignatures aggregates a non-zero number of signatures into a single
// aggregated signature.
// Invariant: all [sigs] have been validated.
func AggregateSignatures(sigs []*Signature) (*Signature, error) {
	if len(sigs) == 0 {
		return nil, errNoSignatures
	}

	luxSigs := make([]*luxbls.Signature, len(sigs))
	for i, sig := range sigs {
		if sig == nil || sig.sig == nil {
			return nil, errInvalidSignature
		}
		luxSigs[i] = sig.sig
	}

	aggSig, err := luxbls.AggregateSignatures(luxSigs)
	if err != nil {
		return nil, errFailedSignatureAggregation
	}
	return &Signature{sig: aggSig}, nil
}

// Methods for blst compatibility

func (sig *Signature) SigValidate(dummy bool) bool {
	return sig != nil && sig.sig != nil
}

func (sig *Signature) Compress() []byte {
	return SignatureToBytes(sig)
}

func (sig *Signature) Uncompress(data []byte) *Signature {
	newSig, err := SignatureFromBytes(data)
	if err != nil {
		return nil
	}
	return newSig
}

func (sig *Signature) Sign(sk *SecretKey, msg []byte, dst []byte) *Signature {
	if sk == nil || sk.sk == nil {
		return nil
	}
	// The dst parameter is the ciphersuite, which is handled internally by luxfi/crypto
	return &Signature{sig: sk.sk.Sign(msg)}
}

func (sig *Signature) Verify(groupcheck bool, pk *PublicKey, pkValidate bool, msg []byte, dst []byte) bool {
	if sig == nil || sig.sig == nil || pk == nil || pk.pk == nil {
		return false
	}
	// The dst parameter and validation flags are handled internally by luxfi/crypto
	return luxbls.Verify(pk.pk, sig.sig, msg)
}

// AggregateSignature methods

func (as *AggregateSignature) Aggregate(sigs []*Signature, groupcheck bool) bool {
	as.sigs = make([]*luxbls.Signature, 0, len(sigs))
	for _, sig := range sigs {
		if sig == nil || sig.sig == nil {
			return false
		}
		as.sigs = append(as.sigs, sig.sig)
	}
	return true
}

func (as *AggregateSignature) ToAffine() *Signature {
	if len(as.sigs) == 0 {
		return nil
	}
	aggSig, err := luxbls.AggregateSignatures(as.sigs)
	if err != nil {
		return nil
	}
	return &Signature{sig: aggSig}
}