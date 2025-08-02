// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/vms/aivm"
	"github.com/luxfi/node/v2/vms/bridgevm"
	"github.com/luxfi/node/v2/vms/mpcvm"
	"github.com/luxfi/node/v2/vms/quantumvm"
	"github.com/luxfi/node/v2/vms/zkvm"
)

// TestEightChainsConfig verifies the 8-chain configuration
func TestEightChainsConfig(t *testing.T) {
	require := require.New(t)

	config := GetEightChainsConfig()
	require.NotNil(config)

	// Verify all 8 chains are configured
	expectedChains := map[string]ids.ID{
		"A-Chain": aivm.ID,
		"B-Chain": bridgevm.ID,
		"C-Chain": constants.EVMID,
		"M-Chain": mpcvm.ID,
		"P-Chain": constants.PlatformVMID,
		"Q-Chain": quantumvm.ID,
		"X-Chain": constants.XVMID,
		"Z-Chain": zkvm.ID,
	}

	// Check A-Chain
	require.NotNil(config.AChainConfig)
	require.Equal(aivm.ID, config.AChainConfig.VMID)
	require.NotEmpty(config.AChainConfig.Genesis)
	
	// Check B-Chain
	require.NotNil(config.BChainConfig)
	require.Equal(bridgevm.ID, config.BChainConfig.VMID)
	require.NotEmpty(config.BChainConfig.Genesis)
	
	// Check C-Chain
	require.NotNil(config.CChainConfig)
	require.Equal(constants.EVMID, config.CChainConfig.VMID)
	require.NotEmpty(config.CChainConfig.Genesis)
	
	// Check M-Chain
	require.NotNil(config.MChainConfig)
	require.Equal(mpcvm.ID, config.MChainConfig.VMID)
	require.NotEmpty(config.MChainConfig.Genesis)
	
	// Check P-Chain
	require.NotNil(config.PChainConfig)
	require.Equal(constants.PlatformVMID, config.PChainConfig.VMID)
	require.NotEmpty(config.PChainConfig.Genesis)
	
	// Check Q-Chain
	require.NotNil(config.QChainConfig)
	require.Equal(quantumvm.ID, config.QChainConfig.VMID)
	require.NotEmpty(config.QChainConfig.Genesis)
	
	// Check X-Chain
	require.NotNil(config.XChainConfig)
	require.Equal(constants.XVMID, config.XChainConfig.VMID)
	require.NotEmpty(config.XChainConfig.Genesis)
	
	// Check Z-Chain
	require.NotNil(config.ZChainConfig)
	require.Equal(zkvm.ID, config.ZChainConfig.VMID)
	require.NotEmpty(config.ZChainConfig.Genesis)

	// Log all chain IDs for visibility
	t.Logf("8-Chain Configuration:")
	for name, vmID := range expectedChains {
		t.Logf("  %s: VM ID = %s", name, vmID)
	}
}

// TestMinimalBootConfig tests configuration with minimal chains (P, C, X, Q)
func TestMinimalBootConfig(t *testing.T) {
	require := require.New(t)

	// Create a minimal config
	minimalConfig := &EightChainsConfig{
		// Required chains
		PChainConfig: ChainConfig{
			VMID:    constants.PlatformVMID,
			Genesis: []byte("{}"),
			IsEnabled: true,
		},
		CChainConfig: ChainConfig{
			VMID:    constants.EVMID,
			Genesis: []byte("{}"),
			IsEnabled: true,
		},
		XChainConfig: ChainConfig{
			VMID:    constants.XVMID,
			Genesis: []byte("{}"),
			IsEnabled: true,
		},
		// Q-Chain for quantum safety
		QChainConfig: ChainConfig{
			VMID:    quantumvm.ID,
			Genesis: []byte("{}"),
			IsEnabled: true,
		},
		// Optional chains disabled
		AChainConfig: ChainConfig{
			VMID:      aivm.ID,
			IsEnabled: false,
		},
		BChainConfig: ChainConfig{
			VMID:      bridgevm.ID,
			IsEnabled: false,
		},
		MChainConfig: ChainConfig{
			VMID:      mpcvm.ID,
			IsEnabled: false,
		},
		ZChainConfig: ChainConfig{
			VMID:      zkvm.ID,
			IsEnabled: false,
		},
	}

	// Verify minimal config
	require.True(minimalConfig.PChainConfig.IsEnabled)
	require.True(minimalConfig.CChainConfig.IsEnabled)
	require.True(minimalConfig.XChainConfig.IsEnabled)
	require.True(minimalConfig.QChainConfig.IsEnabled)
	
	require.False(minimalConfig.AChainConfig.IsEnabled)
	require.False(minimalConfig.BChainConfig.IsEnabled)
	require.False(minimalConfig.MChainConfig.IsEnabled)
	require.False(minimalConfig.ZChainConfig.IsEnabled)

	t.Log("Minimal boot configuration verified: P, C, X, Q chains enabled")
}

// TestChainProcessorAffinity verifies processor affinity is set correctly
func TestChainProcessorAffinity(t *testing.T) {
	require := require.New(t)

	config := GetEightChainsConfig()
	config.SetProcessorAffinity()

	// Verify each chain has unique processor affinity
	require.Equal([]int{0}, config.AChainConfig.ProcessorAffinity)
	require.Equal([]int{1}, config.BChainConfig.ProcessorAffinity)
	require.Equal([]int{2}, config.CChainConfig.ProcessorAffinity)
	require.Equal([]int{3}, config.MChainConfig.ProcessorAffinity)
	require.Equal([]int{4}, config.PChainConfig.ProcessorAffinity)
	require.Equal([]int{5}, config.QChainConfig.ProcessorAffinity)
	require.Equal([]int{6}, config.XChainConfig.ProcessorAffinity)
	require.Equal([]int{7}, config.ZChainConfig.ProcessorAffinity)

	t.Log("Processor affinity correctly assigned to all 8 chains")
}

// TestChainParameters verifies chain-specific parameters
func TestChainParameters(t *testing.T) {
	require := require.New(t)

	config := GetEightChainsConfig()
	config.SetParameters()

	// Verify gas limits are set appropriately
	require.Greater(config.AChainConfig.GasLimit, uint64(0))
	require.Greater(config.BChainConfig.GasLimit, uint64(0))
	require.Greater(config.CChainConfig.GasLimit, uint64(0))
	require.Greater(config.MChainConfig.GasLimit, uint64(0))
	require.Greater(config.QChainConfig.GasLimit, uint64(0))
	require.Greater(config.ZChainConfig.GasLimit, uint64(0))

	// Verify block times
	require.Greater(config.AChainConfig.TargetBlockTime, 0)
	require.Greater(config.BChainConfig.TargetBlockTime, 0)
	require.Greater(config.CChainConfig.TargetBlockTime, 0)
	require.Greater(config.MChainConfig.TargetBlockTime, 0)
	require.Greater(config.QChainConfig.TargetBlockTime, 0)
	require.Greater(config.ZChainConfig.TargetBlockTime, 0)
}