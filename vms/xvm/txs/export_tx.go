// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"github.com/luxfi/consensus"
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

func (t *ExportTx) InitCtx(ctx *consensus.Context) {
	for _, out := range t.ExportedOuts {
		out.InitCtx(ctx)
	}
	t.BaseTx.InitCtx(ctx)
}

func (t *ExportTx) Visit(v Visitor) error {
	return v.ExportTx(t)
}

// InitializeWithContext initializes the transaction with consensus context
func (tx *ExportTx) InitializeWithContext(ctx context.Context, chainCtx *consensus.Context) error {
    // Initialize any context-dependent fields here
    return nil
}
