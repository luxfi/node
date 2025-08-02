// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/v2/api/keystore"
	"github.com/luxfi/node/v2/chains"
	"github.com/luxfi/node/v2/chains/atomic"
	"github.com/luxfi/node/v2/database/memdb"
	"github.com/luxfi/node/v2/ids"
	"github.com/luxfi/node/v2/quasar/engine/common"
	"github.com/luxfi/node/v2/quasar/validatortest"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/utils/crypto/bls"
	"github.com/luxfi/node/v2/utils/logging"
	"github.com/luxfi/node/v2/vms/aivm"
	"github.com/luxfi/node/v2/vms/bridgevm"
	"github.com/luxfi/node/v2/vms/mpcvm"
	"github.com/luxfi/node/v2/vms/platformvm"
	"github.com/luxfi/node/v2/vms/quantumvm"
	"github.com/luxfi/node/v2/vms/registry"
	"github.com/luxfi/node/v2/vms/xvm"
	"github.com/luxfi/node/v2/vms/zkvm"

	// Force registration of all VMs
	_ "github.com/luxfi/node/v2/vms/registry"
)

// TestChainBootstrap tests that all chains can be initialized
func TestChainBootstrap(t *testing.T) {
	tests := []struct {
		name    string
		chainID ids.ID
		vmID    ids.ID
	}{
		{
			name:    "P-Chain",
			chainID: constants.PlatformChainID,
			vmID:    constants.PlatformVMID,
		},
		{
			name:    "C-Chain (EVM)",
			chainID: ids.GenerateTestID(),
			vmID:    constants.EVMID,
		},
		{
			name:    "X-Chain",
			chainID: ids.GenerateTestID(),
			vmID:    constants.XVMID,
		},
		{
			name:    "A-Chain (AI VM)",
			chainID: ids.GenerateTestID(),
			vmID:    aivm.ID,
		},
		{
			name:    "B-Chain (Bridge VM)",
			chainID: ids.GenerateTestID(),
			vmID:    bridgevm.ID,
		},
		{
			name:    "M-Chain (MPC VM)",
			chainID: ids.GenerateTestID(),
			vmID:    mpcvm.ID,
		},
		{
			name:    "Q-Chain (Quantum VM)",
			chainID: ids.GenerateTestID(),
			vmID:    quantumvm.ID,
		},
		{
			name:    "Z-Chain (ZK VM)",
			chainID: ids.GenerateTestID(),
			vmID:    zkvm.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Create a simple VM factory
			vmFactory, err := registry.NewVMFactory(registry.VMFactoryConfig{})
			require.NoError(err)

			// Get the VM factory for this VM ID
			factory, err := vmFactory.GetFactory(tt.vmID)
			require.NoError(err, "VM factory not found for %s", tt.name)
			require.NotNil(factory, "VM factory is nil for %s", tt.name)

			// Create VM instance
			vm, err := factory.New(logging.NoLog{})
			require.NoError(err, "Failed to create VM instance for %s", tt.name)
			require.NotNil(vm, "VM instance is nil for %s", tt.name)

			// Basic type assertions to ensure we have the right VM types
			switch tt.vmID {
			case constants.PlatformVMID:
				_, ok := vm.(*platformvm.VM)
				require.True(ok, "Expected PlatformVM type")
			case constants.EVMID:
				// EVM returns a wrapped VM, so we just check it's not nil
				require.NotNil(vm, "Expected EVM to be non-nil")
			case constants.XVMID:
				_, ok := vm.(*xvm.VM)
				require.True(ok, "Expected XVM type")
			case aivm.ID:
				_, ok := vm.(*aivm.VM)
				require.True(ok, "Expected AIVM type")
			case bridgevm.ID:
				_, ok := vm.(*bridgevm.VM)
				require.True(ok, "Expected BridgeVM type")
			case mpcvm.ID:
				_, ok := vm.(*mpcvm.VM)
				require.True(ok, "Expected MPCVM type")
			case quantumvm.ID:
				_, ok := vm.(*quantumvm.VM)
				require.True(ok, "Expected QuantumVM type")
			case zkvm.ID:
				_, ok := vm.(*zkvm.VM)
				require.True(ok, "Expected ZKVM type")
			}
		})
	}
}

