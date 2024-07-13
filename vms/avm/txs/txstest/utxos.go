// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	"fmt"

	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/database"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/avm/state"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/wallet/chain/x/builder"
	"github.com/luxfi/node/wallet/chain/x/signer"
)

const maxPageSize uint64 = 1024

var (
	_ builder.Backend = (*walletUTXOsAdapter)(nil)
	_ signer.Backend  = (*walletUTXOsAdapter)(nil)
)

func newUTXOs(
	ctx *snow.Context,
	state state.State,
	sharedMemory atomic.SharedMemory,
	codec codec.Manager,
) *utxos {
	return &utxos{
		xchainID:     ctx.ChainID,
		state:        state,
		sharedMemory: sharedMemory,
		codec:        codec,
	}
}

type utxos struct {
	xchainID     ids.ID
	state        state.State
	sharedMemory atomic.SharedMemory
	codec        codec.Manager
}

func (u *utxos) UTXOs(addrs set.Set[ids.ShortID], sourceChainID ids.ID) ([]*lux.UTXO, error) {
	if sourceChainID == u.xchainID {
		return lux.GetAllUTXOs(u.state, addrs)
	}

	atomicUTXOs, _, _, err := lux.GetAtomicUTXOs(
		u.sharedMemory,
		u.codec,
		sourceChainID,
		addrs,
		ids.ShortEmpty,
		ids.Empty,
		int(maxPageSize),
	)
	return atomicUTXOs, err
}

func (u *utxos) GetUTXO(addrs set.Set[ids.ShortID], chainID, utxoID ids.ID) (*lux.UTXO, error) {
	if chainID == u.xchainID {
		return u.state.GetUTXO(utxoID)
	}

	atomicUTXOs, _, _, err := lux.GetAtomicUTXOs(
		u.sharedMemory,
		u.codec,
		chainID,
		addrs,
		ids.ShortEmpty,
		ids.Empty,
		int(maxPageSize),
	)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}
	for _, utxo := range atomicUTXOs {
		if utxo.InputID() == utxoID {
			return utxo, nil
		}
	}
	return nil, database.ErrNotFound
}

type walletUTXOsAdapter struct {
	utxos *utxos
	addrs set.Set[ids.ShortID]
}

func (w *walletUTXOsAdapter) UTXOs(_ context.Context, sourceChainID ids.ID) ([]*lux.UTXO, error) {
	return w.utxos.UTXOs(w.addrs, sourceChainID)
}

func (w *walletUTXOsAdapter) GetUTXO(_ context.Context, chainID, utxoID ids.ID) (*lux.UTXO, error) {
	return w.utxos.GetUTXO(w.addrs, chainID, utxoID)
}
