// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	_ "embed"

	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/validators/fee"
)

var (
	//go:embed genesis_testnet.json
	testnetGenesisConfigJSON []byte

	// TestnetParams are the params used for the testnet
	TestnetParams = Params{
		TxFeeConfig: TxFeeConfig{
			CreateAssetTxFee: 10 * units.MilliLux,
			TxFee:            units.MilliLux,
			DynamicFeeConfig: gas.Config{
				Weights: gas.Dimensions{
					gas.Bandwidth: 1,
					gas.DBRead:    1_000,
					gas.DBWrite:   1_000,
					gas.Compute:   4,
				},
				MaxCapacity:              1_000_000,
				MaxPerSecond:             100_000,
				TargetPerSecond:          50_000,
				MinPrice:                 1,
				ExcessConversionConstant: 2_164_043,
			},
			ValidatorFeeConfig: fee.Config{
				Capacity:                 20_000,
				Target:                   10_000,
				MinPrice:                 gas.Price(512 * units.NanoLux),
				ExcessConversionConstant: 1_246_488_515,
			},
		},
		StakingConfig: StakingConfig{
			UptimeRequirement: .8,
			MinValidatorStake: 1 * units.MegaLux,
			MaxValidatorStake: 100 * units.MegaLux,
			MinDelegatorStake: 1 * units.KiloLux,
			MinDelegationFee:  20000,
			MinStakeDuration:  24 * time.Hour,
			MaxStakeDuration:  365 * 24 * time.Hour,
			RewardConfig: reward.Config{
				MaxConsumptionRate: .12 * reward.PercentDenominator,
				MinConsumptionRate: .10 * reward.PercentDenominator,
				MintingPeriod:      365 * 24 * time.Hour,
				SupplyCap:          2000 * units.MegaLux,
			},
		},
	}
)
