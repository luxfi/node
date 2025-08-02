// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package graph

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/set"
)

// Graph represents a directed graph
type Graph interface {
	// Add adds a vertex to the graph
	Add(vertex ids.ID)

	// Remove removes a vertex from the graph
	Remove(vertex ids.ID)

	// AddEdge adds an edge from u to v
	AddEdge(u, v ids.ID)

	// RemoveEdge removes the edge from u to v
	RemoveEdge(u, v ids.ID)

	// Neighbors returns the neighbors of a vertex
	Neighbors(vertex ids.ID) set.Set[ids.ID]

	// HasEdge returns true if there is an edge from u to v
	HasEdge(u, v ids.ID) bool

	// Vertices returns all vertices in the graph
	Vertices() set.Set[ids.ID]

	// Len returns the number of vertices
	Len() int
}

// Factory creates new graphs
type Factory interface {
	// New returns a new graph
	New() Graph
}

// Topological provides topological operations on graphs
type Topological interface {
	Graph

	// TopologicalSort returns a topological ordering of the graph
	TopologicalSort() ([]ids.ID, error)

	// HasCycle returns true if the graph has a cycle
	HasCycle() bool

	// StronglyConnectedComponents returns the strongly connected components
	StronglyConnectedComponents() []set.Set[ids.ID]
}

// Tx represents a transaction in the graph
type Tx interface {
	// ID returns the transaction ID
	ID() ids.ID

	// Inputs returns the input IDs
	Inputs() set.Set[ids.ID]

	// Outputs returns the output IDs
	Outputs() set.Set[ids.ID]

	// Bytes returns the binary representation
	Bytes() []byte
}