// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpcvm_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/log"
	"github.com/luxfi/node/v2/version"
	"github.com/luxfi/node/v2/vms/mpcvm"
	pluginmpcvm "github.com/luxfi/node/v2/vms/mpcvm/plugin/mpcvm"
)

// TestVMID verifies the VM ID is set correctly
func TestVMID(t *testing.T) {
	require := require.New(t)
	
	// Verify ID is not empty
	require.NotEqual(mpcvm.ID.String(), "")
	t.Logf("MPCVM ID: %s", mpcvm.ID.String())
}

// TestFactory verifies the factory can create VM instances
func TestFactory(t *testing.T) {
	require := require.New(t)

	factory := &pluginmpcvm.Factory{}
	
	// Create VM instance
	vm, err := factory.New(log.NewNoOpLogger())
	require.NoError(err)
	require.NotNil(vm)
	
	// Verify it's the right type
	mpcVM, ok := vm.(*pluginmpcvm.VM)
	require.True(ok, "Factory should return *VM type")
	require.NotNil(mpcVM)
}

// TestVMBasicMethods tests that basic VM methods don't panic
func TestVMBasicMethods(t *testing.T) {
	require := require.New(t)

	vm := &pluginmpcvm.VM{}
	
	// These methods should not panic even without initialization
	require.NotPanics(func() {
		ctx := context.Background()
		_, _ = vm.Version(ctx)
	})
	
	// Verify version is set
	ctx := context.Background()
	ver, err := vm.Version(ctx)
	require.NoError(err)
	require.Equal(version.Current.String(), ver)
}