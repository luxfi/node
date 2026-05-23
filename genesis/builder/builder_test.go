// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/genesis"
	"github.com/luxfi/node/vms/platformvm/txs"

	genesiscfg "github.com/luxfi/genesis/pkg/genesis"
)

func TestGetStakingConfig(t *testing.T) {
	tests := []struct {
		name      string
		networkID uint32
	}{
		{"Mainnet", constants.MainnetID},
		{"Testnet", constants.TestnetID},
		{"CustomID", constants.LocalID},
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
		{"CustomID", constants.LocalID},
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
		{"Devnet", constants.DevnetID},
		{"CustomID", constants.LocalID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bootstrappers, err := GetBootstrappers(tt.networkID)
			require.NoError(t, err)

			// Bootstrappers may not be configured for all networks
			// This is acceptable - they can be provided via config

			// Verify each bootstrapper has valid ID and endpoint
			for _, b := range bootstrappers {
				require.NotEqual(t, b.ID.String(), "")
				require.True(t, b.Endpoint.Port > 0, "endpoint port must be non-zero")
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
		{"Custom_0", constants.LocalID, 0},
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
		{"CustomID", constants.LocalID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GetConfig(tt.networkID)
			require.NotNil(t, cfg)
			require.Greater(t, cfg.NetworkID, uint32(0))
		})
	}
}

func TestGetConfigAllocations(t *testing.T) {
	tests := []struct {
		name         string
		networkID    uint32
		minAllocs    int
		minStakers   int
	}{
		{"Mainnet", constants.MainnetID, 50, 1},
		{"Testnet", constants.TestnetID, 50, 1},
		{"Devnet", constants.DevnetID, 50, 1},
		{"Local", constants.LocalID, 50, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GetConfig(tt.networkID)
			require.NotNil(t, cfg)
			require.GreaterOrEqual(t, len(cfg.Allocations), tt.minAllocs,
				"network %d must have at least %d allocations", tt.networkID, tt.minAllocs)
			require.GreaterOrEqual(t, len(cfg.InitialStakers), tt.minStakers,
				"network %d must have at least %d stakers", tt.networkID, tt.minStakers)
		})
	}
}

