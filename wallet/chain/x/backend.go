// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	stdcontext "context"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/vms/avm/txs"
	"github.com/luxdefi/node/vms/components/lux"
)

var _ Backend = (*backend)(nil)

type ChainUTXOs interface {
	AddUTXO(ctx stdcontext.Context, destinationChainID ids.ID, utxo *lux.UTXO) error
	RemoveUTXO(ctx stdcontext.Context, sourceChainID, utxoID ids.ID) error

	UTXOs(ctx stdcontext.Context, sourceChainID ids.ID) ([]*lux.UTXO, error)
	GetUTXO(ctx stdcontext.Context, sourceChainID, utxoID ids.ID) (*lux.UTXO, error)
}

// Backend defines the full interface required to support an X-chain wallet.
type Backend interface {
	ChainUTXOs
	BuilderBackend
	SignerBackend

	AcceptTx(ctx stdcontext.Context, tx *txs.Tx) error
}

type backend struct {
	Context
	ChainUTXOs
}

func NewBackend(ctx Context, utxos ChainUTXOs) Backend {
	return &backend{
		Context:    ctx,
		ChainUTXOs: utxos,
	}
}

func (b *backend) AcceptTx(ctx stdcontext.Context, tx *txs.Tx) error {
	err := tx.Unsigned.Visit(&backendVisitor{
		b:    b,
		ctx:  ctx,
		txID: tx.ID(),
	})
	if err != nil {
		return err
	}

	chainID := b.Context.BlockchainID()
	inputUTXOs := tx.Unsigned.InputUTXOs()
	for _, utxoID := range inputUTXOs {
		if utxoID.Symbol {
			continue
		}
		if err := b.RemoveUTXO(ctx, chainID, utxoID.InputID()); err != nil {
			return err
		}
	}

	outputUTXOs := tx.UTXOs()
	for _, utxo := range outputUTXOs {
		if err := b.AddUTXO(ctx, chainID, utxo); err != nil {
			return err
		}
	}
	return nil
}
