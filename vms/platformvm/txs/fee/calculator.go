// Copyright (C) 2024-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"

	"github.com/luxfi/node/vms/platformvm/txs"
)

var ErrUnsupportedTx = errors.New("unsupported transaction type")

// Calculator calculates the minimum required fee, in µLUX, that an unsigned
// transaction must pay for valid inclusion into a block.
type Calculator interface {
	CalculateFee(tx txs.UnsignedTx) (uint64, error)
}
