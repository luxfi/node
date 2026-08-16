// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Allow vm to execute custom logic against the underlying transaction types.
type Visitor interface {
	AddValidatorTx(*AddValidatorTx) error
	AddChainValidatorTx(*AddChainValidatorTx) error
	AddDelegatorTx(*AddDelegatorTx) error
	CreateNetworkTx(*CreateNetworkTx) error
	ConvertNetworkTx(*ConvertNetworkTx) error
	CreateChainTx(*CreateChainTx) error
	ImportTx(*ImportTx) error
	ExportTx(*ExportTx) error
	RewardValidatorTx(*RewardValidatorTx) error

	RemoveChainValidatorTx(*RemoveChainValidatorTx) error
	TransformChainTx(*TransformChainTx) error
	AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error
	AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error

	TransferChainOwnershipTx(*TransferChainOwnershipTx) error
	BaseTx(*BaseTx) error

	RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error
	SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error
	IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error
	DisableL1ValidatorTx(*DisableL1ValidatorTx) error
}
