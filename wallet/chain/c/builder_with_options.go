// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"

	"github.com/luxfi/evm/plugin/evm/atomic"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/vms/secp256k1fx"
	walletutil "github.com/luxfi/node/wallet"

	"github.com/luxfi/evm"
)

var _ Builder = (*builderWithOptions)(nil)

type builderWithOptions struct {
	Builder
	options []walletutil.Option
}

// NewBuilderWithOptions returns a new transaction builder that will use the
// given options by default.
//
//   - [builder] is the builder that will be called to perform the underlying
//     operations.
//   - [options] will be provided to the builder in addition to the options
//     provided in the method calls.
func NewBuilderWithOptions(builder Builder, options ...walletutil.Option) Builder {
	return &builderWithOptions{
		Builder: builder,
		options: options,
	}
}

func (b *builderWithOptions) GetBalance(
	options ...walletutil.Option,
) (*big.Int, error) {
	return b.Builder.GetBalance(
		walletutil.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) GetImportableBalance(
	chainID ids.ID,
	options ...walletutil.Option,
) (uint64, error) {
	return b.Builder.GetImportableBalance(
		chainID,
		walletutil.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewImportTx(
	chainID ids.ID,
	to geth.Address,
	baseFee *big.Int,
	options ...walletutil.Option,
) (*atomic.UnsignedImportTx, error) {
	return b.Builder.NewImportTx(
		chainID,
		to,
		baseFee,
		walletutil.UnionOptions(b.options, options)...,
	)
}

func (b *builderWithOptions) NewExportTx(
	chainID ids.ID,
	outputs []*secp256k1fx.TransferOutput,
	baseFee *big.Int,
	options ...walletutil.Option,
) (*atomic.UnsignedExportTx, error) {
	return b.Builder.NewExportTx(
		chainID,
		outputs,
		baseFee,
		walletutil.UnionOptions(b.options, options)...,
	)
}
