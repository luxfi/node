// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/v2/ids"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/utils/logging"
	"github.com/luxfi/node/v2/vms/aivm"
	"github.com/luxfi/node/v2/vms/bridgevm"
	"github.com/luxfi/node/v2/vms/mpcvm"
	"github.com/luxfi/node/v2/vms/quantumvm"
	"github.com/luxfi/node/v2/vms/zkvm"
)

// TestAllVMsRegistered verifies that all 8 chains' VMs are properly registered
func TestAllVMsRegistered(t *testing.T) {
	require := require.New(t)

	vmFactory, err := NewVMFactory(VMFactoryConfig{})
	require.NoError(err)

	tests := []struct {
		name string
		vmID ids.ID
	}{
		// Core chains (P, C, X)
		{
			name: "PlatformVM",
			vmID: constants.PlatformVMID,
		},
		{
			name: "EVM (C-Chain)",
			vmID: constants.EVMID,
		},
		{
			name: "XVM (X-Chain)",
			vmID: constants.XVMID,
		},
		// New chains
		{
			name: "AIVM (A-Chain)",
			vmID: aivm.ID,
		},
		{
			name: "BridgeVM (B-Chain)",
			vmID: bridgevm.ID,
		},
		{
			name: "MPCVM (M-Chain)",
			vmID: mpcvm.ID,
		},
		{
			name: "QuantumVM (Q-Chain)",
			vmID: quantumvm.ID,
		},
		{
			name: "ZKVM (Z-Chain)",
			vmID: zkvm.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory, err := vmFactory.GetFactory(tt.vmID)
			require.NoError(err, "VM %s should be registered", tt.name)
			require.NotNil(factory, "Factory for %s should not be nil", tt.name)

			// Try to create an instance
			vm, err := factory.New(logging.NoLog{})
			require.NoError(err, "Should be able to create %s instance", tt.name)
			require.NotNil(vm, "VM instance for %s should not be nil", tt.name)
		})
	}
}

// TestMinimalBootConfiguration tests that we can boot with just P, C, X, Q chains
func TestMinimalBootConfiguration(t *testing.T) {
	require := require.New(t)

	vmFactory, err := NewVMFactory(VMFactoryConfig{})
	require.NoError(err)

	// Minimal set: P-Chain, C-Chain, X-Chain, and Q-Chain for quantum safety
	minimalVMs := []struct {
		name string
		vmID ids.ID
	}{
		{
			name: "PlatformVM (required)",
			vmID: constants.PlatformVMID,
		},
		{
			name: "EVM/C-Chain (required)",
			vmID: constants.EVMID,
		},
		{
			name: "XVM/X-Chain (required)",
			vmID: constants.XVMID,
		},
		{
			name: "QuantumVM/Q-Chain (quantum safety)",
			vmID: quantumvm.ID,
		},
	}

	for _, vm := range minimalVMs {
		factory, err := vmFactory.GetFactory(vm.vmID)
		require.NoError(err, "Essential VM %s must be available", vm.name)
		require.NotNil(factory, "Factory for essential VM %s must not be nil", vm.name)
	}

	// Verify optional chains are also available but not required for boot
	optionalVMs := []ids.ID{
		aivm.ID,      // A-Chain
		bridgevm.ID,  // B-Chain
		mpcvm.ID,     // M-Chain
		zkvm.ID,      // Z-Chain
	}

	for _, vmID := range optionalVMs {
		factory, err := vmFactory.GetFactory(vmID)
		require.NoError(err, "Optional VM should still be registered")
		require.NotNil(factory, "Optional VM factory should exist")
	}
}

// TestVMIDUniqueness ensures all VM IDs are unique
func TestVMIDUniqueness(t *testing.T) {
	require := require.New(t)

	vmIDs := map[string]ids.ID{
		"PlatformVM": constants.PlatformVMID,
		"EVM":        constants.EVMID,
		"XVM":        constants.XVMID,
		"AIVM":       aivm.ID,
		"BridgeVM":   bridgevm.ID,
		"MPCVM":      mpcvm.ID,
		"QuantumVM":  quantumvm.ID,
		"ZKVM":       zkvm.ID,
	}

	// Check for duplicates
	seen := make(map[ids.ID]string)
	for name, id := range vmIDs {
		if existingName, exists := seen[id]; exists {
			require.Fail("Duplicate VM ID found",
				"VM %s and %s have the same ID: %s", name, existingName, id)
		}
		seen[id] = name
	}

	// Verify we have exactly 8 unique VMs
	require.Len(seen, 8, "Should have exactly 8 unique VM IDs")
}

// TestNewChainVMCreation specifically tests the new A, B, M, Q, Z chains
func TestNewChainVMCreation(t *testing.T) {
	require := require.New(t)

	vmFactory, err := NewVMFactory(VMFactoryConfig{})
	require.NoError(err)

	newChains := []struct {
		name        string
		vmID        ids.ID
		description string
	}{
		{
			name:        "AIVM",
			vmID:        aivm.ID,
			description: "AI/ML operations chain",
		},
		{
			name:        "BridgeVM",
			vmID:        bridgevm.ID,
			description: "Cross-chain bridge operations",
		},
		{
			name:        "MPCVM",
			vmID:        mpcvm.ID,
			description: "Multi-party computation chain",
		},
		{
			name:        "QuantumVM",
			vmID:        quantumvm.ID,
			description: "Quantum-resistant operations",
		},
		{
			name:        "ZKVM",
			vmID:        zkvm.ID,
			description: "Zero-knowledge proof chain",
		},
	}

	for _, chain := range newChains {
		t.Run(chain.name, func(t *testing.T) {
			// Get factory
			factory, err := vmFactory.GetFactory(chain.vmID)
			require.NoError(err, "%s (%s) factory should exist", chain.name, chain.description)
			require.NotNil(factory)

			// Create instance
			vm, err := factory.New(logging.NoLog{})
			require.NoError(err, "Should create %s instance", chain.name)
			require.NotNil(vm)

			// Log success for visibility
			t.Logf("✓ %s (%s) - VM registered and instance created successfully", 
				chain.name, chain.description)
		})
	}
}