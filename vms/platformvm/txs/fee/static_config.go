// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

type StaticConfig struct {
	// Fee that is burned by every non-state creating transaction
	TxFee uint64 `json:"txFee"`

	// Fee that must be burned by every state creating transaction before AP3
	CreateAssetTxFee uint64 `json:"createAssetTxFee"`

	// Fee that must be burned by every net creating transaction after AP3
	CreateNetTxFee uint64 `json:"createSubnetTxFee"`

	// Fee that must be burned by every transform net transaction
	TransformNetTxFee uint64 `json:"transformSubnetTxFee"`

	// Fee that must be burned by every blockchain creating transaction after AP3
	CreateBlockchainTxFee uint64 `json:"createBlockchainTxFee"`

	// Transaction fee for adding a primary network validator
	AddPrimaryNetworkValidatorFee uint64 `json:"addPrimaryNetworkValidatorFee"`

	// Transaction fee for adding a primary network delegator
	AddPrimaryNetworkDelegatorFee uint64 `json:"addPrimaryNetworkDelegatorFee"`

	// Transaction fee for adding a net validator
	AddNetValidatorFee uint64 `json:"addNetValidatorFee"`

	// Transaction fee for adding a net delegator
	AddNetDelegatorFee uint64 `json:"addSubnetDelegatorFee"`
}
