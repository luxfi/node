// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

// TestAllVMIDsDefined verifies all 8 VM IDs are defined
func TestAllVMIDsDefined(t *testing.T) {
	require := require.New(t)

	// Test all VM IDs are non-empty
	require.NotEqual(ids.ID{}, PlatformVMID, "PlatformVMID should not be empty")
	require.NotEqual(ids.ID{}, QuantumVMID, "QuantumVMID should not be empty")
	require.NotEqual(ids.ID{}, XVMID, "XVMID should not be empty")
	require.NotEqual(ids.ID{}, EVMID, "EVMID should not be empty")
	require.NotEqual(ids.ID{}, AIVMID, "AIVMID should not be empty")
	require.NotEqual(ids.ID{}, BridgeVMID, "BridgeVMID should not be empty")
	require.NotEqual(ids.ID{}, ZKVMID, "ZKVMID should not be empty")
	require.NotEqual(ids.ID{}, MPCVMID, "MPCVMID should not be empty")

	// Log all VM IDs
	t.Logf("Platform VM ID: %s", PlatformVMID)
	t.Logf("Quantum VM ID: %s", QuantumVMID)
	t.Logf("X VM ID: %s", XVMID)
	t.Logf("EVM ID: %s", EVMID)
	t.Logf("AI VM ID: %s", AIVMID)
	t.Logf("Bridge VM ID: %s", BridgeVMID)
	t.Logf("ZK VM ID: %s", ZKVMID)
	t.Logf("MPC VM ID: %s", MPCVMID)
}

// TestVMNamesMapping verifies VM name mapping works correctly
func TestVMNamesMapping(t *testing.T) {
	require := require.New(t)

	tests := []struct {
		id   ids.ID
		want string
	}{
		{PlatformVMID, PlatformVMName},
		{QuantumVMID, QuantumVMName},
		{XVMID, XVMName},
		{EVMID, EVMName},
		{AIVMID, AIVMName},
		{BridgeVMID, BridgeVMName},
		{ZKVMID, ZKVMName},
		{MPCVMID, MPCVMName},
	}

	for _, tt := range tests {
		name := VMName(tt.id)
		require.Equal(tt.want, name, "VM name should match for %s", tt.want)
		t.Logf("%s -> %s", tt.id, name)
	}
}

// TestMinimalBootVMIDs verifies minimal boot VM IDs (P, C, X, Q)
func TestMinimalBootVMIDs(t *testing.T) {
	require := require.New(t)

	minimalVMs := []struct {
		name string
		id   ids.ID
	}{
		{"P-Chain (Platform)", PlatformVMID},
		{"C-Chain (EVM)", EVMID},
		{"X-Chain (Exchange)", XVMID},
		{"Q-Chain (Quantum)", QuantumVMID},
	}

	for _, vm := range minimalVMs {
		require.NotEqual(ids.ID{}, vm.id, "%s should have a valid ID for minimal boot", vm.name)
		t.Logf("Minimal boot VM %s: %s", vm.name, vm.id)
	}
}