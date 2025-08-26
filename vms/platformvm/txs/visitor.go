// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Allow vm to execute custom logic against the underlying transaction types.
type Visitor interface {
	AddValidatorTx(*AddValidatorTx) error
	AddNetValidatorTx(*AddNetValidatorTx) error
	AddDelegatorTx(*AddDelegatorTx) error
	CreateChainTx(*CreateChainTx) error
	CreateNetTx(*CreateNetTx) error
	ImportTx(*ImportTx) error
	ExportTx(*ExportTx) error
	AdvanceTimeTx(*AdvanceTimeTx) error
	RewardValidatorTx(*RewardValidatorTx) error
	RemoveNetValidatorTx(*RemoveNetValidatorTx) error
	TransformNetTx(*TransformNetTx) error
	AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error
	AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error
	TransferNetOwnershipTx(*TransferNetOwnershipTx) error
	BaseTx(*BaseTx) error
	ConvertNetToL1Tx(*ConvertNetToL1Tx) error
	DisableL1ValidatorTx(*DisableL1ValidatorTx) error
	IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error
	RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error
	SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error
}
