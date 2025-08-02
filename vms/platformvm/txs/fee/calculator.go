// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"

	"github.com/luxfi/node/vms/platformvm/txs"
)

var ErrUnsupportedTx = errors.New("unsupported transaction type")

// Calculator calculates the minimum required fee, in nLUX, that an unsigned
// transaction must pay for valid inclusion into a block.
type Calculator interface {
	CalculateFee(tx txs.UnsignedTx) (uint64, error)
}
