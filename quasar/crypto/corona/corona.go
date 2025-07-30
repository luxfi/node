// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corona

import (
	"crypto/sha256"
	"errors"

	"github.com/luxfi/corona"
)

// Constants - estimated sizes based on Corona implementation
const (
	SKSize      = 96    // Secret key size (estimated)
	PKSize      = 192   // Public key size (estimated)
	ShareSize   = 430   // Share size (≈ 430 B as per docs)
	CertSize    = 3072  // Certificate size (≈ 3 kB as per docs)
	PrecompSize = 40960 // Precomputation size (32-40 kB as per docs)
)

// Types
type (
	Share   = corona.Share
	Precomp = corona.Precomp
)

// KeyGen generates a new key pair
func KeyGen(seed []byte) ([]byte, []byte, error) {
	if len(seed) != 32 {
		return nil, nil, errors.New("seed must be 32 bytes")
	}
	return corona.KeyGen(seed)
}

// Precompute generates precomputation data
func Precompute(sk []byte) (Precomp, error) {
	if len(sk) != SKSize {
		return nil, errors.New("invalid secret key size")
	}
	return corona.Precompute(sk)
}

// QuickSign creates a signature share using precomputation
func QuickSign(pre Precomp, msg []byte) (Share, error) {
	if len(msg) != 32 {
		return nil, errors.New("message must be 32 bytes")
	}
	return corona.QuickSign(pre, msg)
}

// Sign creates a signature share without precomputation
func Sign(sk []byte, msg []byte) (Share, error) {
	if len(sk) != SKSize {
		return nil, errors.New("invalid secret key size")
	}
	if len(msg) != 32 {
		return nil, errors.New("message must be 32 bytes")
	}

	// Use precomputation internally for better performance
	pre, err := Precompute(sk)
	if err != nil {
		return nil, err
	}
	return QuickSign(pre, msg)
}

// VerifyShare verifies a signature share
func VerifyShare(pk []byte, msg []byte, share Share) bool {
	if len(pk) != PKSize || len(msg) != 32 || len(share) != ShareSize {
		return false
	}
	return corona.VerifyShare(pk, msg, share)
}

// Aggregate combines shares into a certificate
func Aggregate(shares []Share) ([]byte, error) {
	if len(shares) == 0 {
		return nil, errors.New("no shares to aggregate")
	}
	return corona.Aggregate(shares)
}

// Verify verifies a certificate
func Verify(pk []byte, msg []byte, cert []byte) bool {
	if len(pk) != PKSize || len(msg) != 32 || len(cert) != CertSize {
		return false
	}
	return corona.Verify(pk, msg, cert)
}

// HashMessage hashes a message to 32 bytes for signing
func HashMessage(msg []byte) [32]byte {
	return sha256.Sum256(msg)
}

// QuickSignHash signs a pre-hashed message
func QuickSignHash(pre Precomp, hash [32]byte) (Share, error) {
	return QuickSign(pre, hash[:])
}

// SignHash signs a pre-hashed message
func SignHash(sk []byte, hash [32]byte) (Share, error) {
	return Sign(sk, hash[:])
}

// VerifyShareHash verifies a share with a pre-hashed message
func VerifyShareHash(pk []byte, hash [32]byte, share Share) bool {
	return VerifyShare(pk, hash[:], share)
}

// VerifyHash verifies a certificate with a pre-hashed message
func VerifyHash(pk []byte, hash [32]byte, cert []byte) bool {
	return Verify(pk, hash[:], cert)
}