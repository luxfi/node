// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"github.com/luxfi/crypto/bls"
)

// BLSSignerWrapper wraps a SecretKey to implement the Signer interface with error returns
type BLSSignerWrapper struct {
	key *bls.SecretKey
}

func NewBLSSignerWrapper(key *bls.SecretKey) bls.Signer {
	return &BLSSignerWrapper{key: key}
}

func (s *BLSSignerWrapper) Sign(msg []byte) (*bls.Signature, error) {
	// The underlying Sign doesn't return an error, so we add the error return
	sig := s.key.Sign(msg)
	return sig, nil
}

func (s *BLSSignerWrapper) PublicKey() *bls.PublicKey {
	return s.key.PublicKey()
}

func (s *BLSSignerWrapper) SignProofOfPossession(msg []byte) (*bls.Signature, error) {
	// For ProofOfPossession, we can use the same Sign method
	return s.Sign(msg)
}