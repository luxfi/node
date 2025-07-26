// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"math/big"
	"time"

	"github.com/luxfi/geth/ethclient"
	// "github.com/luxfi/evm/plugin/evm"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/rpc"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/subnet/primary/common"

	ethcommon "github.com/ethereum/go-ethereum/common"
)

var _ Wallet = (*wallet)(nil)

type Wallet interface {
	// Builder returns the builder that will be used to create the transactions.
	Builder() Builder

	// Signer returns the signer that will be used to sign the transactions.
	Signer() Signer

	// IssueImportTx creates, signs, and issues an import transaction that
	// attempts to consume all the available UTXOs and import the funds to [to].
	//
	// - [chainID] specifies the chain to be importing funds from.
	// - [to] specifies where to send the imported funds to.
	IssueImportTx(
		chainID ids.ID,
		to ethcommon.Address,
		options ...common.Option,
	) (*Tx, error)

	// IssueExportTx creates, signs, and issues an export transaction that
	// attempts to send all the provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	IssueExportTx(
		chainID ids.ID,
		outputs []*secp256k1fx.TransferOutput,
		options ...common.Option,
	) (*Tx, error)

	// IssueUnsignedTx signs and issues the unsigned tx.
	IssueUnsignedAtomicTx(
		utx UnsignedAtomicTx,
		options ...common.Option,
	) (*Tx, error)

	// IssueAtomicTx issues the signed tx.
	IssueAtomicTx(
		tx *Tx,
		options ...common.Option,
	) error
}

func NewWallet(
	builder Builder,
	signer Signer,
	luxClient Client,
	ethClient ethclient.Client,
	backend Backend,
) Wallet {
	return &wallet{
		Backend:   backend,
		builder:   builder,
		signer:    signer,
		luxClient: luxClient,
		ethClient: ethClient,
	}
}

type wallet struct {
	Backend
	builder   Builder
	signer    Signer
	luxClient Client
	ethClient ethclient.Client
}

func (w *wallet) Builder() Builder {
	return w.builder
}

func (w *wallet) Signer() Signer {
	return w.signer
}

func (w *wallet) IssueImportTx(
	chainID ids.ID,
	to ethcommon.Address,
	options ...common.Option,
) (*Tx, error) {
	baseFee, err := w.baseFee(options)
	if err != nil {
		return nil, err
	}

	utx, err := w.builder.NewImportTx(chainID, to, baseFee, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedAtomicTx(utx, options...)
}

func (w *wallet) IssueExportTx(
	chainID ids.ID,
	outputs []*secp256k1fx.TransferOutput,
	options ...common.Option,
) (*Tx, error) {
	baseFee, err := w.baseFee(options)
	if err != nil {
		return nil, err
	}

	utx, err := w.builder.NewExportTx(chainID, outputs, baseFee, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedAtomicTx(utx, options...)
}

func (w *wallet) IssueUnsignedAtomicTx(
	utx UnsignedAtomicTx,
	options ...common.Option,
) (*Tx, error) {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	tx, err := SignUnsignedAtomic(ctx, w.signer, utx)
	if err != nil {
		return nil, err
	}

	return tx, w.IssueAtomicTx(tx, options...)
}

func (w *wallet) IssueAtomicTx(
	tx *Tx,
	options ...common.Option,
) error {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	txID, err := w.luxClient.IssueTx(tx)
	if err != nil {
		return err
	}

	if f := ops.PostIssuanceFunc(); f != nil {
		f(txID)
	}

	if ops.AssumeDecided() {
		return w.Backend.AcceptAtomicTx(ctx, tx)
	}

	if err := awaitTxAccepted(w.luxClient, ctx, txID, ops.PollFrequency()); err != nil {
		return err
	}

	return w.Backend.AcceptAtomicTx(ctx, tx)
}

func (w *wallet) baseFee(options []common.Option) (*big.Int, error) {
	ops := common.NewOptions(options)
	baseFee := ops.BaseFee(nil)
	if baseFee != nil {
		return baseFee, nil
	}

	// TODO: Implement EstimateBaseFee in ethclient
	// For now, return a default base fee
	return big.NewInt(25000000000), nil // 25 gwei
}

// TODO: Upstream this function into coreth.
func awaitTxAccepted(
	c Client,
	ctx context.Context,
	txID ids.ID,
	freq time.Duration,
	options ...rpc.Option,
) error {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		status, err := c.GetAtomicTxStatus(txID)
		if err != nil {
			return err
		}

		if status == Accepted {
			return nil
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
