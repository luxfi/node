// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// Post-quantum cryptography support - FALCON signatures for X-Chain

package falconfx

import (
	"github.com/luxfi/node/codec"
	"github.com/luxfi/log"
)

// VM defines the required VM interface for FALCON fx
type VM interface {
	CodecRegistry() codec.Registry
	Logger() log.Logger
}
