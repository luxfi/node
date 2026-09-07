// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"errors"

	"github.com/go-json-experiment/json"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/formatting"
)

var (
	_ HybridSigner = (*HybridProofOfPossession)(nil)

	ErrMissingBLSKey  = errors.New("missing BLS public key")
	ErrMissingRTKey   = errors.New("missing Corona public key for Q-Chain validator")
	ErrHybridRequired = errors.New("both BLS and RT keys required for Q-Chain validators")
)

// HybridSigner extends Signer with post-quantum Corona key support.
// This is the key material binding interface for Q-Chain validators.
//
// NodeID X corresponds to:
//   - TLS pubkey A (staking certificate) - established during TLS handshake
//   - BLS pubkey B (aggregate signatures) - from ProofOfPossession
//   - RT pubkey C (post-quantum security) - from RTProofOfPossession
//
// All three keys must be bound to the same validator identity for
// Q-Chain consensus to be secure against both classical and quantum attacks.
type HybridSigner interface {
	Signer
	RTSigner

	// HasRT returns true if this signer has a Corona key.
	// For Q-Chain validators, this MUST return true.
	HasRT() bool
}

// HybridProofOfPossession combines BLS and Corona proofs of possession.
// This is REQUIRED for all Q-Chain validators to bind both classical
// and post-quantum keys to their validator identity.
//
// Wire format:
//   - BLS PublicKey: 48 bytes (compressed)
//   - BLS ProofOfPossession: 96 bytes
//   - RT PublicKey: 1952 bytes (ML-DSA-65)
//   - RT ProofOfPossession: 3309 bytes (ML-DSA-65 signature)
type HybridProofOfPossession struct {
	BLS *ProofOfPossession   `json:"bls"`
	RT  *RTProofOfPossession `json:"rt"`
}

// NewHybridProofOfPossession creates a new hybrid proof combining BLS and RT.
func NewHybridProofOfPossession(
	blsSigner bls.Signer,
	rtPublicKey []byte,
	rtSign func(msg []byte) ([]byte, error),
) (*HybridProofOfPossession, error) {
	blsPoP, err := NewProofOfPossession(blsSigner)
	if err != nil {
		return nil, err
	}

	rtPoP, err := NewRTProofOfPossession(rtPublicKey, rtSign)
	if err != nil {
		return nil, err
	}

	return &HybridProofOfPossession{
		BLS: blsPoP,
		RT:  rtPoP,
	}, nil
}

// Verify verifies both BLS and RT proofs of possession.
func (h *HybridProofOfPossession) Verify() error {
	if h.BLS == nil {
		return ErrMissingBLSKey
	}
	if err := h.BLS.Verify(); err != nil {
		return err
	}
	if h.RT == nil {
		return ErrMissingRTKey
	}
	if err := h.RT.Verify(); err != nil {
		return err
	}
	return nil
}

// Key returns the BLS public key (implements Signer).
func (h *HybridProofOfPossession) Key() *bls.PublicKey {
	if h.BLS == nil {
		return nil
	}
	return h.BLS.Key()
}

// RTKey returns the Corona public key (implements RTSigner).
func (h *HybridProofOfPossession) RTKey() []byte {
	if h.RT == nil {
		return nil
	}
	return h.RT.RTKey()
}

// HasRT returns true if this hybrid signer has an RT key.
func (h *HybridProofOfPossession) HasRT() bool {
	return h.RT != nil && len(h.RT.PublicKey) > 0
}

type jsonHybridProofOfPossession struct {
	BLS *jsonProofOfPossession   `json:"bls"`
	RT  *jsonRTProofOfPossession `json:"rt"`
}

func (h *HybridProofOfPossession) MarshalJSON() ([]byte, error) {
	j := jsonHybridProofOfPossession{}

	if h.BLS != nil {
		pk, err := formatting.Encode(formatting.HexNC, h.BLS.PublicKey[:])
		if err != nil {
			return nil, err
		}
		pop, err := formatting.Encode(formatting.HexNC, h.BLS.ProofOfPossession[:])
		if err != nil {
			return nil, err
		}
		j.BLS = &jsonProofOfPossession{
			PublicKey:         pk,
			ProofOfPossession: pop,
		}
	}

	if h.RT != nil {
		pk, err := formatting.Encode(formatting.HexNC, h.RT.PublicKey)
		if err != nil {
			return nil, err
		}
		pop, err := formatting.Encode(formatting.HexNC, h.RT.ProofOfPossession)
		if err != nil {
			return nil, err
		}
		j.RT = &jsonRTProofOfPossession{
			PublicKey:         pk,
			ProofOfPossession: pop,
		}
	}

	return json.Marshal(j)
}

func (h *HybridProofOfPossession) UnmarshalJSON(b []byte) error {
	j := jsonHybridProofOfPossession{}
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}

	if j.BLS != nil {
		h.BLS = &ProofOfPossession{}
		pkBytes, err := formatting.Decode(formatting.HexNC, j.BLS.PublicKey)
		if err != nil {
			return err
		}
		popBytes, err := formatting.Decode(formatting.HexNC, j.BLS.ProofOfPossession)
		if err != nil {
			return err
		}
		copy(h.BLS.PublicKey[:], pkBytes)
		copy(h.BLS.ProofOfPossession[:], popBytes)
	}

	if j.RT != nil {
		h.RT = &RTProofOfPossession{}
		pkBytes, err := formatting.Decode(formatting.HexNC, j.RT.PublicKey)
		if err != nil {
			return err
		}
		popBytes, err := formatting.Decode(formatting.HexNC, j.RT.ProofOfPossession)
		if err != nil {
			return err
		}
		h.RT.PublicKey = pkBytes
		h.RT.ProofOfPossession = popBytes
	}

	return nil
}

// ValidateForQChain verifies that this hybrid signer has all required
// key material for Q-Chain validation. This enforces:
// - BLS key present and valid
// - RT key present and valid
func (h *HybridProofOfPossession) ValidateForQChain() error {
	if h.BLS == nil {
		return ErrMissingBLSKey
	}
	if h.RT == nil {
		return ErrMissingRTKey
	}
	if err := h.Verify(); err != nil {
		return err
	}
	if !h.HasRT() {
		return ErrHybridRequired
	}
	return nil
}
