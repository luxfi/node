// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	"time"

	"github.com/luxfi/consensus"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/wallet/chain/p/builder"
)

func newContext(
	ctx context.Context,
	networkID uint32,
	luxAssetID ids.ID,
	cfg *config.Config,
	timestamp time.Time,
) *builder.Context {
	var (
		feeCalc         = fee.NewStaticCalculator(cfg.StaticFeeConfig, cfg.UpgradeConfig)
		createSubnetFee = feeCalc.CalculateFee(&txs.CreateSubnetTx{}, timestamp)
		createChainFee  = feeCalc.CalculateFee(&txs.CreateChainTx{}, timestamp)
	)

	// Get chain ID from context
	chainID := ids.Empty
	if ctx != nil {
		// Try to get chain ID from consensus context
		chainID = consensus.GetChainID(ctx)
	}

	return &builder.Context{
		NetworkID:                     networkID,
		BlockchainID:                  chainID,
		LUXAssetID:                    luxAssetID,
		BaseTxFee:                     cfg.StaticFeeConfig.TxFee,
		CreateSubnetTxFee:             createSubnetFee,
		TransformSubnetTxFee:          cfg.StaticFeeConfig.TransformSubnetTxFee,
		CreateBlockchainTxFee:         createChainFee,
		AddPrimaryNetworkValidatorFee: cfg.StaticFeeConfig.AddPrimaryNetworkValidatorFee,
		AddPrimaryNetworkDelegatorFee: cfg.StaticFeeConfig.AddPrimaryNetworkDelegatorFee,
		AddSubnetValidatorFee:         cfg.StaticFeeConfig.AddSubnetValidatorFee,
		AddSubnetDelegatorFee:         cfg.StaticFeeConfig.AddSubnetDelegatorFee,
	}
}
