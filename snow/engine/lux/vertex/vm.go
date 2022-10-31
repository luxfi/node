// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
	"github.com/luxdefi/luxd/snow/engine/common"
)

// DAGVM defines the minimum functionality that an lux VM must
// implement
type DAGVM interface {
	common.VM
	Getter

	// Return any transactions that have not been sent to consensus yet
	PendingTxs() []snowstorm.Tx

	// Convert a stream of bytes to a transaction or return an error
	ParseTx(tx []byte) (snowstorm.Tx, error)
}

// Getter defines the functionality for fetching a tx/block by its ID.
type Getter interface {
	// Retrieve a transaction that was submitted previously
	GetTx(ids.ID) (snowstorm.Tx, error)
}
