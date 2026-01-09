// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package graph

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"

	"github.com/luxfi/database"
)

// Precompile address for Graph operations.
// Address: 0x0300000000000000000000000000000000000010
var GraphPrecompileAddress = [20]byte{0x03, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10}

// Function selectors (first 4 bytes of keccak256 of function signature).
var (
	// Mutations
	SelectorCreateNode = [4]byte{0x01, 0x00, 0x00, 0x01} // createNode(bytes32,string,bytes)
	SelectorDeleteNode = [4]byte{0x01, 0x00, 0x00, 0x02} // deleteNode(bytes32)
	SelectorCreateEdge = [4]byte{0x01, 0x00, 0x00, 0x03} // createEdge(bytes32,bytes32,bytes32,string,bytes)
	SelectorDeleteEdge = [4]byte{0x01, 0x00, 0x00, 0x04} // deleteEdge(bytes32)
	SelectorUpdateNode = [4]byte{0x01, 0x00, 0x00, 0x05} // updateNode(bytes32,bytes)
	SelectorUpdateEdge = [4]byte{0x01, 0x00, 0x00, 0x06} // updateEdge(bytes32,bytes)

	// Queries
	SelectorGetNode           = [4]byte{0x02, 0x00, 0x00, 0x01} // getNode(bytes32)
	SelectorGetEdge           = [4]byte{0x02, 0x00, 0x00, 0x02} // getEdge(bytes32)
	SelectorQueryNodesByLabel = [4]byte{0x02, 0x00, 0x00, 0x03} // queryNodesByLabel(string)
	SelectorQueryEdgesByLabel = [4]byte{0x02, 0x00, 0x00, 0x04} // queryEdgesByLabel(string)
	SelectorGetOutgoingEdges  = [4]byte{0x02, 0x00, 0x00, 0x05} // getOutgoingEdges(bytes32)
	SelectorGetIncomingEdges  = [4]byte{0x02, 0x00, 0x00, 0x06} // getIncomingEdges(bytes32)

	// Traversal
	SelectorBFS          = [4]byte{0x03, 0x00, 0x00, 0x01} // bfs(bytes32,uint8,uint32,uint32,string)
	SelectorDFS          = [4]byte{0x03, 0x00, 0x00, 0x02} // dfs(bytes32,uint8,uint32,uint32,string)
	SelectorShortestPath = [4]byte{0x03, 0x00, 0x00, 0x03} // shortestPath(bytes32,bytes32,uint8,uint32,string)
	SelectorHasCycle     = [4]byte{0x03, 0x00, 0x00, 0x04} // hasCycle(bytes32)

	// Pattern Matching
	SelectorSubgraphMatch       = [4]byte{0x04, 0x00, 0x00, 0x01} // subgraphMatch(bytes,uint32)
	SelectorTriangleCount       = [4]byte{0x04, 0x00, 0x00, 0x02} // triangleCount()
	SelectorConnectedComponents = [4]byte{0x04, 0x00, 0x00, 0x03} // connectedComponents()

	// Stats
	SelectorGetNodeCount = [4]byte{0x05, 0x00, 0x00, 0x01} // getNodeCount()
	SelectorGetEdgeCount = [4]byte{0x05, 0x00, 0x00, 0x02} // getEdgeCount()
)

// Gas costs for operations.
const (
	GasCreateNode          = 20000
	GasDeleteNode          = 15000
	GasCreateEdge          = 25000
	GasDeleteEdge          = 15000
	GasUpdateNode          = 10000
	GasUpdateEdge          = 10000
	GasGetNode             = 2000
	GasGetEdge             = 2000
	GasQueryBase           = 5000
	GasQueryPerItem        = 1000
	GasBFSBase             = 10000
	GasBFSPerNode          = 500
	GasDFSBase             = 10000
	GasDFSPerNode          = 500
	GasShortestPath        = 15000
	GasHasCycle            = 20000
	GasSubgraphMatch       = 50000
	GasTriangleCount       = 100000
	GasConnectedComponents = 50000
	GasGetCount            = 1000
)

// GraphContract implements the graph database precompile.
type GraphContract struct {
	storage *GraphStorage
	mu      sync.RWMutex
}

// NewGraphContract creates a new graph contract instance.
func NewGraphContract(db database.Database) *GraphContract {
	return &GraphContract{
		storage: NewGraphStorage(db),
	}
}

