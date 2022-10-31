// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
)

// Storage defines the persistent storage that is required by the consensus
// engine.
type Storage interface {
	// Get a vertex by its hash from storage.
	GetVtx(vtxID ids.ID) (lux.Vertex, error)
	// Edge returns a list of accepted vertex IDs with no accepted children.
	Edge() (vtxIDs []ids.ID)
	// Returns "true" if accepted frontier ("Edge") is stop vertex.
	StopVertexAccepted() (bool, error)
}
