<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
// See the file LICENSE for licensing terms.

package vertex

import (
<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/storage.go
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
=======
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/storage.go
>>>>>>> 53a8245a8 (Update consensus)
)

// Storage defines the persistent storage that is required by the consensus
// engine.
type Storage interface {
	// Get a vertex by its hash from storage.
<<<<<<< HEAD
	GetVtx(vtxID ids.ID) (lux.Vertex, error)
	// Edge returns a list of accepted vertex IDs with no accepted children.
	Edge() (vtxIDs []ids.ID)
	// Returns "true" if accepted frontier ("Edge") is stop vertex.
	StopVertexAccepted() (bool, error)
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/storage.go
	GetVtx(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error)
=======
	GetVtx(vtxID ids.ID) (lux.Vertex, error)
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/storage.go
	// Edge returns a list of accepted vertex IDs with no accepted children.
	Edge(ctx context.Context) (vtxIDs []ids.ID)
	// Returns "true" if accepted frontier ("Edge") is stop vertex.
	StopVertexAccepted(ctx context.Context) (bool, error)
>>>>>>> 53a8245a8 (Update consensus)
}
