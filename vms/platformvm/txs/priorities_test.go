// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPriorityIsCurrent(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{
			priority: PrimaryNetworkDelegatorLegacyPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorPermissionlessPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessDelegatorCurrentPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkDelegatorCurrentPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkValidatorCurrentPriority,
			expected: true,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.priority), func(t *testing.T) {
			require.Equal(t, test.expected, test.priority.IsCurrent())
		})
	}
}

func TestPriorityIsPending(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{
			priority: PrimaryNetworkDelegatorLegacyPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkDelegatorPermissionlessPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessValidatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessDelegatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionedValidatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorCurrentPriority,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.priority), func(t *testing.T) {
			require.Equal(t, test.expected, test.priority.IsPending())
		})
	}
}

func TestPriorityIsValidator(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{
			priority: PrimaryNetworkDelegatorLegacyPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkDelegatorPermissionlessPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionedValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorCurrentPriority,
			expected: true,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.priority), func(t *testing.T) {
			require.Equal(t, test.expected, test.priority.IsValidator())
		})
	}
}

func TestPriorityIsPermissionedValidator(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{
			priority: PrimaryNetworkDelegatorLegacyPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorPermissionlessPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionedValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorCurrentPriority,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.priority), func(t *testing.T) {
			require.Equal(t, test.expected, test.priority.IsPermissionedValidator())
		})
	}
}

func TestPriorityIsDelegator(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{
			priority: PrimaryNetworkDelegatorLegacyPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorPermissionlessPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorCurrentPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorCurrentPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkValidatorCurrentPriority,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.priority), func(t *testing.T) {
			require.Equal(t, test.expected, test.priority.IsDelegator())
		})
	}
}

func TestPriorityIsCurrentValidator(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{
			priority: PrimaryNetworkDelegatorLegacyPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorPermissionlessPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorCurrentPriority,
			expected: true,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.priority), func(t *testing.T) {
			require.Equal(t, test.expected, test.priority.IsCurrentValidator())
		})
	}
}

func TestPriorityIsCurrentDelegator(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{
			priority: PrimaryNetworkDelegatorLegacyPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorPermissionlessPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorCurrentPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorCurrentPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkValidatorCurrentPriority,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.priority), func(t *testing.T) {
			require.Equal(t, test.expected, test.priority.IsCurrentDelegator())
		})
	}
}

func TestPriorityIsPendingValidator(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{
			priority: PrimaryNetworkDelegatorLegacyPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkDelegatorPermissionlessPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorCurrentPriority,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.priority), func(t *testing.T) {
			require.Equal(t, test.expected, test.priority.IsPendingValidator())
		})
	}
}

func TestPriorityIsPendingDelegator(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{
			priority: PrimaryNetworkDelegatorLegacyPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorPermissionlessPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorPendingPriority,
			expected: true,
		},
		{
			priority: ChainPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: ChainPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: ChainPermissionlessValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorCurrentPriority,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d", test.priority), func(t *testing.T) {
			require.Equal(t, test.expected, test.priority.IsPendingDelegator())
		})
	}
}
