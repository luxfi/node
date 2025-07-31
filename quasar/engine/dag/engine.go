// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dag

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
	"github.com/luxfi/node/quasar/engine/core"
)

// Engine is a DAG consensus engine.
type Engine interface {
	core.Engine

	// Initialize this engine.
	Initialize(ctx context.Context, params Parameters) error

	// GetVertex retrieves a vertex by its ID.
	GetVertex(vtxID ids.ID) (Vertex, error)

	// GetVtx retrieves a vertex by its ID.
	GetVtx(vtxID ids.ID) (Vertex, error)

	// Issued returns true if the vertex has been issued.
	Issued(vtx Vertex) bool

	// StopVertexAccepted returns true if all new vertices should be rejected.
	StopVertexAccepted() bool
}

// Parameters defines the parameters for the DAG consensus engine.
type Parameters struct {
	// Parents is the number of parents a vertex should have.
	Parents int

	// BatchSize is the number of vertices to batch together.
	BatchSize int

	// The consensus parameters.
	ConsensusParams interface{}
}

// Vertex is a vertex in the DAG.
type Vertex interface {
	choices.Decidable

	// Vertex returns the unique ID of this vertex.
	Vertex() ids.ID

	// Parents returns the parents of this vertex.
	Parents() []ids.ID

	// Height returns the height of this vertex.
	Height() uint64

	// Epoch returns the epoch of this vertex.
	Epoch() uint32

	// Verify that this vertex is valid.
	Verify(context.Context) error

	// Bytes returns the byte representation of this vertex.
	Bytes() []byte
}

// Storage defines the storage interface for the DAG engine.
type Storage interface {
	// Get a vertex by its ID.
	GetVertex(vtxID ids.ID) (Vertex, error)

	// Put a vertex into storage.
	PutVertex(vtx Vertex) error

	// HasVertex returns true if the vertex exists in storage.
	HasVertex(vtxID ids.ID) bool

	// Edge returns the edge from source to destination.
	Edge(src, dst ids.ID) bool

	// SetEdge sets an edge from source to destination.
	SetEdge(src, dst ids.ID) error
}

// Manager manages vertices in the DAG.
type Manager interface {
	Storage

	// Add a vertex to the DAG.
	Add(vtx Vertex) error

	// Remove a vertex from the DAG.
	Remove(vtxID ids.ID) error

	// GetAncestors returns the ancestors of a vertex.
	GetAncestors(vtxID ids.ID) ([]ids.ID, error)

	// GetDescendants returns the descendants of a vertex.
	GetDescendants(vtxID ids.ID) ([]ids.ID, error)
}