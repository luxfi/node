// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// (c) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

// TestSetPreferenceContextCancellation tests that SetPreference properly handles context cancellation
func TestSetPreferenceContextCancellation(t *testing.T) {
	require := require.New(t)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Create a VM (minimal setup for testing)
	vm := &VM{}

	// SetPreference should return context.Canceled error
	testID := ids.GenerateTestID()
	err := vm.SetPreference(ctx, testID)
	require.ErrorIs(err, context.Canceled, "SetPreference should fail with context.Canceled")
}

// TestSetPreferenceContextTimeout tests that SetPreference respects context timeout
func TestSetPreferenceContextTimeout(t *testing.T) {
	require := require.New(t)

	// Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for timeout to trigger
	time.Sleep(10 * time.Millisecond)

	// Create a VM
	vm := &VM{}

	// SetPreference should return context.DeadlineExceeded error
	testID := ids.GenerateTestID()
	err := vm.SetPreference(ctx, testID)
	require.ErrorIs(err, context.DeadlineExceeded, "SetPreference should fail with context.DeadlineExceeded")
}

// TestSetPreferenceValidContext tests that SetPreference succeeds with valid context
func TestSetPreferenceValidContext(t *testing.T) {
	require := require.New(t)

	// Create a valid context
	ctx := context.Background()

	// Create a VM
	vm := &VM{}

	// SetPreference should succeed
	testID := ids.GenerateTestID()
	err := vm.SetPreference(ctx, testID)
	require.NoError(err, "SetPreference should succeed with valid context")
}
