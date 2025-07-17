// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import "github.com/luxfi/node/upgrade"

// Struct collecting all the foundational parameters of the AVM
type Config struct {
	Upgrades upgrade.Config

	// Fee that is burned by every non-asset creating transaction
	TxFee uint64

	// Fee that must be burned by every asset creating transaction
	CreateAssetTxFee uint64
}
