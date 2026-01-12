// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package graph

import (
	"encoding/binary"
	"encoding/json"
	"errors"
)

var (
	ErrNodeNotFound   = errors.New("node not found")
	ErrEdgeNotFound   = errors.New("edge not found")
	ErrInvalidInput   = errors.New("invalid input")
	ErrDuplicateNode  = errors.New("duplicate node")
	ErrDuplicateEdge  = errors.New("duplicate edge")
	ErrCyclicGraph    = errors.New("cyclic graph detected where not allowed")
	ErrPatternInvalid = errors.New("invalid pattern")
)

// NodeID is a 32-byte identifier for a node.
type NodeID [32]byte

// EdgeID is a 32-byte identifier for an edge.
type EdgeID [32]byte

// Node represents a vertex in the graph.
type Node struct {
	ID         NodeID            `json:"id"`
	Label      string            `json:"label"`
	Properties map[string][]byte `json:"properties"`
}

// Edge represents a directed edge between two nodes.
type Edge struct {
	ID         EdgeID            `json:"id"`
	From       NodeID            `json:"from"`
	To         NodeID            `json:"to"`
	Label      string            `json:"label"`
	Properties map[string][]byte `json:"properties"`
}

// TraversalResult represents a path found during traversal.
type TraversalResult struct {
	Path  []NodeID `json:"path"`
	Depth int      `json:"depth"`
}

// PatternNode represents a node constraint in a subgraph pattern.
type PatternNode struct {
	Var        string            `json:"var"`        // Variable name for binding
	Label      string            `json:"label"`      // Required label (empty = any)
	Properties map[string][]byte `json:"properties"` // Required properties
}

// PatternEdge represents an edge constraint in a subgraph pattern.
type PatternEdge struct {
	FromVar    string            `json:"from_var"`
	ToVar      string            `json:"to_var"`
	Label      string            `json:"label"`
	Properties map[string][]byte `json:"properties"`
}

// SubgraphPattern represents a pattern for matching subgraphs.
type SubgraphPattern struct {
	Nodes []PatternNode `json:"nodes"`
	Edges []PatternEdge `json:"edges"`
}

// MatchResult represents a single match from subgraph matching.
type MatchResult struct {
	Bindings map[string]NodeID `json:"bindings"` // var name -> matched node
}

// Serialize serializes a Node to bytes.
func (n *Node) Serialize() ([]byte, error) {
	return json.Marshal(n)
}

// DeserializeNode deserializes bytes to a Node.
func DeserializeNode(data []byte) (*Node, error) {
	var n Node
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// Serialize serializes an Edge to bytes.
func (e *Edge) Serialize() ([]byte, error) {
	return json.Marshal(e)
}

// DeserializeEdge deserializes bytes to an Edge.
func DeserializeEdge(data []byte) (*Edge, error) {
	var e Edge
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// ParseNodeID parses a NodeID from bytes.
func ParseNodeID(data []byte) (NodeID, error) {
	if len(data) != 32 {
		return NodeID{}, ErrInvalidInput
	}
	var id NodeID
	copy(id[:], data)
	return id, nil
}

// ParseEdgeID parses an EdgeID from bytes.
func ParseEdgeID(data []byte) (EdgeID, error) {
	if len(data) != 32 {
		return EdgeID{}, ErrInvalidInput
	}
	var id EdgeID
	copy(id[:], data)
	return id, nil
}

// Uint64ToBytes converts uint64 to bytes (big-endian).
func Uint64ToBytes(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

// BytesToUint64 converts bytes to uint64 (big-endian).
func BytesToUint64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}
