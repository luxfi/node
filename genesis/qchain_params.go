// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
)

// QChainParams defines the parameters for Q-Chain
var (
	QChainMainnetParams = Params{
		StakingConfig: StakingConfig{
			UptimeRequirement: .8, // 80%
			MinValidatorStake: 100 * units.Lux,
			MaxValidatorStake: 10_000 * units.Lux,
			MinDelegatorStake: 25 * units.Lux,
			MinDelegationFee:  20000, // 2%
			MinStakeDuration:  14 * 24 * time.Hour,
			MaxStakeDuration:  365 * 24 * time.Hour,
			RewardConfig: reward.Config{
				MaxConsumptionRate: .12 * reward.PercentDenominator,
				MinConsumptionRate: .10 * reward.PercentDenominator,
				MintingPeriod:      365 * 24 * time.Hour,
				SupplyCap:          1_000_000_000 * units.Lux, // 1 billion Q tokens
			},
		},
		StaticConfig: fee.StaticConfig{
			TxFee:                         units.MilliLux,
			CreateAssetTxFee:              10 * units.MilliLux,
			CreateNetTxFee:                100 * units.Lux,
			TransformNetTxFee:             100 * units.Lux,
			CreateBlockchainTxFee:         100 * units.Lux,
			AddPrimaryNetworkValidatorFee: 0,
			AddPrimaryNetworkDelegatorFee: 0,
			AddNetValidatorFee:            units.MilliLux,
			AddNetDelegatorFee:            units.MilliLux,
		},
	}

	QChainTestnetParams = Params{
		StakingConfig: StakingConfig{
			UptimeRequirement: .6, // 60% for testnet
			MinValidatorStake: 10 * units.Lux,
			MaxValidatorStake: 1_000 * units.Lux,
			MinDelegatorStake: 1 * units.Lux,
			MinDelegationFee:  10000, // 1%
			MinStakeDuration:  24 * time.Hour,
			MaxStakeDuration:  90 * 24 * time.Hour,
			RewardConfig: reward.Config{
				MaxConsumptionRate: .20 * reward.PercentDenominator,
				MinConsumptionRate: .15 * reward.PercentDenominator,
				MintingPeriod:      90 * 24 * time.Hour,
				SupplyCap:          100_000_000 * units.Lux, // 100 million for testnet
			},
		},
		StaticConfig: fee.StaticConfig{
			TxFee:                         units.Lux / 1000,
			CreateAssetTxFee:              units.Lux / 100,
			CreateNetTxFee:                10 * units.Lux,
			TransformNetTxFee:             10 * units.Lux,
			CreateBlockchainTxFee:         10 * units.Lux,
			AddPrimaryNetworkValidatorFee: 0,
			AddPrimaryNetworkDelegatorFee: 0,
			AddNetValidatorFee:            units.Lux / 1000,
			AddNetDelegatorFee:            units.Lux / 1000,
		},
	}
)

// QChainConfig represents Q-Chain specific configuration
type QChainConfig struct {
	// Quantum-resistant parameters
	QuantumEnabled     bool
	SignatureAlgorithm string
	HashFunction       string
	MinValidators      int
	MaxValidators      int
	ConsensusThreshold int

	// Chain identification
	ChainID    ids.ID
	ChainAlias string
	VMID       ids.ID

	// Network parameters
	NetworkID uint32
}

// GetQChainParams returns the Q-Chain parameters for the given network ID
func GetQChainParams(networkID uint32) Params {
	switch networkID {
	case constants.QChainMainnetID:
		return QChainMainnetParams
	case constants.QChainTestnetID:
		return QChainTestnetParams
	default:
		return QChainTestnetParams
	}
}

// GetQChainConfig returns the Q-Chain configuration for the given network ID
func GetQChainConfig(networkID uint32) QChainConfig {
	switch networkID {
	case constants.QChainMainnetID:
		return QChainConfig{
			QuantumEnabled:     true,
			SignatureAlgorithm: "dilithium3",
			HashFunction:       "sha3-256",
			MinValidators:      5,
			MaxValidators:      100,
			ConsensusThreshold: 3,
			ChainID:            constants.QChainID,
			ChainAlias:         "Q",
			VMID:               constants.QVMID,
			NetworkID:          constants.QChainMainnetID,
		}
	case constants.QChainTestnetID:
		return QChainConfig{
			QuantumEnabled:     true,
			SignatureAlgorithm: "dilithium3",
			HashFunction:       "sha3-256",
			MinValidators:      3,
			MaxValidators:      50,
			ConsensusThreshold: 2,
			ChainID:            constants.QChainID,
			ChainAlias:         "Q",
			VMID:               constants.QVMID,
			NetworkID:          constants.QChainTestnetID,
		}
	default:
		return QChainConfig{
			QuantumEnabled:     false,
			MinValidators:      1,
			MaxValidators:      10,
			ConsensusThreshold: 1,
			ChainID:            constants.QChainID,
			ChainAlias:         "Q",
			VMID:               constants.QVMID,
			NetworkID:          networkID,
		}
	}
}
