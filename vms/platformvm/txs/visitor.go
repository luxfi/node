// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Allow vm to execute custom logic against the underlying transaction types.
type Visitor interface {
	AddValidatorTx(*AddValidatorTx) error
	AddSubnetValidatorTx(*AddSubnetValidatorTx) error
	AddDelegatorTx(*AddDelegatorTx) error
	CreateChainTx(*CreateChainTx) error
	CreateSubnetTx(*CreateSubnetTx) error
	ImportTx(*ImportTx) error
	ExportTx(*ExportTx) error
	AdvanceTimeTx(*AdvanceTimeTx) error
	RewardValidatorTx(*RewardValidatorTx) error
	RemoveSubnetValidatorTx(*RemoveSubnetValidatorTx) error
	TransformSubnetTx(*TransformSubnetTx) error
	AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error
	AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error
	TransferSubnetOwnershipTx(*TransferSubnetOwnershipTx) error
	BaseTx(*BaseTx) error
	DisableL1ValidatorTx(*DisableL1ValidatorTx) error
	IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error
	RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error
	SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error
}
