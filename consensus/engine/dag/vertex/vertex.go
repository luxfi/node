// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow/choices"
)

// Vertex is a basic vertex interface
type Vertex interface {
	choices.Decidable

	// Parents returns the vertices this vertex depends on
	Parents() ([]ids.ID, error)

	// Height returns the height of this vertex
	Height() uint64

	// Epoch returns the epoch this vertex was issued in
	Epoch() uint32

	// Timestamp returns the time this vertex was created
	Timestamp() int64

	// Verify that this vertex is well-formed
	Verify(context.Context) error

	// Bytes returns the binary representation of this vertex
	Bytes() []byte
}

// LinearizableVertex is a vertex that can be linearized
type LinearizableVertex interface {
	Vertex

	// Linearize returns a linear ordering of operations contained in this vertex
	Linearize(context.Context) error
}

// Storage defines the storage interface for vertices
type Storage interface {
	// GetVertex returns the vertex with the given ID
	GetVertex(ctx context.Context, vertexID ids.ID) (Vertex, error)

	// StoreVertex stores a vertex
	StoreVertex(ctx context.Context, vertex Vertex) error

	// RemoveVertex removes a vertex from storage
	RemoveVertex(ctx context.Context, vertexID ids.ID) error

	// VertexIDs returns all vertex IDs in storage
	VertexIDs(ctx context.Context) ([]ids.ID, error)
}

// Manager manages vertex operations
type Manager interface {
	Storage

	// Add a vertex to the manager
	Add(context.Context, Vertex) error

	// Get the vertex with the given ID
	Get(context.Context, ids.ID) (Vertex, error)

	// GetAncestors returns the ancestors of the given vertex
	GetAncestors(context.Context, ids.ID) ([]Vertex, error)

	// Edge returns the edge vertices
	Edge(context.Context) []ids.ID

	// StopVertex stops vertex with given ID
	StopVertex(context.Context, ids.ID) error
}

// LinearizableVM defines a VM that can produce linearizable vertices
type LinearizableVM interface {
	// ParseVertex parses a vertex from bytes
	ParseVertex(context.Context, []byte) (Vertex, error)

	// BuildVertex builds a new vertex
	BuildVertex(context.Context) (Vertex, error)
}

// LinearizableVMWithEngine is a LinearizableVM with engine
type LinearizableVMWithEngine interface {
	LinearizableVM

	// GetEngine returns the consensus engine
	GetEngine() interface{}
}