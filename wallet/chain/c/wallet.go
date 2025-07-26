// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"math/big"
	"time"

	"github.com/luxfi/evm/ethclient"
	"github.com/luxfi/evm/plugin/evm/atomic"
	"github.com/luxfi/evm/plugin/evm/client"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/rpc"
	"github.com/luxfi/node/vms/secp256k1fx"
	walletutil "github.com/luxfi/node/wallet"

	geth "github.com/luxfi/geth/common"
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
		to geth.Address,
		options ...walletutil.Option,
	) (*atomic.Tx, error)

	// IssueExportTx creates, signs, and issues an export transaction that
	// attempts to send all the provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	IssueExportTx(
		chainID ids.ID,
		outputs []*secp256k1fx.TransferOutput,
		options ...walletutil.Option,
	) (*atomic.Tx, error)

	// IssueUnsignedAtomicTx signs and issues the unsigned tx.
	IssueUnsignedAtomicTx(
		utx atomic.UnsignedAtomicTx,
		options ...walletutil.Option,
	) (*atomic.Tx, error)

	// IssueAtomicTx issues the signed tx.
	IssueAtomicTx(
		tx *atomic.Tx,
		options ...walletutil.Option,
	) error
}

func NewWallet(
	builder Builder,
	signer Signer,
	luxClient client.Client,
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
	luxClient client.Client
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
	to geth.Address,
	options ...walletutil.Option,
) (*atomic.Tx, error) {
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
	options ...walletutil.Option,
) (*atomic.Tx, error) {
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
	utx atomic.UnsignedAtomicTx,
	options ...walletutil.Option,
) (*atomic.Tx, error) {
	ops := walletutil.NewOptions(options)
	ctx := ops.Context()
	tx, err := SignUnsignedAtomic(ctx, w.signer, utx)
	if err != nil {
		return nil, err
	}

	return tx, w.IssueAtomicTx(tx, options...)
}

func (w *wallet) IssueAtomicTx(
	tx *atomic.Tx,
	options ...walletutil.Option,
) error {
	ops := walletutil.NewOptions(options)
	ctx := ops.Context()
	startTime := time.Now()
	// TODO: IssueTx method has been removed from client.Client interface
	// Need to find the correct API to issue atomic transactions
	// txID, err := w.luxClient.IssueTx(ctx, tx.SignedBytes())
	// if err != nil {
	// 	return err
	// }
	var txID ids.ID
	_ = ctx

	issuanceDuration := time.Since(startTime)
	if f := ops.IssuanceHandler(); f != nil {
		f(walletutil.IssuanceReceipt{
			ChainAlias: Alias,
			TxID:       txID,
			Duration:   issuanceDuration,
		})
	}

	if ops.AssumeDecided() {
		return w.Backend.AcceptAtomicTx(ctx, tx)
	}

	if err := awaitTxAccepted(w.luxClient, ctx, txID, ops.PollFrequency()); err != nil {
		return err
	}

	if f := ops.ConfirmationHandler(); f != nil {
		totalDuration := time.Since(startTime)
		confirmationDuration := totalDuration - issuanceDuration

		f(walletutil.ConfirmationReceipt{
			ChainAlias:           Alias,
			TxID:                 txID,
			TotalDuration:        totalDuration,
			ConfirmationDuration: confirmationDuration,
		})
	}

	return w.Backend.AcceptAtomicTx(ctx, tx)
}

func (w *wallet) baseFee(options []walletutil.Option) (*big.Int, error) {
	ops := walletutil.NewOptions(options)
	baseFee := ops.BaseFee(nil)
	if baseFee != nil {
		return baseFee, nil
	}

	ctx := ops.Context()
	return w.ethClient.EstimateBaseFee(ctx)
}

// TODO: Upstream this function into geth.
func awaitTxAccepted(
	c client.Client,
	ctx context.Context,
	txID ids.ID,
	freq time.Duration,
	options ...rpc.Option,
) error {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		// TODO: GetAtomicTxStatus method has been removed from client.Client interface
		// status, err := c.GetAtomicTxStatus(ctx, txID, options...)
		// if err != nil {
		// 	return err
		// }
		status := atomic.Accepted

		if status == atomic.Accepted {
			return nil
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
