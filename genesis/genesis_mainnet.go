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
	//go:embed genesis_mainnet.json
	mainnetGenesisConfigJSON []byte

	// MainnetParams are the params used for mainnet
	MainnetParams = Params{
		StaticConfig: fee.StaticConfig{
			TxFee:                         units.MilliLux,
			CreateAssetTxFee:              10 * units.MilliLux,
			CreateSubnetTxFee:             1 * units.Lux,
			TransformSubnetTxFee:          10 * units.Lux,
			CreateBlockchainTxFee:         1 * units.Lux,
			AddPrimaryNetworkValidatorFee: 0,
			AddPrimaryNetworkDelegatorFee: 0,
			AddSubnetValidatorFee:         units.MilliLux,
			AddSubnetDelegatorFee:         units.MilliLux,
		},
		StakingConfig: StakingConfig{
			UptimeRequirement: .8, // 80%
			MinValidatorStake: 2 * units.KiloLux,
			MaxValidatorStake: 3 * units.MegaLux,
			MinDelegatorStake: 25 * units.Lux,
			MinDelegationFee:  20000, // 2%
			MinStakeDuration:  2 * 7 * 24 * time.Hour,
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
