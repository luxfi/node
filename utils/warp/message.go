// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/luxfi/ids"
)

// Message represents a parsed warp message.
// This is a minimal interface to avoid import cycles.
type Message interface {
	// SourceChainID returns the ID of the chain that sent this message
	SourceChainID() ids.ID
	
	// ID returns the unique ID of this message
	ID() ids.ID
	
	// Bytes returns the binary representation of this message
	Bytes() []byte
	
	// Signature returns the signature interface
	Signature() interface{ NumSigners() (int, error) }
}