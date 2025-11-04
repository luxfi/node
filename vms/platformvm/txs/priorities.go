// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

const (
	// First primary network apricot delegators are moved from the pending to
	// the current validator set,
	PrimaryNetworkDelegatorApricotPendingPriority Priority = iota + 1
	// then primary network validators,
	PrimaryNetworkValidatorPendingPriority
	// then primary network banff delegators,
	PrimaryNetworkDelegatorBanffPendingPriority
	// then permissionless net validators,
	NetPermissionlessValidatorPendingPriority
	// then permissionless net delegators.
	NetPermissionlessDelegatorPendingPriority
	// then permissioned net validators,
	NetPermissionedValidatorPendingPriority

	// First permissioned net validators are removed from the current
	// validator set,
	// Invariant: All permissioned stakers must be removed first because they
	//            are removed by the advancement of time. Permissionless stakers
	//            are removed with a RewardValidatorTx after time has advanced.
	NetPermissionedValidatorCurrentPriority
	// then permissionless net delegators,
	NetPermissionlessDelegatorCurrentPriority
	// then permissionless net validators,
	NetPermissionlessValidatorCurrentPriority
	// then primary network delegators,
	PrimaryNetworkDelegatorCurrentPriority
	// then primary network validators.
	PrimaryNetworkValidatorCurrentPriority
)

var PendingToCurrentPriorities = []Priority{
	PrimaryNetworkDelegatorApricotPendingPriority: PrimaryNetworkDelegatorCurrentPriority,
	PrimaryNetworkValidatorPendingPriority:        PrimaryNetworkValidatorCurrentPriority,
	PrimaryNetworkDelegatorBanffPendingPriority:   PrimaryNetworkDelegatorCurrentPriority,
	NetPermissionlessValidatorPendingPriority:  NetPermissionlessValidatorCurrentPriority,
	NetPermissionlessDelegatorPendingPriority:  NetPermissionlessDelegatorCurrentPriority,
	NetPermissionedValidatorPendingPriority:    NetPermissionedValidatorCurrentPriority,
}

type Priority byte

func (p Priority) IsCurrent() bool {
	return p.IsCurrentValidator() || p.IsCurrentDelegator()
}

func (p Priority) IsPending() bool {
	return p.IsPendingValidator() || p.IsPendingDelegator()
}

func (p Priority) IsValidator() bool {
	return p.IsCurrentValidator() || p.IsPendingValidator()
}

func (p Priority) IsPermissionedValidator() bool {
	return p == NetPermissionedValidatorCurrentPriority ||
		p == NetPermissionedValidatorPendingPriority
}

func (p Priority) IsDelegator() bool {
	return p.IsCurrentDelegator() || p.IsPendingDelegator()
}

func (p Priority) IsCurrentValidator() bool {
	return p == PrimaryNetworkValidatorCurrentPriority ||
		p == NetPermissionedValidatorCurrentPriority ||
		p == NetPermissionlessValidatorCurrentPriority
}

func (p Priority) IsCurrentDelegator() bool {
	return p == PrimaryNetworkDelegatorCurrentPriority ||
		p == NetPermissionlessDelegatorCurrentPriority
}

func (p Priority) IsPendingValidator() bool {
	return p == PrimaryNetworkValidatorPendingPriority ||
		p == NetPermissionedValidatorPendingPriority ||
		p == NetPermissionlessValidatorPendingPriority
}

func (p Priority) IsPendingDelegator() bool {
	return p == PrimaryNetworkDelegatorBanffPendingPriority ||
		p == PrimaryNetworkDelegatorApricotPendingPriority ||
		p == NetPermissionlessDelegatorPendingPriority
}
