// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wallet

import (
	"context"
	"sync"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/node/wallet/chain/p/signer"
	"github.com/luxfi/node/wallet/subnet/primary/common"
)

var _ Backend = (*backend)(nil)

// Backend defines the full interface required to support a P-chain wallet.
type Backend interface {
	builder.Backend
	signer.Backend

	AcceptTx(ctx context.Context, tx *txs.Tx) error
}

type backend struct {
	common.ChainUTXOs

	ownersLock sync.RWMutex
	owners     map[ids.ID]fx.Owner // subnetID or validationID -> owner
}

func NewBackend(utxos common.ChainUTXOs, owners map[ids.ID]fx.Owner) Backend {
	return &backend{
		ChainUTXOs: utxos,
		owners:     owners,
	}
}

func (b *backend) AcceptTx(ctx context.Context, tx *txs.Tx) error {
	txID := tx.ID()
	err := tx.Unsigned.Visit(&backendVisitor{
		b:    b,
		ctx:  ctx,
		txID: txID,
	})
	if err != nil {
		return err
	}

	producedUTXOSlice := tx.UTXOs()
	return b.addUTXOs(ctx, constants.PlatformChainID, producedUTXOSlice)
}

func (b *backend) addUTXOs(ctx context.Context, destinationChainID ids.ID, utxos []*avax.UTXO) error {
	for _, utxo := range utxos {
		if err := b.AddUTXO(ctx, destinationChainID, utxo); err != nil {
			return err
		}
	}
	return nil
}

func (b *backend) removeUTXOs(ctx context.Context, sourceChain ids.ID, utxoIDs set.Set[ids.ID]) error {
	for utxoID := range utxoIDs {
		if err := b.RemoveUTXO(ctx, sourceChain, utxoID); err != nil {
			return err
		}
	}
	return nil
}

func (b *backend) GetOwner(_ context.Context, ownerID ids.ID) (fx.Owner, error) {
	b.ownersLock.RLock()
	defer b.ownersLock.RUnlock()

	owner, exists := b.owners[ownerID]
	if !exists {
		return nil, database.ErrNotFound
	}
	return owner, nil
}

func (b *backend) setOwner(ownerID ids.ID, owner fx.Owner) {
	b.ownersLock.Lock()
	defer b.ownersLock.Unlock()

	b.owners[ownerID] = owner
}