func TestFromConfigNonZeroSupply(t *testing.T) {
	for _, networkID := range []uint32{constants.MainnetID, constants.TestnetID, constants.DevnetID, constants.LocalID} {
		t.Run(fmt.Sprintf("network_%d", networkID), func(t *testing.T) {
			cfg := GetConfig(networkID)
			require.NotNil(t, cfg)

			genesisBytes, _, err := FromConfig(cfg)
			require.NoError(t, err)
			require.NotEmpty(t, genesisBytes)
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

func TestFromConfigExplicitStakers(t *testing.T) {
	require := require.New(t)

	// Build 5 stakers with explicit Weight and NodeID (no BLS keys)
	nodeIDs := []string{
		"NodeID-7D3wajA7bNpfyHpfEtkjUF1KcuhFEfPbZ",
		"NodeID-A6d3tQtteyaBCiffij3ohxxgq17DdvChs",
		"NodeID-Kv37z2BuRsGPhYNeCyktfgeYHK5QbZMUu",
		"NodeID-HvrW5UP1SdR8ujEHzCdUurhvUj1Liydxo",
		"NodeID-LBhpNZ7Sf9gpT1gJv2MKKS4Yd2SibTVeK",
	}

	var secpAddr ids.ShortID
	stakerHash := hash.ComputeHash160([]byte("test-staker-v1"))
	copy(secpAddr[:], stakerHash)

	startTime := uint64(time.Now().Unix())
	const hundredYears = 100 * 365 * 24 * 60 * 60
	const oneBillionLUX = 1_000_000_000_000_000_000
	const oneMillionLUX = 1_000_000_000_000_000

	stakers := make([]genesiscfg.Staker, len(nodeIDs))
	for i, nidStr := range nodeIDs {
		nodeID, err := ids.NodeIDFromString(nidStr)
		require.NoError(err)
		stakers[i] = genesiscfg.Staker{
			NodeID:        nodeID,
			RewardAddress: secpAddr,
			DelegationFee: 1000000,
			Weight:        oneBillionLUX / uint64(len(nodeIDs)),
			StartTime:     startTime,
			EndTime:       startTime + hundredYears,
		}
	}

	var ethShortID ids.ShortID

	cfg := &genesiscfg.Config{
		NetworkID: constants.LocalID,
		Allocations: []genesiscfg.Allocation{
			{
				EVMAddr:       secpAddr,
				UTXOAddr:      secpAddr,
				InitialAmount: 0,
				UnlockSchedule: []genesiscfg.LockedAmount{
					{Amount: oneBillionLUX, Locktime: 0},
				},
			},
			{
				EVMAddr:       ethShortID,
				UTXOAddr:      ethShortID,
				InitialAmount: oneMillionLUX,
			},
		},
		StartTime:                  startTime,
		InitialStakeDuration:       hundredYears,
		InitialStakeDurationOffset: 0,
		InitialStakedFunds:         []ids.ShortID{secpAddr},
		InitialStakers:             stakers,
		CChainGenesis:              `{"config":{"chainId":31337},"alloc":{}}`,
		Message:                    "Test Genesis",
	}

	genesisBytes, _, err := FromConfig(cfg)
	require.NoError(err)
	require.NotEmpty(genesisBytes)

	// Parse genesis and verify validators
	parsed, err := genesis.Parse(genesisBytes)
	require.NoError(err)
	require.Len(parsed.Validators, 5, "expected 5 validators in genesis")

	for i, vdrTx := range parsed.Validators {
		switch ut := vdrTx.Unsigned.(type) {
		case *txs.AddValidatorTx:
			require.Equal(stakers[i].Weight, ut.Wght,
				"validator %d weight mismatch", i)
			t.Logf("Validator %d: NodeID=%s Weight=%d StakeOuts=%d",
				i, ut.Validator.NodeID, ut.Wght, len(ut.StakeOuts))
		case *txs.AddPermissionlessValidatorTx:
			require.Equal(stakers[i].Weight, ut.Wght,
				"validator %d weight mismatch", i)
			t.Logf("Validator %d: NodeID=%s Weight=%d StakeOuts=%d",
				i, ut.Validator.NodeID, ut.Wght, len(ut.StakeOuts))
		default:
			t.Fatalf("unexpected validator tx type: %T", ut)
		}
	}
}

// TestFromConfigExplicitStakersNoStakedFunds reproduces the bug: when
// InitialStakedFunds is empty but stakers have explicit Weight, the validators
// end up with empty StakeOuts and the P-Chain rejects them.
func TestFromConfigExplicitStakersNoStakedFunds(t *testing.T) {
	require := require.New(t)

	nodeIDs := []string{
		"NodeID-7D3wajA7bNpfyHpfEtkjUF1KcuhFEfPbZ",
		"NodeID-A6d3tQtteyaBCiffij3ohxxgq17DdvChs",
		"NodeID-Kv37z2BuRsGPhYNeCyktfgeYHK5QbZMUu",
		"NodeID-HvrW5UP1SdR8ujEHzCdUurhvUj1Liydxo",
		"NodeID-LBhpNZ7Sf9gpT1gJv2MKKS4Yd2SibTVeK",
	}

	var deployerAddr ids.ShortID
	deployerHash := hash.ComputeHash160([]byte("test-deployer"))
	copy(deployerAddr[:], deployerHash)

	startTime := uint64(time.Now().Unix())
	const hundredYears = 100 * 365 * 24 * 60 * 60
	const oneBillionLUX = 1_000_000_000_000_000_000
	const oneMillionLUX = 1_000_000_000_000_000

	stakers := make([]genesiscfg.Staker, len(nodeIDs))
	for i, nidStr := range nodeIDs {
		nodeID, err := ids.NodeIDFromString(nidStr)
		require.NoError(err)
		stakers[i] = genesiscfg.Staker{
			NodeID:        nodeID,
			RewardAddress: deployerAddr,
			DelegationFee: 1000000,
			Weight:        oneBillionLUX / uint64(len(nodeIDs)),
			StartTime:     startTime,
			EndTime:       startTime + hundredYears,
		}
	}

	// No InitialStakedFunds -- stakers have explicit Weight only
	cfg := &genesiscfg.Config{
		NetworkID: constants.LocalID,
		Allocations: []genesiscfg.Allocation{
			{
				EVMAddr:       deployerAddr,
				UTXOAddr:      deployerAddr,
				InitialAmount: oneMillionLUX,
			},
		},
		StartTime:                  startTime,
		InitialStakeDuration:       hundredYears,
		InitialStakeDurationOffset: 0,
		InitialStakedFunds:         nil, // No staked funds
		InitialStakers:             stakers,
		CChainGenesis:              `{"config":{"chainId":31337},"alloc":{}}`,
		Message:                    "Test Genesis No Staked Funds",
	}

	genesisBytes, _, err := FromConfig(cfg)
	require.NoError(err)
	require.NotEmpty(genesisBytes)

	parsed, err := genesis.Parse(genesisBytes)
	require.NoError(err)
	require.Len(parsed.Validators, 5, "expected 5 validators in genesis")

	for i, vdrTx := range parsed.Validators {
		switch ut := vdrTx.Unsigned.(type) {
		case *txs.AddValidatorTx:
			require.Greater(ut.Wght, uint64(0), "validator %d weight must be non-zero", i)
			require.Greater(len(ut.StakeOuts), 0, "validator %d must have stake outputs", i)
			t.Logf("Validator %d: NodeID=%s Weight=%d StakeOuts=%d",
				i, ut.Validator.NodeID, ut.Wght, len(ut.StakeOuts))
		case *txs.AddPermissionlessValidatorTx:
			require.Greater(ut.Wght, uint64(0), "validator %d weight must be non-zero", i)
			require.Greater(len(ut.StakeOuts), 0, "validator %d must have stake outputs", i)
			t.Logf("Validator %d: NodeID=%s Weight=%d StakeOuts=%d",
				i, ut.Validator.NodeID, ut.Wght, len(ut.StakeOuts))
		default:
			t.Fatalf("unexpected validator tx type: %T", ut)
		}
	}
}
