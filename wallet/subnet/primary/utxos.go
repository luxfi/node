// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"github.com/luxfi/node/wallet"
)

// Re-export types and functions from the wallet package for backward compatibility
type UTXOs = wallet.UTXOs
type ChainUTXOs = wallet.ChainUTXOs

var (
	NewUTXOs = wallet.NewUTXOs
	NewChainUTXOs = wallet.NewChainUTXOs
)
