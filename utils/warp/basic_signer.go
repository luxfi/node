// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/hashing"
)

var (
	ErrWrongSourceChainID = errors.New("wrong SourceChainID")
	ErrWrongNetworkID     = errors.New("wrong networkID")
)

// BasicSigner implements Signer for basic warp message signing
type BasicSigner struct {
	sk        bls.Signer
	networkID uint32
	chainID   ids.ID
}

// NewBasicSigner creates a new BasicSigner
func NewBasicSigner(sk bls.Signer, networkID uint32, chainID ids.ID) Signer {
	return &BasicSigner{
		sk:        sk,
		networkID: networkID,
		chainID:   chainID,
	}
}

// Sign implements the Signer interface
func (s *BasicSigner) Sign(networkID uint32, sourceChainID ids.ID, payloadBytes []byte) ([]byte, error) {
	if sourceChainID != s.chainID {
		return nil, ErrWrongSourceChainID
	}
	if networkID != s.networkID {
		return nil, ErrWrongNetworkID
	}

	// Create a simple message representation
	// This matches the warp message format but avoids importing platformvm/warp
	msgBytes := make([]byte, 0, 4+32+len(payloadBytes))
	msgBytes = append(msgBytes, byte(networkID>>24), byte(networkID>>16), byte(networkID>>8), byte(networkID))
	msgBytes = append(msgBytes, sourceChainID[:]...)
	msgBytes = append(msgBytes, payloadBytes...)
	
	// Hash the message
	msgHash := hashing.ComputeHash256(msgBytes)
	
	sig, err := s.sk.Sign(msgHash)
	if err != nil {
		return nil, err
	}
	return bls.SignatureToBytes(sig), nil
}