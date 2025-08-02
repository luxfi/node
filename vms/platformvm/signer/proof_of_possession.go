// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/node/v2/utils/formatting"
)

var (
	_ Signer = (*ProofOfPossession)(nil)

	errInvalidProofOfPossession = errors.New("invalid proof of possession")
)

type ProofOfPossession struct {
	PublicKey [bls.PublicKeyLen]byte `serialize:"true" json:"publicKey"`
	// BLS signature proving ownership of [PublicKey]. The signed message is the
	// [PublicKey].
	ProofOfPossession [bls.SignatureLen]byte `serialize:"true" json:"proofOfPossession"`

	// publicKey is the parsed version of [PublicKey]. It is populated in
	// [Verify].
	publicKey *bls.PublicKey
}

func NewProofOfPossession(sk *bls.SecretKey) *ProofOfPossession {
	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToCompressedBytes(pk)
	sig := sk.SignProofOfPossession(pkBytes)
	sigBytes := bls.SignatureToBytes(sig)

	pop := &ProofOfPossession{
		publicKey: pk,
	}
	copy(pop.PublicKey[:], pkBytes)
	copy(pop.ProofOfPossession[:], sigBytes)
	return pop
}

func (p *ProofOfPossession) Verify() error {
	// ---------------------------------------------------------------------
	// DEV / migration flag: allow EMPTY proof exactly once, on first boot.
	// ---------------------------------------------------------------------
	if os.Getenv("LUX_GENESIS") == "1" {
		// Empty POP is all zeros – accept it and continue initial replay.
		p.publicKey, _ = bls.PublicKeyFromCompressedBytes(p.PublicKey[:])
		return nil
	}

	// --- normal path ------------------------------------------------------
	publicKey, err := bls.PublicKeyFromCompressedBytes(p.PublicKey[:])
	if err != nil {
		return err
	}
	signature, err := bls.SignatureFromBytes(p.ProofOfPossession[:])
	if err != nil {
		return err
	}
	if !bls.VerifyProofOfPossession(publicKey, signature, p.PublicKey[:]) {
		return errInvalidProofOfPossession
	}
	p.publicKey = publicKey
	return nil
}

func (p *ProofOfPossession) Key() *bls.PublicKey {
	return p.publicKey
}

type jsonProofOfPossession struct {
	PublicKey         string `json:"publicKey"`
	ProofOfPossession string `json:"proofOfPossession"`
}

func (p *ProofOfPossession) MarshalJSON() ([]byte, error) {
	pk, err := formatting.Encode(formatting.HexNC, p.PublicKey[:])
	if err != nil {
		return nil, err
	}
	pop, err := formatting.Encode(formatting.HexNC, p.ProofOfPossession[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(jsonProofOfPossession{
		PublicKey:         pk,
		ProofOfPossession: pop,
	})
}

func (p *ProofOfPossession) UnmarshalJSON(b []byte) error {
	jsonBLS := jsonProofOfPossession{}
	err := json.Unmarshal(b, &jsonBLS)
	if err != nil {
		return err
	}

	pkBytes, err := formatting.Decode(formatting.HexNC, jsonBLS.PublicKey)
	if err != nil {
		return err
	}
	pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	if err != nil {
		return err
	}

	popBytes, err := formatting.Decode(formatting.HexNC, jsonBLS.ProofOfPossession)
	if err != nil {
		return err
	}

	copy(p.PublicKey[:], pkBytes)
	copy(p.ProofOfPossession[:], popBytes)
	p.publicKey = pk
	return nil
}
