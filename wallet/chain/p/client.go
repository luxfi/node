// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/luxfi/node/v2/vms/platformvm"
	"github.com/luxfi/node/v2/vms/platformvm/txs"
	wallet "github.com/luxfi/node/v2/wallet"
	"github.com/luxfi/node/v2/wallet/chain/p/builder"
	pwallet "github.com/luxfi/node/v2/wallet/chain/p/wallet"
)

var _ pwallet.Client = (*Client)(nil)

func NewClient(
	c *platformvm.Client,
	b pwallet.Backend,
) *Client {
	return &Client{
		client:  c,
		backend: b,
	}
}

type Client struct {
	client  *platformvm.Client
	backend pwallet.Backend
}

func (c *Client) IssueTx(
	tx *txs.Tx,
	options ...wallet.Option,
) error {
	ops := wallet.NewOptions(options)
	ctx := ops.Context()
	startTime := time.Now()
	txID, err := c.client.IssueTx(ctx, tx.Bytes())
	if err != nil {
		return err
	}

	issuanceDuration := time.Since(startTime)
	if f := ops.IssuanceHandler(); f != nil {
		f(wallet.IssuanceReceipt{
			ChainAlias: builder.Alias,
			TxID:       txID,
			Duration:   issuanceDuration,
		})
	}

	if ops.AssumeDecided() {
		return c.backend.AcceptTx(ctx, tx)
	}

	if err := platformvm.AwaitTxAccepted(c.client, ctx, txID, ops.PollFrequency()); err != nil {
		return err
	}

	if f := ops.ConfirmationHandler(); f != nil {
		totalDuration := time.Since(startTime)
		confirmationDuration := totalDuration - issuanceDuration

		f(wallet.ConfirmationReceipt{
			ChainAlias:           builder.Alias,
			TxID:                 txID,
			TotalDuration:        totalDuration,
			ConfirmationDuration: confirmationDuration,
		})
	}

	return c.backend.AcceptTx(ctx, tx)
}
