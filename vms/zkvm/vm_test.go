// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zkvm

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
	t.Logf("ZKVM ID: %s", ID.String())
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
	zkVM, ok := vm.(*VM)
	require.True(ok, "Factory should return *VM type")
	require.NotNil(zkVM)
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
	require.Equal("v0.0.1", ver)
}