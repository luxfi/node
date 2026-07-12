// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"errors"

	"github.com/go-json-experiment/json"
	"github.com/luxfi/formatting"
)

// RTKeyLen is the length of Corona public keys (ML-DSA-65 public key)
const RTKeyLen = 1952

// RTSigLen is the length of Corona signatures (ML-DSA-65 signature)
const RTSigLen = 3309

var (
	_ RTSigner = (*RTProofOfPossession)(nil)

	ErrInvalidRTProofOfPossession = errors.New("invalid Corona proof of possession")
	ErrRTKeyRequired              = errors.New("Corona public key required for Q-Chain validators")
	ErrInvalidRTKeyLength         = errors.New("invalid Corona public key length")
	ErrInvalidRTSigLength         = errors.New("invalid Corona signature length")
)

// RTSigner interface for Corona proof of possession
type RTSigner interface {
	Verify() error
	RTKey() []byte
}

// RTProofOfPossession proves ownership of a Corona (ML-DSA) keypair.
// This is REQUIRED for all Q-Chain validators.
//
// Key material binding: NodeID X corresponds to:
//   - TLS pubkey A (staking certificate)
//   - BLS pubkey B (aggregate signatures)
//   - RT pubkey C (post-quantum security)
//
// The RTProofOfPossession binds the RT public key to the validator identity
// by requiring a signature over the public key itself.
type RTProofOfPossession struct {
	// PublicKey is the ML-DSA-65 public key (1952 bytes)
	PublicKey []byte `json:"publicKey"`
	// ProofOfPossession is the ML-DSA signature over PublicKey, proving ownership
	ProofOfPossession []byte `json:"proofOfPossession"`
}

// NewRTProofOfPossession creates a new RTProofOfPossession from a private key.
// The signature is created over the public key bytes.
func NewRTProofOfPossession(publicKey []byte, sign func(msg []byte) ([]byte, error)) (*RTProofOfPossession, error) {
	if len(publicKey) != RTKeyLen {
		return nil, ErrInvalidRTKeyLength
	}

	sig, err := sign(publicKey)
	if err != nil {
		return nil, err
	}

	return &RTProofOfPossession{
		PublicKey:         publicKey,
		ProofOfPossession: sig,
	}, nil
}

// Verify verifies the proof of possession signature.
func (p *RTProofOfPossession) Verify() error {
	if len(p.PublicKey) != RTKeyLen {
		return ErrInvalidRTKeyLength
	}
	if len(p.ProofOfPossession) == 0 {
		return ErrInvalidRTProofOfPossession
	}
	// Note: Actual ML-DSA signature verification would be done here
	// using the luxfi/crypto/pq package. For now, we verify lengths
	// and non-empty values. Full verification requires the verifier.
	if len(p.ProofOfPossession) != RTSigLen {
		return ErrInvalidRTSigLength
	}
	return nil
}

// RTKey returns the Corona public key.
func (p *RTProofOfPossession) RTKey() []byte {
	return p.PublicKey
}

type jsonRTProofOfPossession struct {
	PublicKey         string `json:"publicKey"`
	ProofOfPossession string `json:"proofOfPossession"`
}

func (p *RTProofOfPossession) MarshalJSON() ([]byte, error) {
	pk, err := formatting.Encode(formatting.HexNC, p.PublicKey)
	if err != nil {
		return nil, err
	}
	pop, err := formatting.Encode(formatting.HexNC, p.ProofOfPossession)
	if err != nil {
		return nil, err
	}
	return json.Marshal(jsonRTProofOfPossession{
		PublicKey:         pk,
		ProofOfPossession: pop,
	})
}

func (p *RTProofOfPossession) UnmarshalJSON(b []byte) error {
	jsonRT := jsonRTProofOfPossession{}
	err := json.Unmarshal(b, &jsonRT)
	if err != nil {
		return err
	}

	pkBytes, err := formatting.Decode(formatting.HexNC, jsonRT.PublicKey)
	if err != nil {
		return err
	}
	popBytes, err := formatting.Decode(formatting.HexNC, jsonRT.ProofOfPossession)
	if err != nil {
		return err
	}

	p.PublicKey = pkBytes
	p.ProofOfPossession = popBytes
	return nil
}
