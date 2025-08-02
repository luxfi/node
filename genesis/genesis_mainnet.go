// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	_ "embed"

	"github.com/luxfi/node/v2/quasar/sampling"
	"github.com/luxfi/node/v2/utils/units"
	"github.com/luxfi/node/v2/vms/components/gas"
	"github.com/luxfi/node/v2/vms/platformvm/reward"
	"github.com/luxfi/node/v2/vms/platformvm/validators/fee"
)

var (
	//go:embed genesis_mainnet.json
	mainnetGenesisConfigJSON []byte

	// MainnetConsensusParameters are the consensus parameters for mainnet (21 nodes)
	MainnetConsensusParameters = sampling.Parameters{
		K:               21, // Sample all 21 nodes
		AlphaPreference: 13, // ~62% quorum - can tolerate up to 8 failures
		AlphaConfidence: 18, // ~86% quorum - can tolerate up to 3 failures
		Beta:            8,  // 8 rounds → 8×50ms + 100ms = 500ms finality
		ConcurrentRepolls: 8, // Pipeline all 8 rounds for maximum throughput
	}

	// MainnetParams are the params used for mainnet
	MainnetParams = Params{
		ConsensusParameters: MainnetConsensusParameters,
		TxFeeConfig: TxFeeConfig{
			CreateAssetTxFee: 10 * units.MilliLux,
			TxFee:            units.MilliLux,
			DynamicFeeConfig: gas.Config{
				Weights: gas.Dimensions{
					gas.Bandwidth: 1,     // Max block size ~1MB
					gas.DBRead:    1_000, // Max reads per block 1,000
					gas.DBWrite:   1_000, // Max writes per block 1,000
					gas.Compute:   4,     // Max compute time per block ~250ms
				},
				MaxCapacity:     1_000_000,
				MaxPerSecond:    100_000, // Refill time 10s
				TargetPerSecond: 50_000,  // Target is half of max
				MinPrice:        1,
				// ExcessConversionConstant = (MaxPerSecond - TargetPerSecond) * NumberOfSecondsPerDoubling / ln(2)
				//
				// ln(2) is a float and the result is consensus critical, so we
				// hardcode the result.
				ExcessConversionConstant: 2_164_043, // Double every 30s
			},
			ValidatorFeeConfig: fee.Config{
				Capacity: 20_000,
				Target:   10_000,
				MinPrice: gas.Price(512 * units.NanoLux),
				// ExcessConversionConstant = (Capacity - Target) * NumberOfSecondsPerDoubling / ln(2)
				//
				// ln(2) is a float and the result is consensus critical, so we
				// hardcode the result.
				ExcessConversionConstant: 1_246_488_515, // Double every day
			},
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
