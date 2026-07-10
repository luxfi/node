// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Allow vm to execute custom logic against the underlying transaction types.
type Visitor interface {
	AddValidatorTx(*AddValidatorTx) error
	AddChainValidatorTx(*AddChainValidatorTx) error
	AddDelegatorTx(*AddDelegatorTx) error
	CreateNetworkTx(*CreateNetworkTx) error
	CreateChainTx(*CreateChainTx) error
	ImportTx(*ImportTx) error
	ExportTx(*ExportTx) error
	AdvanceTimeTx(*AdvanceTimeTx) error
	RewardValidatorTx(*RewardValidatorTx) error

	RemoveChainValidatorTx(*RemoveChainValidatorTx) error
	TransformChainTx(*TransformChainTx) error
	AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error
	AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error

	TransferChainOwnershipTx(*TransferChainOwnershipTx) error
	BaseTx(*BaseTx) error

	ConvertNetworkToL1Tx(*ConvertNetworkToL1Tx) error
	// CreateSovereignL1Tx atomically registers a new sovereign L1
	// (network + genesis validators + chain manifest + manager-contract
	// handoff) in one tx. The single-step alternative to the legacy
	// CreateNetworkTx + AddChainValidatorTx ×N + CreateChainTx ×K +
	// ConvertNetworkToL1Tx flow. After commit, the primary network
	// neither track-chains nor validates the L1's blocks — the L1's
	// own validators run their own consensus from genesis.
	CreateSovereignL1Tx(*CreateSovereignL1Tx) error
	RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error
	SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error
	IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error
	DisableL1ValidatorTx(*DisableL1ValidatorTx) error
}
