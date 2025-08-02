// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import "github.com/luxfi/node/v2/vms/components/lux"

var _ Visitor = (*utxoGetter)(nil)

// Allow vm to execute custom logic against the underlying transaction types.
type Visitor interface {
	BaseTx(*BaseTx) error
	CreateAssetTx(*CreateAssetTx) error
	OperationTx(*OperationTx) error
	ImportTx(*ImportTx) error
	ExportTx(*ExportTx) error
	BurnTx(*BurnTx) error
	MintTx(*MintTx) error
	NFTTransferTx(*NFTTransferTx) error
}

// utxoGetter returns the UTXOs transaction is producing.
type utxoGetter struct {
	tx    *Tx
	utxos []*lux.UTXO
}

func (u *utxoGetter) BaseTx(tx *BaseTx) error {
	txID := u.tx.ID()
	u.utxos = make([]*lux.UTXO, len(tx.Outs))
	for i, out := range tx.Outs {
		u.utxos[i] = &lux.UTXO{
			UTXOID: lux.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i),
			},
			Asset: lux.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}
	}
	return nil
}

func (u *utxoGetter) ImportTx(tx *ImportTx) error {
	return u.BaseTx(&tx.BaseTx)
}

func (u *utxoGetter) ExportTx(tx *ExportTx) error {
	return u.BaseTx(&tx.BaseTx)
}

func (u *utxoGetter) CreateAssetTx(t *CreateAssetTx) error {
	if err := u.BaseTx(&t.BaseTx); err != nil {
		return err
	}

	txID := u.tx.ID()
	for _, state := range t.States {
		for _, out := range state.Outs {
			u.utxos = append(u.utxos, &lux.UTXO{
				UTXOID: lux.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(u.utxos)),
				},
				Asset: lux.Asset{
					ID: txID,
				},
				Out: out,
			})
		}
	}
	return nil
}

func (u *utxoGetter) OperationTx(t *OperationTx) error {
	// The error is explicitly dropped here because no error is ever returned
	// from the utxoGetter.
	_ = u.BaseTx(&t.BaseTx)

	txID := u.tx.ID()
	for _, op := range t.Ops {
		asset := op.AssetID()
		for _, out := range op.Op.Outs() {
			u.utxos = append(u.utxos, &lux.UTXO{
				UTXOID: lux.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(u.utxos)),
				},
				Asset: lux.Asset{ID: asset},
				Out:   out,
			})
		}
	}
	return nil
}

func (u *utxoGetter) BurnTx(tx *BurnTx) error {
	// Burn transactions don't produce UTXOs for the burned assets
	// Only handle any change outputs from the base transaction
	return u.BaseTx(&tx.BaseTx)
}

func (u *utxoGetter) MintTx(tx *MintTx) error {
	// Mint transactions produce UTXOs for the minted assets
	return u.BaseTx(&tx.BaseTx)
}

func (u *utxoGetter) NFTTransferTx(tx *NFTTransferTx) error {
	// NFT transfer transactions don't produce UTXOs on X-Chain
	// The NFT is burned on X-Chain and minted on destination chain
	return u.BaseTx(&tx.BaseTx)
}
