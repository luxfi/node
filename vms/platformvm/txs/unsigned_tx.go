// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/runtime"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// RuntimeInitializable defines the interface for initializing context
type RuntimeInitializable interface {
	InitRuntime(rt *runtime.Runtime)
}

// UnsignedTx is an unsigned transaction
type UnsignedTx interface {
	// RuntimeInitializable is required for both platformvm and exchangevm
	// transaction types to share initialization logic.
	RuntimeInitializable
	secp256k1fx.UnsignedTx
	SetBytes(unsignedBytes []byte)

	// InputIDs returns the set of inputs this transaction consumes
	InputIDs() set.Set[ids.ID]

	Outputs() []*lux.TransferableOutput

	// Attempts to verify this transaction without any provided state.
	SyntacticVerify(rt *runtime.Runtime) error

	// Visit calls [visitor] with this transaction's concrete type
	Visit(visitor Visitor) error
}
