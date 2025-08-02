// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"github.com/luxfi/node/v2/wallet"
)

// Re-export types and functions from the wallet package for backward compatibility
type UTXOs = wallet.UTXOs
type ChainUTXOs = wallet.ChainUTXOs

var (
	NewUTXOs      = wallet.NewUTXOs
	NewChainUTXOs = wallet.NewChainUTXOs
)
