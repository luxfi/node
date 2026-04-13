// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"errors"

	"github.com/luxfi/crypto/mldsa"
)

var (
	ErrUnsupportedAlgorithm     = errors.New("staking: cannot verify signature: unsupported algorithm")
	ErrECDSAVerificationFailure = errors.New("staking: ECDSA verification failure")
)

// MLDSAPublicKey wraps an ML-DSA public key to satisfy the crypto.PublicKey interface
type MLDSAPublicKey struct {
	Key  *mldsa.PublicKey
	Mode mldsa.Mode
}

// CheckSignature verifies that the signature is a valid signature over signed
// from the certificate.
//
// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L793-L797
// Ref: https://github.com/golang/go/blob/go1.19.12/src/crypto/x509/x509.go#L816-L879
func CheckSignature(cert *Certificate, msg []byte, signature []byte) error {
	hasher := crypto.SHA256.New()
	_, err := hasher.Write(msg)
	if err != nil {
		return err
	}
	hashed := hasher.Sum(nil)

	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return rsa.VerifyPKCS1v15(pub, crypto.SHA256, hashed, signature)
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(pub, hashed, signature) {
			return ErrECDSAVerificationFailure
		}
		return nil
	case *MLDSAPublicKey:
		// ML-DSA uses the raw message, not the hash
		if !pub.Key.VerifySignature(msg, signature) {
			return ErrMLDSAVerificationFailure
		}
		return nil
	default:
		return ErrUnsupportedAlgorithm
	}
}

// CheckHybridSignature verifies both classical (ECDSA/RSA) and post-quantum (ML-DSA) signatures.
// This provides defense-in-depth: if either algorithm is broken, the other still protects.
func CheckHybridSignature(cert *Certificate, pqPubKey []byte, msg []byte, classicalSig, pqSig []byte) error {
	// Verify classical signature
	if err := CheckSignature(cert, msg, classicalSig); err != nil {
		return err
	}
	// Verify ML-DSA signature
	return VerifyPQSignature(pqPubKey, msg, pqSig)
}
