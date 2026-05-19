// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Allow vm to execute custom logic against the underlying transaction types.
type Visitor interface {
	// Apricot Transactions:
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
	RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error
	SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error
	IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error
	DisableL1ValidatorTx(*DisableL1ValidatorTx) error

	SlashValidatorTx(*SlashValidatorTx) error

	// P-only primary network — historically X-Chain
	// (kept on PlatformVM, opt-in for X-Chain when present):
	CreateAssetTx(*CreateAssetTx) error
	OperationTx(*OperationTx) error
}
