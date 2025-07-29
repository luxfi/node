// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	// "github.com/luxfi/node/evm/plugin/evm"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/utils/math"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/wallet/subnet/primary/common"

	ethcommon "github.com/luxfi/geth/common"
)

var (
	_ Backend = (*backend)(nil)

	errUnknownTxType = errors.New("unknown tx type")
)

// Backend defines the full interface required to support a C-chain wallet.
type Backend interface {
	common.ChainUTXOs
	BuilderBackend
	SignerBackend

	AcceptAtomicTx(ctx context.Context, tx *Tx) error
}

type backend struct {
	common.ChainUTXOs

	accountsLock sync.RWMutex
	accounts     map[ethcommon.Address]*Account
}

type Account struct {
	Balance *big.Int
	Nonce   uint64
}

func NewBackend(
	utxos common.ChainUTXOs,
	accounts map[ethcommon.Address]*Account,
) Backend {
	return &backend{
		ChainUTXOs: utxos,
		accounts:   accounts,
	}
}

func (b *backend) AcceptAtomicTx(ctx context.Context, tx *Tx) error {
	// TODO: Implement proper atomic transaction handling
	switch utx := tx.UnsignedAtomicTx.(type) {
	case *UnsignedImportTx:
		for _, input := range utx.ImportedInputs {
			utxoID := input.InputID()
			if err := b.RemoveUTXO(ctx, utx.SourceChain, utxoID); err != nil {
				return err
			}
		}

		b.accountsLock.Lock()
		defer b.accountsLock.Unlock()

		for _, output := range utx.Outs {
			account, ok := b.accounts[output.Address]
			if !ok {
				continue
			}

			balance := new(big.Int).SetUint64(output.Amount)
			balance.Mul(balance, luxConversionRate)
			account.Balance.Add(account.Balance, balance)
		}
	case *UnsignedExportTx:
		txID := tx.ID
		for i, out := range utx.ExportedOutputs {
			err := b.AddUTXO(
				ctx,
				utx.DestinationChain,
				&lux.UTXO{
					UTXOID: lux.UTXOID{
						TxID:        txID,
						OutputIndex: uint32(i),
					},
					Asset: lux.Asset{ID: out.AssetID()},
					Out:   out.Out,
				},
			)
			if err != nil {
				return err
			}
		}

		b.accountsLock.Lock()
		defer b.accountsLock.Unlock()

		for _, input := range utx.Ins {
			account, ok := b.accounts[input.Address]
			if !ok {
				continue
			}

			balance := new(big.Int).SetUint64(input.Amount)
			balance.Mul(balance, luxConversionRate)
			if account.Balance.Cmp(balance) == -1 {
				return errInsufficientFunds
			}
			account.Balance.Sub(account.Balance, balance)

			newNonce, err := math.Add64(input.Nonce, 1)
			if err != nil {
				return err
			}
			account.Nonce = newNonce
		}
	default:
		return fmt.Errorf("%w: %T", errUnknownTxType, utx)
	}
	return nil
}

func (b *backend) Balance(_ context.Context, addr ethcommon.Address) (*big.Int, error) {
	b.accountsLock.RLock()
	defer b.accountsLock.RUnlock()

	account, exists := b.accounts[addr]
	if !exists {
		return nil, database.ErrNotFound
	}
	return account.Balance, nil
}

func (b *backend) Nonce(_ context.Context, addr ethcommon.Address) (uint64, error) {
	b.accountsLock.RLock()
	defer b.accountsLock.RUnlock()

	account, exists := b.accounts[addr]
	if !exists {
		return 0, database.ErrNotFound
	}
	return account.Nonce, nil
}
