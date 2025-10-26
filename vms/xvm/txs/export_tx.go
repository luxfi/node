<<<<<<< HEAD:vms/avm/txs/export_tx.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/txs/export_tx.go
// See the file LICENSE for licensing terms.

package txs

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/secp256k1fx"
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

func (t *ExportTx) InitCtx(ctx context.Context) {
	for _, out := range t.ExportedOuts {
		out.InitCtx(ctx)
	}
	t.BaseTx.InitCtx(ctx)
}

// InitializeContext initializes the context for this transaction
func (t *ExportTx) InitializeContext(ctx context.Context) error {
	t.InitCtx(ctx)
	return nil
}

func (t *ExportTx) Visit(v Visitor) error {
	return v.ExportTx(t)
}

// InitializeWithContext initializes the transaction with consensus context
func (tx *ExportTx) InitializeWithContext(ctx context.Context) error {
	// Initialize any context-dependent fields here
	return nil
}
