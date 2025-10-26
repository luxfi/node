<<<<<<< HEAD:vms/avm/wallet_client.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/wallet_client.go
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/api"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/formatting"
	"github.com/luxfi/node/utils/rpc"
)

<<<<<<< HEAD:vms/avm/wallet_client.go
// WalletClient for interacting with avm managed wallet.
=======
var _ WalletClient = (*client)(nil)

// interface of an XVM wallet client for interacting with xvm managed wallet on [chain]
type WalletClient interface {
	// IssueTx issues a transaction to a node and returns the TxID
	IssueTx(ctx context.Context, tx []byte, options ...rpc.Option) (ids.ID, error)
	// Send [amount] of [assetID] to address [to]
	//
	// Deprecated: Transactions should be issued using the
	// `node/wallet/chain/x.Wallet` utility.
	Send(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		amount uint64,
		assetID string,
		to ids.ShortID,
		memo string,
		options ...rpc.Option,
	) (ids.ID, error)
	// SendMultiple sends a transaction from [user] funding all [outputs]
	//
	// Deprecated: Transactions should be issued using the
	// `node/wallet/chain/x.Wallet` utility.
	SendMultiple(
		ctx context.Context,
		user api.UserPass,
		from []ids.ShortID,
		changeAddr ids.ShortID,
		outputs []ClientSendOutput,
		memo string,
		options ...rpc.Option,
	) (ids.ID, error)
}

// implementation of an XVM wallet client for interacting with xvm managed wallet on [chain]
type walletClient struct {
	requester rpc.EndpointRequester
}

// NewWalletClient returns an XVM wallet client for interacting with xvm managed wallet on [chain]
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/wallet_client.go
//
// Deprecated: Transactions should be issued using the
// `node/wallet/chain/x.Wallet` utility.
type WalletClient struct {
	Requester rpc.EndpointRequester
}

// NewWalletClient returns an AVM wallet client for interacting with avm managed
// wallet
//
// Deprecated: Transactions should be issued using the
// `node/wallet/chain/x.Wallet` utility.
func NewWalletClient(uri, chain string) *WalletClient {
	path := fmt.Sprintf(
		"%s/ext/%s/%s/wallet",
		uri,
		constants.ChainAliasPrefix,
		chain,
	)
	return &WalletClient{
		Requester: rpc.NewEndpointRequester(path),
	}
}

// IssueTx issues a transaction to a node and returns the TxID
func (c *WalletClient) IssueTx(ctx context.Context, txBytes []byte, options ...rpc.Option) (ids.ID, error) {
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
	if err != nil {
		return ids.Empty, err
	}
	res := &api.JSONTxID{}
	err = c.Requester.SendRequest(ctx, "wallet.issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res, options...)
	return res.TxID, err
}
