// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
)

func TestGetStakingConfig(t *testing.T) {
	tests := []struct {
		name      string
		networkID uint32
	}{
		{"Mainnet", constants.MainnetID},
		{"Testnet", constants.TestnetID},
		{"CustomID", constants.CustomID},
		{"Custom", 12345},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GetStakingConfig(tt.networkID)

			// Verify basic constraints
			require.GreaterOrEqual(t, cfg.UptimeRequirement, 0.0)
			require.LessOrEqual(t, cfg.UptimeRequirement, 1.0)
			require.Greater(t, cfg.MinValidatorStake, uint64(0))
			require.GreaterOrEqual(t, cfg.MaxValidatorStake, cfg.MinValidatorStake)
			require.Greater(t, cfg.MinDelegatorStake, uint64(0))
			require.Greater(t, cfg.MinStakeDuration, time.Duration(0))
			require.GreaterOrEqual(t, cfg.MaxStakeDuration, cfg.MinStakeDuration)

			// RewardConfig is populated by builder with node-specific types
			// The genesis package only provides the base staking parameters
		})
	}
}

func TestGetTxFeeConfig(t *testing.T) {
	tests := []struct {
		name      string
		networkID uint32
	}{
		{"Mainnet", constants.MainnetID},
		{"Testnet", constants.TestnetID},
		{"CustomID", constants.CustomID},
		{"Custom", 12345},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GetTxFeeConfig(tt.networkID)

			// Verify basic fee config
			require.Greater(t, cfg.TxFee, uint64(0))
			require.Greater(t, cfg.CreateAssetTxFee, uint64(0))

			// Verify dynamic fee config
			require.Greater(t, uint64(cfg.DynamicFeeConfig.MaxCapacity), uint64(0))
			require.Greater(t, uint64(cfg.DynamicFeeConfig.MaxPerSecond), uint64(0))

			// Verify validator fee config
			require.Greater(t, uint64(cfg.ValidatorFeeConfig.Capacity), uint64(0))
			require.Greater(t, uint64(cfg.ValidatorFeeConfig.Target), uint64(0))
		})
	}
}

func TestGetBootstrappers(t *testing.T) {
	tests := []struct {
		name      string
		networkID uint32
	}{
		{"Mainnet", constants.MainnetID},
		{"Testnet", constants.TestnetID},
		{"CustomID", constants.CustomID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bootstrappers, err := GetBootstrappers(tt.networkID)
			require.NoError(t, err)

			// Bootstrappers may not be configured for all networks
			// This is acceptable - they can be provided via config

			// Verify each bootstrapper has valid ID and IP
			for _, b := range bootstrappers {
				require.NotEqual(t, b.ID.String(), "")
				require.True(t, b.IP.IsValid())
			}
		})
	}
}

func TestSampleBootstrappers(t *testing.T) {
	tests := []struct {
		name      string
		networkID uint32
		count     int
	}{
		{"Mainnet_5", constants.MainnetID, 5},
		{"Mainnet_10", constants.MainnetID, 10},
		{"Testnet_3", constants.TestnetID, 3},
		{"Custom_0", constants.CustomID, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampled, err := SampleBootstrappers(tt.networkID, tt.count)
			require.NoError(t, err)

			// Should not exceed requested count
			require.LessOrEqual(t, len(sampled), tt.count)

			// Should not exceed available bootstrappers
			all, err := GetBootstrappers(tt.networkID)
			require.NoError(t, err)
			require.LessOrEqual(t, len(sampled), len(all))
		})
	}
}

func TestGetConfig(t *testing.T) {
	tests := []struct {
		name      string
		networkID uint32
	}{
		{"Mainnet", constants.MainnetID},
		{"Testnet", constants.TestnetID},
		{"MainnetChainID", constants.MainnetChainID},
		{"TestnetChainID", constants.TestnetChainID},
		{"CustomID", constants.CustomID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GetConfig(tt.networkID)
			require.NotNil(t, cfg)
			// Config should have a valid NetworkID
			require.Greater(t, cfg.NetworkID, uint32(0))
		})
	}
}

func TestVMAliases(t *testing.T) {
	// Verify all expected VMs have aliases
	require.Contains(t, VMAliases, constants.PlatformVMID)
	require.Contains(t, VMAliases, constants.XVMID)
	require.Contains(t, VMAliases, constants.EVMID)

	// Verify aliases are non-empty
	for vmID, aliases := range VMAliases {
		require.NotEmpty(t, aliases, "VM %s should have aliases", vmID)
	}
}

func TestChainAliases(t *testing.T) {
	require.NotEmpty(t, PChainAliases)
	require.NotEmpty(t, XChainAliases)
	require.NotEmpty(t, CChainAliases)

	require.Contains(t, PChainAliases, "P")
	require.Contains(t, XChainAliases, "X")
	require.Contains(t, CChainAliases, "C")
}

func TestDefaultFeeConfigs(t *testing.T) {
	// Test dynamic fee configs
	configs := []struct {
		name   string
		config interface{}
	}{
		{"MainnetDynamic", MainnetDynamicFeeConfig},
		{"TestnetDynamic", TestnetDynamicFeeConfig},
		{"LocalDynamic", LocalDynamicFeeConfig},
		{"MainnetValidator", MainnetValidatorFeeConfig},
		{"TestnetValidator", TestnetValidatorFeeConfig},
		{"LocalValidator", LocalValidatorFeeConfig},
	}

	for _, tt := range configs {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.config)
		})
	}
}
