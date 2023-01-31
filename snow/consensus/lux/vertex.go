<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
// See the file LICENSE for licensing terms.

package lux

import (
<<<<<<< HEAD
	"github.com/luxdefi/luxd/snow/choices"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
	"github.com/luxdefi/luxd/vms/components/verify"
=======
	"context"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
>>>>>>> 53a8245a8 (Update consensus)
)

// Vertex is a collection of multiple transactions tied to other vertices
type Vertex interface {
	choices.Decidable
<<<<<<< HEAD
	// Vertex verification should be performed before issuance.
	verify.Verifiable
	snowstorm.Whitelister

=======
	snowstorm.Whitelister

	// Vertex verification should be performed before issuance.
	Verify(context.Context) error

>>>>>>> 53a8245a8 (Update consensus)
	// Returns the vertices this vertex depends on
	Parents() ([]Vertex, error)

	// Returns the height of this vertex. A vertex's height is defined by one
	// greater than the maximum height of the parents.
	Height() (uint64, error)

	// Returns a series of state transitions to be performed on acceptance
<<<<<<< HEAD
	Txs() ([]snowstorm.Tx, error)
=======
	Txs(context.Context) ([]snowstorm.Tx, error)
>>>>>>> 53a8245a8 (Update consensus)

	// Returns the binary representation of this vertex
	Bytes() []byte
}
