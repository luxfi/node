// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"github.com/luxfi/evm/plugin/evm/atomic"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/vms/secp256k1fx"
	walletutil "github.com/luxfi/node/wallet"

	"github.com/luxfi/evm"
)

var _ Wallet = (*walletWithOptions)(nil)

func NewWalletWithOptions(
	wallet Wallet,
	options ...walletutil.Option,
) Wallet {
	return &walletWithOptions{
		Wallet:  wallet,
		options: options,
	}
}

type walletWithOptions struct {
	Wallet
	options []walletutil.Option
}

func (w *walletWithOptions) Builder() Builder {
	return NewBuilderWithOptions(
		w.Wallet.Builder(),
		w.options...,
	)
}

func (w *walletWithOptions) IssueImportTx(
	chainID ids.ID,
	to geth.Address,
	options ...walletutil.Option,
) (*atomic.Tx, error) {
	return w.Wallet.IssueImportTx(
		chainID,
		to,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueExportTx(
	chainID ids.ID,
	outputs []*secp256k1fx.TransferOutput,
	options ...walletutil.Option,
) (*atomic.Tx, error) {
	return w.Wallet.IssueExportTx(
		chainID,
		outputs,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueUnsignedAtomicTx(
	utx atomic.UnsignedAtomicTx,
	options ...walletutil.Option,
) (*atomic.Tx, error) {
	return w.Wallet.IssueUnsignedAtomicTx(
		utx,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueAtomicTx(
	tx *atomic.Tx,
	options ...walletutil.Option,
) error {
	return w.Wallet.IssueAtomicTx(
		tx,
		walletutil.UnionOptions(w.options, options)...,
	)
}
