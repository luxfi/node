// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	
	"github.com/luxfi/consensus"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/wallet/chain/x/builder"
)

func newContext(
	ctx context.Context,
	cfg *config.Config,
	feeAssetID ids.ID,
) *builder.Context {
	// Extract IDs from context
	networkID := consensus.GetNetworkID(ctx)
	chainID := consensus.GetChainID(ctx)
	
	return &builder.Context{
		NetworkID:        networkID,
		BlockchainID:     chainID,
		LUXAssetID:       feeAssetID,
		BaseTxFee:        cfg.TxFee,
		CreateAssetTxFee: cfg.CreateAssetTxFee,
	}
}
