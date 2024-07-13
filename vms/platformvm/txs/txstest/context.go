// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"time"

	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/wallet/chain/p/builder"
)

func newContext(
	ctx *snow.Context,
	cfg *config.Config,
	timestamp time.Time,
) *builder.Context {
	var (
		feeCalc         = fee.NewStaticCalculator(cfg.StaticFeeConfig, cfg.UpgradeConfig)
		createSubnetFee = feeCalc.CalculateFee(&txs.CreateSubnetTx{}, timestamp)
		createChainFee  = feeCalc.CalculateFee(&txs.CreateChainTx{}, timestamp)
	)

	return &builder.Context{
		NetworkID:                     ctx.NetworkID,
		LUXAssetID:                   ctx.LUXAssetID,
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
