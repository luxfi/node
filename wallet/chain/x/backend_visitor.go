// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/vms/components/lux"
	"github.com/luxfi/node/v2/vms/xvm/txs"
)

var _ txs.Visitor = (*backendVisitor)(nil)

// backendVisitor handles accepting of transactions for the backend
type backendVisitor struct {
	b    *backend
	ctx  context.Context
	txID ids.ID
}

func (*backendVisitor) BaseTx(*txs.BaseTx) error {
	return nil
}

func (*backendVisitor) CreateAssetTx(*txs.CreateAssetTx) error {
	return nil
}

func (*backendVisitor) OperationTx(*txs.OperationTx) error {
	return nil
}

func (*backendVisitor) BurnTx(*txs.BurnTx) error {
	return nil
}

func (*backendVisitor) MintTx(*txs.MintTx) error {
	return nil
}

func (*backendVisitor) NFTTransferTx(*txs.NFTTransferTx) error {
	return nil
}

func (b *backendVisitor) ImportTx(tx *txs.ImportTx) error {
	for _, in := range tx.ImportedIns {
		utxoID := in.UTXOID.InputID()
		if err := b.b.RemoveUTXO(b.ctx, tx.SourceChain, utxoID); err != nil {
			return err
		}
	}
	return nil
}

func (b *backendVisitor) ExportTx(tx *txs.ExportTx) error {
	for i, out := range tx.ExportedOuts {
		err := b.b.AddUTXO(
			b.ctx,
			tx.DestinationChain,
			&lux.UTXO{
				UTXOID: lux.UTXOID{
					TxID:        b.txID,
					OutputIndex: uint32(len(tx.Outs) + i),
				},
				Asset: lux.Asset{ID: out.AssetID()},
				Out:   out.Out,
			},
		)
		if err != nil {
			return err
		}
	}
	return nil
}
