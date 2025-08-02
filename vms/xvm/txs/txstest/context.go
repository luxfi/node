// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/vms/xvm/config"
	"github.com/luxfi/node/v2/wallet/chain/x/builder"
)

func newContext(
	ctx *quasar.Context,
	cfg *config.Config,
	feeAssetID ids.ID,
) *builder.Context {
	return &builder.Context{
		NetworkID:        ctx.NetworkID,
		BlockchainID:     ctx.ChainID,
		LUXAssetID:       feeAssetID,
		BaseTxFee:        cfg.TxFee,
		CreateAssetTxFee: cfg.CreateAssetTxFee,
	}
}
