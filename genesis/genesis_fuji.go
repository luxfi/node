// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	_ "embed"

	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
)

var (
	//go:embed genesis_fuji.json
	fujiGenesisConfigJSON []byte

	// FujiParams are the params used for the fuji testnet
	FujiParams = Params{
		StaticConfig: fee.StaticConfig{
			TxFee:                         units.MilliLux,
			CreateAssetTxFee:              10 * units.MilliLux,
			CreateSubnetTxFee:             100 * units.MilliLux,
			TransformSubnetTxFee:          1 * units.Lux,
			CreateBlockchainTxFee:         100 * units.MilliLux,
			AddPrimaryNetworkValidatorFee: 0,
			AddPrimaryNetworkDelegatorFee: 0,
			AddSubnetValidatorFee:         units.MilliLux,
			AddSubnetDelegatorFee:         units.MilliLux,
		},
		StakingConfig: StakingConfig{
			UptimeRequirement: .8, // 80%
			MinValidatorStake: 1 * units.Lux,
			MaxValidatorStake: 3 * units.MegaLux,
			MinDelegatorStake: 1 * units.Lux,
			MinDelegationFee:  20000, // 2%
			MinStakeDuration:  24 * time.Hour,
			MaxStakeDuration:  365 * 24 * time.Hour,
			RewardConfig: reward.Config{
				MaxConsumptionRate: .12 * reward.PercentDenominator,
				MinConsumptionRate: .10 * reward.PercentDenominator,
				MintingPeriod:      365 * 24 * time.Hour,
				SupplyCap:          720 * units.MegaLux,
			},
		},
	}
)
