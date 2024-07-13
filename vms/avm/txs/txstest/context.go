// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/vms/avm/config"
	"github.com/luxfi/node/wallet/chain/x/builder"
)

func newContext(
	ctx *snow.Context,
	cfg *config.Config,
	feeAssetID ids.ID,
) *builder.Context {
	return &builder.Context{
		NetworkID:        ctx.NetworkID,
		BlockchainID:     ctx.XChainID,
		LUXAssetID:      feeAssetID,
		BaseTxFee:        cfg.TxFee,
		CreateAssetTxFee: cfg.CreateAssetTxFee,
	}
}
