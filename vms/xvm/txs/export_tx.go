// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"

	"github.com/luxfi/runtime"
)

var (
	_ UnsignedTx             = (*ExportTx)(nil)
	_ secp256k1fx.UnsignedTx = (*ExportTx)(nil)
)

// ExportTx is a transaction that exports an asset to another blockchain.
type ExportTx struct {
	BaseTx `serialize:"true"`

	// Which chain to send the funds to
	DestinationChain ids.ID `serialize:"true" json:"destinationChain"`

	// The outputs this transaction is sending to the other chain
	ExportedOuts []*lux.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

func (t *ExportTx) InitRuntime(rt *runtime.Runtime) {
	for _, out := range t.ExportedOuts {
		out.InitRuntime(rt)
	}
	t.BaseTx.InitRuntime(rt)
}

// InitializeRuntime initializes the context for this transaction
func (t *ExportTx) InitializeRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

func (t *ExportTx) Visit(v Visitor) error {
	return v.ExportTx(t)
}

// InitializeWithRuntime initializes the transaction with Runtime
func (tx *ExportTx) InitializeWithRuntime(rt *runtime.Runtime) error {
	// Initialize any context-dependent fields here
	return nil
}
