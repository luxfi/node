// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/luxfi/node/vms/platformvm/txs"
)

// Calculator defines the interface for fee calculation
type Calculator interface {
	CalculateFee(tx txs.UnsignedTx) (uint64, error)
}