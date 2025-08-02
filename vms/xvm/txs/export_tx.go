// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/vms/components/lux"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
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

func (t *ExportTx) InitCtx(ctx *quasar.Context) {
	for _, out := range t.ExportedOuts {
		out.InitCtx(ctx)
	}
	t.BaseTx.InitCtx(ctx)
}

// Initialize implements quasar.ContextInitializable
func (t *ExportTx) Initialize(ctx *quasar.Context) error {
	t.InitCtx(ctx)
	return nil
}

func (t *ExportTx) Visit(v Visitor) error {
	return v.ExportTx(t)
}
