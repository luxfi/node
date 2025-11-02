// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
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
			priority: PrimaryNetworkDelegatorApricotPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorBanffPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessDelegatorCurrentPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessValidatorCurrentPriority,
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
			priority: PrimaryNetworkDelegatorApricotPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkDelegatorBanffPendingPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessValidatorPendingPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessDelegatorPendingPriority,
			expected: true,
		},
		{
			priority: SubnetPermissionedValidatorPendingPriority,
			expected: true,
		},
		{
			priority: SubnetPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorCurrentPriority,
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
			priority: PrimaryNetworkDelegatorApricotPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkDelegatorBanffPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorPendingPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorPendingPriority,
			expected: true,
		},
		{
			priority: SubnetPermissionedValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorCurrentPriority,
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
			priority: PrimaryNetworkDelegatorApricotPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorBanffPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorPendingPriority,
			expected: true,
		},
		{
			priority: SubnetPermissionedValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorCurrentPriority,
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
			priority: PrimaryNetworkDelegatorApricotPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorBanffPendingPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorPendingPriority,
			expected: true,
		},
		{
			priority: SubnetPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorCurrentPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessValidatorCurrentPriority,
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
			priority: PrimaryNetworkDelegatorApricotPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorBanffPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorCurrentPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorCurrentPriority,
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
			priority: PrimaryNetworkDelegatorApricotPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorBanffPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorCurrentPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessValidatorCurrentPriority,
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
			priority: PrimaryNetworkDelegatorApricotPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkDelegatorBanffPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorPendingPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessDelegatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorPendingPriority,
			expected: true,
		},
		{
			priority: SubnetPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorCurrentPriority,
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
			priority: PrimaryNetworkDelegatorApricotPendingPriority,
			expected: true,
		},
		{
			priority: PrimaryNetworkValidatorPendingPriority,
			expected: false,
		},
		{
			priority: PrimaryNetworkDelegatorBanffPendingPriority,
			expected: true,
		},
		{
			priority: NetPermissionlessValidatorPendingPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorPendingPriority,
			expected: true,
		},
		{
			priority: SubnetPermissionedValidatorPendingPriority,
			expected: false,
		},
		{
			priority: SubnetPermissionedValidatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessDelegatorCurrentPriority,
			expected: false,
		},
		{
			priority: NetPermissionlessValidatorCurrentPriority,
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
