<<<<<<< HEAD
<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
=======
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
>>>>>>> 34554f662 (Update LICENSE)
>>>>>>> c5eafdb72 (Update LICENSE)
// See the file LICENSE for licensing terms.

package vertex

import (
<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
	"github.com/luxdefi/luxd/snow/engine/common"
=======
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
>>>>>>> 53a8245a8 (Update consensus)
)

// DAGVM defines the minimum functionality that an lux VM must
// implement
type DAGVM interface {
<<<<<<< HEAD
	common.VM
	Getter

	// Return any transactions that have not been sent to consensus yet
	PendingTxs() []snowstorm.Tx

	// Convert a stream of bytes to a transaction or return an error
	ParseTx(tx []byte) (snowstorm.Tx, error)
=======
	block.ChainVM
	Getter

	// Linearize is called after [Initialize] and after the DAG has been
	// finalized. After Linearize is called:
	// - PendingTxs will never be called again
	// - GetTx will never be called again
	// - ParseTx may still be called
	// - All the block based functions of the [block.ChainVM] must work as
	//   expected.
	// Linearize is part of the VM initialization, and will be called at most
	// once per VM instantiation. This means that Linearize should be called
	// every time the chain restarts after the DAG has finalized.
	Linearize(ctx context.Context, stopVertexID ids.ID) error

	// Return any transactions that have not been sent to consensus yet
	PendingTxs(ctx context.Context) []snowstorm.Tx

	// Convert a stream of bytes to a transaction or return an error
	ParseTx(ctx context.Context, txBytes []byte) (snowstorm.Tx, error)
>>>>>>> 53a8245a8 (Update consensus)
}

// Getter defines the functionality for fetching a tx/block by its ID.
type Getter interface {
	// Retrieve a transaction that was submitted previously
<<<<<<< HEAD
	GetTx(ids.ID) (snowstorm.Tx, error)
=======
	GetTx(ctx context.Context, txID ids.ID) (snowstorm.Tx, error)
>>>>>>> 53a8245a8 (Update consensus)
}