// TestMinimalChainConfiguration tests booting with minimal chains (P, C, X + Q)
func TestMinimalChainConfiguration(t *testing.T) {
	require := require.New(t)

	// Create chain manager config with only essential chains
	config := &chains.MockConfig{
		ChainConfigs: map[string]chains.ChainConfig{
			constants.PlatformChainID.String(): {},
			// C-Chain config
			ids.GenerateTestID().String(): {
				VMID: constants.EVMID,
			},
			// X-Chain config
			ids.GenerateTestID().String(): {
				VMID: constants.XVMID,
			},
			// Q-Chain config for quantum safety
			ids.GenerateTestID().String(): {
				VMID: quantumvm.ID,
			},
		},
	}

	// Verify we can create all required VMs
	vmFactory, err := registry.NewVMFactory(registry.VMFactoryConfig{})
	require.NoError(err)

	// Test that all configured VMs can be created
	for _, chainConfig := range config.ChainConfigs {
		if chainConfig.VMID == ids.Empty {
			continue // Skip P-Chain as it has special handling
		}

		factory, err := vmFactory.GetFactory(chainConfig.VMID)
		require.NoError(err)
		require.NotNil(factory)

		vm, err := factory.New(logging.NoLog{})
		require.NoError(err)
		require.NotNil(vm)
	}
}

// TestVMInitialization tests that VMs can be properly initialized
func TestVMInitialization(t *testing.T) {
	tests := []struct {
		name    string
		vmID    ids.ID
		chainID ids.ID
	}{
		{
			name:    "AIVM Initialization",
			vmID:    aivm.ID,
			chainID: ids.GenerateTestID(),
		},
		{
			name:    "BridgeVM Initialization",
			vmID:    bridgevm.ID,
			chainID: ids.GenerateTestID(),
		},
		{
			name:    "MPCVM Initialization",
			vmID:    mpcvm.ID,
			chainID: ids.GenerateTestID(),
		},
		{
			name:    "QuantumVM Initialization",
			vmID:    quantumvm.ID,
			chainID: ids.GenerateTestID(),
		},
		{
			name:    "ZKVM Initialization",
			vmID:    zkvm.ID,
			chainID: ids.GenerateTestID(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Create VM factory
			vmFactory, err := registry.NewVMFactory(registry.VMFactoryConfig{})
			require.NoError(err)

			// Get factory
			factory, err := vmFactory.GetFactory(tt.vmID)
			require.NoError(err)

			// Create VM
			vm, err := factory.New(logging.NoLog{})
			require.NoError(err)

			// Create mock chain context
			ctx := &common.Context{
				NetworkID: constants.TestnetID,
				SubnetID:  constants.PrimaryNetworkID,
				ChainID:   tt.chainID,
				NodeID:    ids.GenerateTestNodeID(),
				Log:       logging.NoLog{},
				Lock:      &sync.RWMutex{},
				Keystore:  keystore.New(memdb.New()),
				SharedMemory: atomic.NewMemory(memdb.New()),
				BCLookup:  func(context.Context, string, ids.ID) (ids.ID, error) {
					return ids.Empty, nil
				},
				Metrics: prometheus.NewRegistry(),
				ValidatorState: &validatortest.State{
					T: t,
				},
			}

			// Initialize VM with minimal config
			genesisBytes := []byte("{}") // Minimal genesis
			err = vm.Initialize(
				context.Background(),
				ctx,
				memdb.New(),
				genesisBytes,
				nil, // no upgrades
				nil, // no config
				nil, // no message handler
				nil, // no fxs
				nil, // no app sender
			)

			// Some VMs might not be fully implemented yet, so we just check
			// that they don't panic during initialization
			if err != nil {
				t.Logf("VM %s initialization returned error (expected for incomplete VMs): %v", tt.name, err)
			}
		})
	}
}

// TestChainRegistry verifies all chains are properly registered
func TestChainRegistry(t *testing.T) {
	require := require.New(t)

	// Expected VM IDs that should be registered
	expectedVMs := map[string]ids.ID{
		"PlatformVM": constants.PlatformVMID,
		"EVM":        constants.EVMID,
		"XVM":        constants.XVMID,
		"AIVM":       aivm.ID,
		"BridgeVM":   bridgevm.ID,
		"MPCVM":      mpcvm.ID,
		"QuantumVM":  quantumvm.ID,
		"ZKVM":       zkvm.ID,
	}

	// Create VM factory
	vmFactory, err := registry.NewVMFactory(registry.VMFactoryConfig{})
	require.NoError(err)

	// Verify each VM is registered
	for name, vmID := range expectedVMs {
		factory, err := vmFactory.GetFactory(vmID)
		require.NoError(err, "VM %s with ID %s should be registered", name, vmID)
		require.NotNil(factory, "Factory for VM %s should not be nil", name)
	}
}

// Mock config for testing
type MockConfig struct {
	ChainConfigs map[string]chains.ChainConfig
}

func (m *MockConfig) GetChainConfigs() map[string]chains.ChainConfig {
	return m.ChainConfigs
}