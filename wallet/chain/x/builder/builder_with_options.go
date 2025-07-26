// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/wallet"
)

var _ Builder = (*builderWithOptions)(nil)

type builderWithOptions struct {
	builder Builder
	options []wallet.Option
}

// NewWithOptions returns a new transaction builder that will use the given
// options by default.
//
//   - [builder] is the builder that will be called to perform the underlying
//     operations.
//   - [options] will be provided to the builder in addition to the options
//     provided in the method calls.
func NewWithOptions(builder Builder, options ...wallet.Option) Builder {
	return &builderWithOptions{
		builder: builder,
		options: options,
	}
}

func (b *builderWithOptions) Context() *Context {
	return b.builder.Context()
}

func (b *builderWithOptions) GetFTBalance(
	options ...wallet.Option,
) (map[ids.ID]uint64, error) {
	return b.builder.GetFTBalance(
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) GetImportableBalance(
	chainID ids.ID,
	options ...wallet.Option,
) (map[ids.ID]uint64, error) {
	return b.builder.GetImportableBalance(
		chainID,
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewBaseTx(
	outputs []*lux.TransferableOutput,
	options ...wallet.Option,
) (*txs.BaseTx, error) {
	return b.builder.NewBaseTx(
		outputs,
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	options ...wallet.Option,
) (*txs.CreateAssetTx, error) {
	return b.builder.NewCreateAssetTx(
		name,
		symbol,
		denomination,
		initialState,
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTx(
	operations []*txs.Operation,
	options ...wallet.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTx(
		operations,
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	options ...wallet.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTxMintFT(
		outputs,
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxMintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	options ...wallet.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTxMintNFT(
		assetID,
		payload,
		owners,
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxMintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...wallet.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTxMintProperty(
		assetID,
		owner,
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewOperationTxBurnProperty(
	assetID ids.ID,
	options ...wallet.Option,
) (*txs.OperationTx, error) {
	return b.builder.NewOperationTxBurnProperty(
		assetID,
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewImportTx(
	chainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...wallet.Option,
) (*txs.ImportTx, error) {
	return b.builder.NewImportTx(
		chainID,
		to,
		wallet.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewExportTx(
	chainID ids.ID,
	outputs []*lux.TransferableOutput,
	options ...wallet.Option,
) (*txs.ExportTx, error) {
	return b.builder.NewExportTx(
		chainID,
		outputs,
		wallet.UnionOptions(b.options, options)...,
	)
}
