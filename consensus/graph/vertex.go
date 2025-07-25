// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dag

import (
	"context"

	"github.com/luxfi/node/consensus/choices"
)

// Vertex is a node in the DAG.
type Vertex interface {
	choices.Decidable

	// Parents returns the vertices this vertex depends on
	Parents() ([]Vertex, error)

	// Height returns the distance from this vertex to the genesis vertex
	Height() (uint64, error)

	// Txs returns the transactions this vertex contains
	Txs(context.Context) ([]Tx, error)

	// Bytes returns the byte representation of this vertex
	Bytes() []byte
}