// RequiredGas returns the gas required for the operation.
func (c *GraphContract) RequiredGas(input []byte) uint64 {
	if len(input) < 4 {
		return 0
	}

	var selector [4]byte
	copy(selector[:], input[:4])

	switch selector {
	case SelectorCreateNode:
		return GasCreateNode
	case SelectorDeleteNode:
		return GasDeleteNode
	case SelectorCreateEdge:
		return GasCreateEdge
	case SelectorDeleteEdge:
		return GasDeleteEdge
	case SelectorUpdateNode:
		return GasUpdateNode
	case SelectorUpdateEdge:
		return GasUpdateEdge
	case SelectorGetNode:
		return GasGetNode
	case SelectorGetEdge:
		return GasGetEdge
	case SelectorQueryNodesByLabel, SelectorQueryEdgesByLabel:
		return GasQueryBase
	case SelectorGetOutgoingEdges, SelectorGetIncomingEdges:
		return GasQueryBase
	case SelectorBFS:
		return GasBFSBase
	case SelectorDFS:
		return GasDFSBase
	case SelectorShortestPath:
		return GasShortestPath
	case SelectorHasCycle:
		return GasHasCycle
	case SelectorSubgraphMatch:
		return GasSubgraphMatch
	case SelectorTriangleCount:
		return GasTriangleCount
	case SelectorConnectedComponents:
		return GasConnectedComponents
	case SelectorGetNodeCount, SelectorGetEdgeCount:
		return GasGetCount
	default:
		return 0
	}
}

// Run executes the graph precompile operation.
func (c *GraphContract) Run(input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrInvalidInput
	}

	var selector [4]byte
	copy(selector[:], input[:4])
	data := input[4:]

	switch selector {
	// Mutations
	case SelectorCreateNode:
		return c.createNode(data)
	case SelectorDeleteNode:
		return c.deleteNode(data)
	case SelectorCreateEdge:
		return c.createEdge(data)
	case SelectorDeleteEdge:
		return c.deleteEdge(data)
	case SelectorUpdateNode:
		return c.updateNode(data)
	case SelectorUpdateEdge:
		return c.updateEdge(data)

	// Queries
	case SelectorGetNode:
		return c.getNode(data)
	case SelectorGetEdge:
		return c.getEdge(data)
	case SelectorQueryNodesByLabel:
		return c.queryNodesByLabel(data)
	case SelectorQueryEdgesByLabel:
		return c.queryEdgesByLabel(data)
	case SelectorGetOutgoingEdges:
		return c.getOutgoingEdges(data)
	case SelectorGetIncomingEdges:
		return c.getIncomingEdges(data)

	// Traversal
	case SelectorBFS:
		return c.bfs(data)
	case SelectorDFS:
		return c.dfs(data)
	case SelectorShortestPath:
		return c.shortestPath(data)
	case SelectorHasCycle:
		return c.hasCycle(data)

	// Pattern Matching
	case SelectorSubgraphMatch:
		return c.subgraphMatch(data)
	case SelectorTriangleCount:
		return c.triangleCount(data)
	case SelectorConnectedComponents:
		return c.connectedComponents(data)

	// Stats
	case SelectorGetNodeCount:
		return c.getNodeCount(data)
	case SelectorGetEdgeCount:
		return c.getEdgeCount(data)

	default:
		return nil, errors.New("unknown function selector")
	}
}

// Mutation methods

func (c *GraphContract) createNode(data []byte) ([]byte, error) {
	// Parse: bytes32 id, string label, bytes properties
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var id NodeID
	copy(id[:], data[:32])

	// Parse label (length-prefixed string)
	labelLen := int(binary.BigEndian.Uint16(data[32:34]))
	if len(data) < 34+labelLen {
		return nil, ErrInvalidInput
	}
	label := string(data[34 : 34+labelLen])

	// Parse properties (JSON)
	var props map[string][]byte
	if len(data) > 34+labelLen {
		propsData := data[34+labelLen:]
		if len(propsData) > 0 {
			if err := json.Unmarshal(propsData, &props); err != nil {
				props = make(map[string][]byte)
			}
		}
	}

	node := &Node{
		ID:         id,
		Label:      label,
		Properties: props,
	}

	if err := c.storage.CreateNode(node); err != nil {
		return nil, err
	}

	return id[:], nil
}

func (c *GraphContract) deleteNode(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var id NodeID
	copy(id[:], data[:32])

	if err := c.storage.DeleteNode(id); err != nil {
		return nil, err
	}

	return []byte{1}, nil // Success
}

