// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
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
	//go:embed genesis_testnet.json
	testnetGenesisConfigJSON []byte

	// TestnetParams are the params used for the testnet
	TestnetParams = Params{
		StaticConfig: fee.StaticConfig{
			TxFee:                         units.MilliLux,
			CreateAssetTxFee:              10 * units.MilliLux,
			CreateNetTxFee:             100 * units.MilliLux,
			TransformNetTxFee:          1000 * units.MilliLux,
			CreateBlockchainTxFee:         100 * units.MilliLux,
			AddPrimaryNetworkValidatorFee: 0,
			AddPrimaryNetworkDelegatorFee: 0,
			AddNetValidatorFee:         units.MilliLux,
			AddNetDelegatorFee:         units.MilliLux,
		},
		StakingConfig: StakingConfig{
			UptimeRequirement: .8, // 80%
			MinValidatorStake: 1000 * units.Lux,    // 1000 LUX for testnet
			MaxValidatorStake: 10 * units.MegaLux,  // 10M LUX maximum
			MinDelegatorStake: 100 * units.Lux,     // 100 LUX minimum delegation
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
