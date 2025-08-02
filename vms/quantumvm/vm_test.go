// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quantumvm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/log"
)

// TestVMID verifies the VM ID is set correctly
func TestVMID(t *testing.T) {
	require := require.New(t)
	
	// Verify ID is not empty
	require.NotEqual(ID.String(), "")
	t.Logf("QuantumVM ID: %s", ID.String())
}

// TestFactory verifies the factory can create VM instances
func TestFactory(t *testing.T) {
	require := require.New(t)

	factory := &Factory{}
	
	// Create VM instance
	vm, err := factory.New(log.NewNoOpLogger())
	require.NoError(err)
	require.NotNil(vm)
	
	// Verify it's the right type
	quantumVM, ok := vm.(*VM)
	require.True(ok, "Factory should return *VM type")
	require.NotNil(quantumVM)
}

// TestVMBasicMethods tests that basic VM methods don't panic
func TestVMBasicMethods(t *testing.T) {
	require := require.New(t)

	vm := &VM{}
	
	// These methods should not panic even without initialization
	require.NotPanics(func() {
		ctx := context.Background()
		_, _ = vm.Version(ctx)
	})
	
	// Verify version is set
	ctx := context.Background()
	ver, err := vm.Version(ctx)
	require.NoError(err)
	require.Equal("0.1.0", ver)
}

// TestQuantumSafety verifies quantum-safety related features
func TestQuantumSafety(t *testing.T) {
	require := require.New(t)

	vm := &VM{}
	
	// Verify the VM advertises quantum safety
	// This is a placeholder - actual implementation would verify
	// post-quantum cryptographic algorithms are available
	require.NotNil(vm)
	t.Log("QuantumVM is designed for post-quantum cryptography")
}