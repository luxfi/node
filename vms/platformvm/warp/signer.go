// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	utilswarp "github.com/luxfi/node/utils/warp"
)

var (
	_ Signer = (*signer)(nil)

	ErrWrongSourceChainID = errors.New("wrong SourceChainID")
	ErrWrongNetworkID     = errors.New("wrong networkID")
)

// Signer wraps the utils/warp.Signer interface to work with UnsignedMessage
type Signer interface {
	// Returns this node's BLS signature over an unsigned message. If the caller
	// does not have the authority to sign the message, an error will be
	// returned.
	//
	// Assumes the unsigned message is correctly initialized.
	Sign(msg *UnsignedMessage) ([]byte, error)
}

func NewSigner(sk bls.Signer, networkID uint32, chainID ids.ID) Signer {
	// Create a basic signer from utils/warp
	basicSigner := utilswarp.NewBasicSigner(sk, networkID, chainID)
	return &signer{
		basicSigner: basicSigner,
		networkID:   networkID,
		chainID:     chainID,
	}
}

type signer struct {
	basicSigner utilswarp.Signer
	networkID   uint32
	chainID     ids.ID
}

func (s *signer) Sign(msg *UnsignedMessage) ([]byte, error) {
	if msg.SourceChainID != s.chainID {
		return nil, ErrWrongSourceChainID
	}
	if msg.NetworkID != s.networkID {
		return nil, ErrWrongNetworkID
	}

	// Use the basic signer from utils/warp
	return s.basicSigner.Sign(msg.NetworkID, msg.SourceChainID, msg.Payload)
}
