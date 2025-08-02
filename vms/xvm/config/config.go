// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import "github.com/luxfi/node/v2/upgrade"

// Struct collecting all the foundational parameters of the XVM
type Config struct {
	Upgrades upgrade.Config

	// Fee that is burned by every non-asset creating transaction
	TxFee uint64

	// Fee that must be burned by every asset creating transaction
	CreateAssetTxFee uint64
}
