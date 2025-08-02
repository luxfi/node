// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/database"
	"github.com/luxfi/node/vms/secp256k1fx"
)

// VM defines the interface that the transactions need to access VM state
type VM interface {
	// Codec returns the codec used for serializing transactions
	Codec() secp256k1fx.VM

	// State returns the database used to track state
	State() database.Database

	// Context returns the context of the VM
	Ctx() *context.Context

	// ChainID returns the ID of the chain
	ChainID() ids.ID

	// NetworkID returns the ID of the network
	NetworkID() uint32
}