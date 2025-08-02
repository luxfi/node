// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/choices"
)

var (
	ErrVertexNotFound = errors.New("vertex not found")
	ErrInvalidStatus  = errors.New("invalid vertex status")
)

// State manages vertex states for DAG consensus
type State interface {
	// GetVertex returns a vertex by its ID
	GetVertex(vertexID ids.ID) (Vertex, error)

	// AddVertex adds a vertex to the state
	AddVertex(vertex Vertex) error

	// VertexStatus returns the status of a vertex
	VertexStatus(vertexID ids.ID) choices.Status

	// SetVertexStatus sets the status of a vertex
	SetVertexStatus(vertexID ids.ID, status choices.Status) error

	// Edge returns the directed edge from vertex u to vertex v
	Edge(u, v ids.ID) bool

	// Conflicts returns vertices that conflict with the given vertex
	Conflicts(vertexID ids.ID) ([]ids.ID, error)
}

// Vertex represents a vertex in the DAG
type Vertex interface {
	ID() ids.ID
	Parents() []ids.ID
	Height() uint64
	Timestamp() int64
	Bytes() []byte
	Verify() error
}