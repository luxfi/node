// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

// Struct collecting all the foundational parameters of the XVM
type Config struct {
	// Fee that is burned by every non-asset creating transaction
	TxFee uint64 `json:"txFee"`

	// Fee that must be burned by every asset creating transaction
	CreateAssetTxFee uint64 `json:"createAssetTxFee"`

	// IndexTransactions enables transaction indexing by address
	IndexTransactions bool `json:"indexTransactions"`
}