func (c *GraphContract) createEdge(data []byte) ([]byte, error) {
	// Parse: bytes32 id, bytes32 from, bytes32 to, string label, bytes properties
	if len(data) < 96 {
		return nil, ErrInvalidInput
	}

	var id EdgeID
	var from, to NodeID
	copy(id[:], data[:32])
	copy(from[:], data[32:64])
	copy(to[:], data[64:96])

	// Parse label
	labelLen := int(binary.BigEndian.Uint16(data[96:98]))
	if len(data) < 98+labelLen {
		return nil, ErrInvalidInput
	}
	label := string(data[98 : 98+labelLen])

	// Parse properties
	var props map[string][]byte
	if len(data) > 98+labelLen {
		propsData := data[98+labelLen:]
		if len(propsData) > 0 {
			if err := json.Unmarshal(propsData, &props); err != nil {
				props = make(map[string][]byte)
			}
		}
	}

	edge := &Edge{
		ID:         id,
		From:       from,
		To:         to,
		Label:      label,
		Properties: props,
	}

	if err := c.storage.CreateEdge(edge); err != nil {
		return nil, err
	}

	return id[:], nil
}

func (c *GraphContract) deleteEdge(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var id EdgeID
	copy(id[:], data[:32])

	if err := c.storage.DeleteEdge(id); err != nil {
		return nil, err
	}

	return []byte{1}, nil
}

func (c *GraphContract) updateNode(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var id NodeID
	copy(id[:], data[:32])

	node, err := c.storage.GetNode(id)
	if err != nil {
		return nil, err
	}

	// Parse new properties
	if len(data) > 32 {
		propsData := data[32:]
		var props map[string][]byte
		if err := json.Unmarshal(propsData, &props); err != nil {
			return nil, err
		}
		for k, v := range props {
			node.Properties[k] = v
		}
	}

	// Delete and recreate (atomic update)
	if err := c.storage.DeleteNode(id); err != nil {
		return nil, err
	}
	if err := c.storage.CreateNode(node); err != nil {
		return nil, err
	}

	return id[:], nil
}

func (c *GraphContract) updateEdge(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var id EdgeID
	copy(id[:], data[:32])

	edge, err := c.storage.GetEdge(id)
	if err != nil {
		return nil, err
	}

	// Parse new properties
	if len(data) > 32 {
		propsData := data[32:]
		var props map[string][]byte
		if err := json.Unmarshal(propsData, &props); err != nil {
			return nil, err
		}
		for k, v := range props {
			edge.Properties[k] = v
		}
	}

	// Delete and recreate
	if err := c.storage.DeleteEdge(id); err != nil {
		return nil, err
	}
	if err := c.storage.CreateEdge(edge); err != nil {
		return nil, err
	}

	return id[:], nil
}

// Query methods

func (c *GraphContract) getNode(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var id NodeID
	copy(id[:], data[:32])

	node, err := c.storage.GetNode(id)
	if err != nil {
		return nil, err
	}

	return node.Serialize()
}

func (c *GraphContract) getEdge(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var id EdgeID
	copy(id[:], data[:32])

	edge, err := c.storage.GetEdge(id)
	if err != nil {
		return nil, err
	}

	return edge.Serialize()
}

func (c *GraphContract) queryNodesByLabel(data []byte) ([]byte, error) {
	label := string(data)

	nodes, err := c.storage.QueryNodesByLabel(label)
	if err != nil {
		return nil, err
	}

	return json.Marshal(nodes)
}

func (c *GraphContract) queryEdgesByLabel(data []byte) ([]byte, error) {
	label := string(data)

	edges, err := c.storage.QueryEdgesByLabel(label)
	if err != nil {
		return nil, err
	}

	return json.Marshal(edges)
}

func (c *GraphContract) getOutgoingEdges(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var id NodeID
	copy(id[:], data[:32])

	edges, err := c.storage.GetOutgoingEdges(id)
	if err != nil {
		return nil, err
	}

	return json.Marshal(edges)
}

func (c *GraphContract) getIncomingEdges(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var id NodeID
	copy(id[:], data[:32])

	edges, err := c.storage.GetIncomingEdges(id)
	if err != nil {
		return nil, err
	}

	return json.Marshal(edges)
}

// Traversal methods

