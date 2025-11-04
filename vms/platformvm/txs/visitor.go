// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Allow vm to execute custom logic against the underlying transaction types.
type Visitor interface {
	// Apricot Transactions:
	AddValidatorTx(*AddValidatorTx) error
	AddNetValidatorTx(*AddNetValidatorTx) error
	AddDelegatorTx(*AddDelegatorTx) error
	CreateChainTx(*CreateChainTx) error
	CreateNetTx(*CreateNetTx) error
	ImportTx(*ImportTx) error
	ExportTx(*ExportTx) error
	AdvanceTimeTx(*AdvanceTimeTx) error
	RewardValidatorTx(*RewardValidatorTx) error

	// Banff Transactions:
	RemoveNetValidatorTx(*RemoveNetValidatorTx) error
	TransformNetTx(*TransformNetTx) error
	AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error
	AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error

	// Durango Transactions:
	TransferNetOwnershipTx(*TransferNetOwnershipTx) error
	BaseTx(*BaseTx) error

	// Etna Transactions:
	ConvertNetToL1Tx(*ConvertNetToL1Tx) error
	RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error
	SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error
	IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error
	DisableL1ValidatorTx(*DisableL1ValidatorTx) error
}
