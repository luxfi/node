// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/vms/components/lux"
	"github.com/luxfi/node/v2/vms/components/verify"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
	"github.com/luxfi/node/v2/vms/xvm/txs"
	walletutil "github.com/luxfi/node/v2/wallet"
	"github.com/luxfi/node/v2/wallet/chain/x/builder"
	"github.com/luxfi/node/v2/wallet/chain/x/signer"
)

var _ Wallet = (*walletWithOptions)(nil)

func NewWalletWithOptions(
	wallet Wallet,
	options ...walletutil.Option,
) Wallet {
	return &walletWithOptions{
		wallet:  wallet,
		options: options,
	}
}

type walletWithOptions struct {
	wallet  Wallet
	options []walletutil.Option
}

func (w *walletWithOptions) Builder() builder.Builder {
	return builder.NewWithOptions(
		w.wallet.Builder(),
		w.options...,
	)
}

func (w *walletWithOptions) Signer() signer.Signer {
	return w.wallet.Signer()
}

func (w *walletWithOptions) IssueBaseTx(
	outputs []*lux.TransferableOutput,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueBaseTx(
		outputs,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueCreateAssetTx(
		name,
		symbol,
		denomination,
		initialState,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTx(
	operations []*txs.Operation,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTx(
		operations,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTxMintFT(
		outputs,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTxMintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTxMintNFT(
		assetID,
		payload,
		owners,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTxMintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTxMintProperty(
		assetID,
		owner,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueOperationTxBurnProperty(
	assetID ids.ID,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueOperationTxBurnProperty(
		assetID,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueImportTx(
	chainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueImportTx(
		chainID,
		to,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueExportTx(
	chainID ids.ID,
	outputs []*lux.TransferableOutput,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueExportTx(
		chainID,
		outputs,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueUnsignedTx(
	utx txs.UnsignedTx,
	options ...walletutil.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueUnsignedTx(
		utx,
		walletutil.UnionOptions(w.options, options)...,
	)
}

func (w *walletWithOptions) IssueTx(
	tx *txs.Tx,
	options ...walletutil.Option,
) error {
	return w.wallet.IssueTx(
		tx,
		walletutil.UnionOptions(w.options, options)...,
	)
}