func (c *GraphContract) bfs(data []byte) ([]byte, error) {
	// Parse: bytes32 startID, uint8 direction, uint32 maxDepth, uint32 maxNodes, string edgeLabel
	if len(data) < 41 {
		return nil, ErrInvalidInput
	}

	var startID NodeID
	copy(startID[:], data[:32])

	direction := TraversalDirection(data[32])
	maxDepth := int(binary.BigEndian.Uint32(data[33:37]))
	maxNodes := int(binary.BigEndian.Uint32(data[37:41]))

	edgeLabel := ""
	if len(data) > 41 {
		edgeLabel = string(data[41:])
	}

	opts := TraversalOptions{
		Direction: direction,
		MaxDepth:  maxDepth,
		MaxNodes:  maxNodes,
		EdgeLabel: edgeLabel,
	}

	results, err := c.storage.BFS(startID, opts)
	if err != nil {
		return nil, err
	}

	return json.Marshal(results)
}

func (c *GraphContract) dfs(data []byte) ([]byte, error) {
	// Parse same as BFS
	if len(data) < 41 {
		return nil, ErrInvalidInput
	}

	var startID NodeID
	copy(startID[:], data[:32])

	direction := TraversalDirection(data[32])
	maxDepth := int(binary.BigEndian.Uint32(data[33:37]))
	maxNodes := int(binary.BigEndian.Uint32(data[37:41]))

	edgeLabel := ""
	if len(data) > 41 {
		edgeLabel = string(data[41:])
	}

	opts := TraversalOptions{
		Direction: direction,
		MaxDepth:  maxDepth,
		MaxNodes:  maxNodes,
		EdgeLabel: edgeLabel,
	}

	results, err := c.storage.DFS(startID, opts)
	if err != nil {
		return nil, err
	}

	return json.Marshal(results)
}

func (c *GraphContract) shortestPath(data []byte) ([]byte, error) {
	// Parse: bytes32 fromID, bytes32 toID, uint8 direction, uint32 maxDepth, string edgeLabel
	if len(data) < 69 {
		return nil, ErrInvalidInput
	}

	var fromID, toID NodeID
	copy(fromID[:], data[:32])
	copy(toID[:], data[32:64])

	direction := TraversalDirection(data[64])
	maxDepth := int(binary.BigEndian.Uint32(data[65:69]))

	edgeLabel := ""
	if len(data) > 69 {
		edgeLabel = string(data[69:])
	}

	opts := TraversalOptions{
		Direction: direction,
		MaxDepth:  maxDepth,
		EdgeLabel: edgeLabel,
	}

	path, err := c.storage.ShortestPath(fromID, toID, opts)
	if err != nil {
		return nil, err
	}

	return json.Marshal(path)
}

func (c *GraphContract) hasCycle(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var startID NodeID
	copy(startID[:], data[:32])

	hasCycle, err := c.storage.HasCycle(startID)
	if err != nil {
		return nil, err
	}

	if hasCycle {
		return []byte{1}, nil
	}
	return []byte{0}, nil
}

// Pattern matching methods

func (c *GraphContract) subgraphMatch(data []byte) ([]byte, error) {
	// Parse: bytes pattern, uint32 maxResults
	if len(data) < 4 {
		return nil, ErrInvalidInput
	}

	maxResults := int(binary.BigEndian.Uint32(data[:4]))
	patternData := data[4:]

	var pattern SubgraphPattern
	if err := json.Unmarshal(patternData, &pattern); err != nil {
		return nil, err
	}

	results, err := c.storage.SubgraphMatch(pattern, maxResults)
	if err != nil {
		return nil, err
	}

	return json.Marshal(results)
}

func (c *GraphContract) triangleCount(_ []byte) ([]byte, error) {
	count, err := c.storage.TriangleCount()
	if err != nil {
		return nil, err
	}

	result := make([]byte, 8)
	binary.BigEndian.PutUint64(result, count)
	return result, nil
}

func (c *GraphContract) connectedComponents(_ []byte) ([]byte, error) {
	count, err := c.storage.ConnectedComponents()
	if err != nil {
		return nil, err
	}

	result := make([]byte, 4)
	binary.BigEndian.PutUint32(result, uint32(count))
	return result, nil
}

// Stats methods

func (c *GraphContract) getNodeCount(_ []byte) ([]byte, error) {
	count, err := c.storage.GetNodeCount()
	if err != nil {
		return nil, err
	}

	result := make([]byte, 8)
	binary.BigEndian.PutUint64(result, count)
	return result, nil
}

func (c *GraphContract) getEdgeCount(_ []byte) ([]byte, error) {
	count, err := c.storage.GetEdgeCount()
	if err != nil {
		return nil, err
	}

	result := make([]byte, 8)
	binary.BigEndian.PutUint64(result, count)
	return result, nil
}
