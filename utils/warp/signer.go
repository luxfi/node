// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

// Signer defines the interface for signing warp messages.
// This interface is placed in utils/warp to avoid import cycles.
type Signer interface {
	// Sign returns this node's BLS signature over an unsigned message. If the caller
	// does not have the authority to sign the message, an error will be returned.
	//
	// The message must contain the correct NetworkID and SourceChainID.
	Sign(networkID uint32, sourceChainID ids.ID, payloadBytes []byte) ([]byte, error)
}

// SignerFactory creates a new Signer instance
type SignerFactory func(sk bls.Signer, networkID uint32, chainID ids.ID) Signer