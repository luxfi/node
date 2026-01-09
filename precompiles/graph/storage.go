// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package graph

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"sync"

	"github.com/luxfi/database"
)

// Storage prefixes for database keys.
var (
	prefixNode      = []byte("graph:node:")
	prefixEdge      = []byte("graph:edge:")
	prefixNodeLabel = []byte("graph:nodeLabel:") // label -> []NodeID
	prefixEdgeLabel = []byte("graph:edgeLabel:") // label -> []EdgeID
	prefixOutgoing  = []byte("graph:out:")       // nodeID -> []EdgeID
	prefixIncoming  = []byte("graph:in:")        // nodeID -> []EdgeID
	prefixNodeCount = []byte("graph:meta:nodeCount")
	prefixEdgeCount = []byte("graph:meta:edgeCount")
)

// GraphStorage provides persistent storage for graph data.
type GraphStorage struct {
	db database.Database
	mu sync.RWMutex
}

// NewGraphStorage creates a new graph storage instance.
func NewGraphStorage(db database.Database) *GraphStorage {
	return &GraphStorage{db: db}
}

// CreateNode stores a new node in the graph.
func (s *GraphStorage) CreateNode(node *Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.nodeKey(node.ID)

	// Check for duplicate
	if exists, _ := s.db.Has(key); exists {
		return ErrDuplicateNode
	}

	data, err := node.Serialize()
	if err != nil {
		return err
	}

	batch := s.db.NewBatch()

	// Store the node
	if err := batch.Put(key, data); err != nil {
		return err
	}

	// Update label index
	if node.Label != "" {
		if err := s.addToLabelIndex(batch, prefixNodeLabel, node.Label, node.ID[:]); err != nil {
			return err
		}
	}

	// Increment node count
	if err := s.incrementCount(batch, prefixNodeCount); err != nil {
		return err
	}

	return batch.Write()
}

// GetNode retrieves a node by ID.
func (s *GraphStorage) GetNode(id NodeID) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.nodeKey(id)
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return nil, ErrNodeNotFound
		}
		return nil, err
	}

	return DeserializeNode(data)
}

// DeleteNode removes a node and all its edges.
func (s *GraphStorage) DeleteNode(id NodeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.nodeKey(id)
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return ErrNodeNotFound
		}
		return err
	}

	node, err := DeserializeNode(data)
	if err != nil {
		return err
	}

	batch := s.db.NewBatch()

	// Delete all outgoing edges
	outEdges, _ := s.getEdgeList(prefixOutgoing, id)
	for _, edgeID := range outEdges {
		if err := s.deleteEdgeInternal(batch, edgeID); err != nil {
			return err
		}
	}

	// Delete all incoming edges
	inEdges, _ := s.getEdgeList(prefixIncoming, id)
	for _, edgeID := range inEdges {
		if err := s.deleteEdgeInternal(batch, edgeID); err != nil {
			return err
		}
	}

	// Delete the node
	if err := batch.Delete(key); err != nil {
		return err
	}

	// Update label index
	if node.Label != "" {
		if err := s.removeFromLabelIndex(batch, prefixNodeLabel, node.Label, id[:]); err != nil {
			return err
		}
	}

	// Decrement node count
	if err := s.decrementCount(batch, prefixNodeCount); err != nil {
		return err
	}

	return batch.Write()
}

// CreateEdge stores a new edge in the graph.
func (s *GraphStorage) CreateEdge(edge *Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify both nodes exist
	if _, err := s.getNodeUnlocked(edge.From); err != nil {
		return err
	}
	if _, err := s.getNodeUnlocked(edge.To); err != nil {
		return err
	}

	key := s.edgeKey(edge.ID)

	// Check for duplicate
	if exists, _ := s.db.Has(key); exists {
		return ErrDuplicateEdge
	}

	data, err := edge.Serialize()
	if err != nil {
		return err
	}

	batch := s.db.NewBatch()

	// Store the edge
	if err := batch.Put(key, data); err != nil {
		return err
	}

	// Update outgoing edges index
	if err := s.addToEdgeList(batch, prefixOutgoing, edge.From, edge.ID); err != nil {
		return err
	}

	// Update incoming edges index
	if err := s.addToEdgeList(batch, prefixIncoming, edge.To, edge.ID); err != nil {
		return err
	}

	// Update label index
	if edge.Label != "" {
		if err := s.addToLabelIndex(batch, prefixEdgeLabel, edge.Label, edge.ID[:]); err != nil {
			return err
		}
	}

	// Increment edge count
	if err := s.incrementCount(batch, prefixEdgeCount); err != nil {
		return err
	}

	return batch.Write()
}

// GetEdge retrieves an edge by ID.
func (s *GraphStorage) GetEdge(id EdgeID) (*Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.edgeKey(id)
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return nil, ErrEdgeNotFound
		}
		return nil, err
	}

	return DeserializeEdge(data)
}

// DeleteEdge removes an edge.
func (s *GraphStorage) DeleteEdge(id EdgeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	batch := s.db.NewBatch()
	if err := s.deleteEdgeInternal(batch, id); err != nil {
		return err
	}
	return batch.Write()
}

// GetOutgoingEdges returns all edges starting from a node.
func (s *GraphStorage) GetOutgoingEdges(nodeID NodeID) ([]*Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	edgeIDs, err := s.getEdgeList(prefixOutgoing, nodeID)
	if err != nil {
		return nil, err
	}

	edges := make([]*Edge, 0, len(edgeIDs))
	for _, edgeID := range edgeIDs {
		edge, err := s.getEdgeUnlocked(edgeID)
		if err == nil {
			edges = append(edges, edge)
		}
	}
	return edges, nil
}

// GetIncomingEdges returns all edges ending at a node.
func (s *GraphStorage) GetIncomingEdges(nodeID NodeID) ([]*Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	edgeIDs, err := s.getEdgeList(prefixIncoming, nodeID)
	if err != nil {
		return nil, err
	}

	edges := make([]*Edge, 0, len(edgeIDs))
	for _, edgeID := range edgeIDs {
		edge, err := s.getEdgeUnlocked(edgeID)
		if err == nil {
			edges = append(edges, edge)
		}
	}
	return edges, nil
}

// QueryNodesByLabel returns all nodes with the given label.
func (s *GraphStorage) QueryNodesByLabel(label string) ([]*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := append(prefixNodeLabel, []byte(label)...)
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return []*Node{}, nil
		}
		return nil, err
	}

	var nodeIDs [][]byte
	if err := json.Unmarshal(data, &nodeIDs); err != nil {
		return nil, err
	}

	nodes := make([]*Node, 0, len(nodeIDs))
	for _, idBytes := range nodeIDs {
		var id NodeID
		copy(id[:], idBytes)
		node, err := s.getNodeUnlocked(id)
		if err == nil {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

// QueryEdgesByLabel returns all edges with the given label.
func (s *GraphStorage) QueryEdgesByLabel(label string) ([]*Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := append(prefixEdgeLabel, []byte(label)...)
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return []*Edge{}, nil
		}
		return nil, err
	}

	var edgeIDs [][]byte
	if err := json.Unmarshal(data, &edgeIDs); err != nil {
		return nil, err
	}

	edges := make([]*Edge, 0, len(edgeIDs))
	for _, idBytes := range edgeIDs {
		var id EdgeID
		copy(id[:], idBytes)
		edge, err := s.getEdgeUnlocked(id)
		if err == nil {
			edges = append(edges, edge)
		}
	}
	return edges, nil
}

// GetNodeCount returns the total number of nodes.
func (s *GraphStorage) GetNodeCount() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getCount(prefixNodeCount)
}

// GetEdgeCount returns the total number of edges.
func (s *GraphStorage) GetEdgeCount() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getCount(prefixEdgeCount)
}

// GenerateNodeID creates a deterministic NodeID from input data.
func GenerateNodeID(data []byte) NodeID {
	hash := sha256.Sum256(data)
	return NodeID(hash)
}

// GenerateEdgeID creates a deterministic EdgeID from input data.
func GenerateEdgeID(from, to NodeID, label string) EdgeID {
	h := sha256.New()
	h.Write(from[:])
	h.Write(to[:])
	h.Write([]byte(label))
	var id EdgeID
	copy(id[:], h.Sum(nil))
	return id
}

// Internal helper methods

func (s *GraphStorage) nodeKey(id NodeID) []byte {
	return append(prefixNode, id[:]...)
}

func (s *GraphStorage) edgeKey(id EdgeID) []byte {
	return append(prefixEdge, id[:]...)
}

func (s *GraphStorage) getNodeUnlocked(id NodeID) (*Node, error) {
	key := s.nodeKey(id)
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return nil, ErrNodeNotFound
		}
		return nil, err
	}
	return DeserializeNode(data)
}

func (s *GraphStorage) getEdgeUnlocked(id EdgeID) (*Edge, error) {
	key := s.edgeKey(id)
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return nil, ErrEdgeNotFound
		}
		return nil, err
	}
	return DeserializeEdge(data)
}

func (s *GraphStorage) deleteEdgeInternal(batch database.Batch, id EdgeID) error {
	key := s.edgeKey(id)
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return ErrEdgeNotFound
		}
		return err
	}

	edge, err := DeserializeEdge(data)
	if err != nil {
		return err
	}

	// Delete the edge
	if err := batch.Delete(key); err != nil {
		return err
	}

	// Update outgoing edges index
	if err := s.removeFromEdgeList(batch, prefixOutgoing, edge.From, id); err != nil {
		return err
	}

	// Update incoming edges index
	if err := s.removeFromEdgeList(batch, prefixIncoming, edge.To, id); err != nil {
		return err
	}

	// Update label index
	if edge.Label != "" {
		if err := s.removeFromLabelIndex(batch, prefixEdgeLabel, edge.Label, id[:]); err != nil {
			return err
		}
	}

	// Decrement edge count
	return s.decrementCount(batch, prefixEdgeCount)
}

func (s *GraphStorage) getEdgeList(prefix []byte, nodeID NodeID) ([]EdgeID, error) {
	key := append(prefix, nodeID[:]...)
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return []EdgeID{}, nil
		}
		return nil, err
	}

	var edgeBytes [][]byte
	if err := json.Unmarshal(data, &edgeBytes); err != nil {
		return nil, err
	}

	edgeIDs := make([]EdgeID, len(edgeBytes))
	for i, b := range edgeBytes {
		copy(edgeIDs[i][:], b)
	}
	return edgeIDs, nil
}

func (s *GraphStorage) addToEdgeList(batch database.Batch, prefix []byte, nodeID NodeID, edgeID EdgeID) error {
	key := append(prefix, nodeID[:]...)
	existing, _ := s.getEdgeList(prefix, nodeID)
	existing = append(existing, edgeID)

	edgeBytes := make([][]byte, len(existing))
	for i, id := range existing {
		edgeBytes[i] = id[:]
	}
	data, err := json.Marshal(edgeBytes)
	if err != nil {
		return err
	}
	return batch.Put(key, data)
}

func (s *GraphStorage) removeFromEdgeList(batch database.Batch, prefix []byte, nodeID NodeID, edgeID EdgeID) error {
	key := append(prefix, nodeID[:]...)
	existing, _ := s.getEdgeList(prefix, nodeID)

	filtered := make([]EdgeID, 0, len(existing))
	for _, id := range existing {
		if id != edgeID {
			filtered = append(filtered, id)
		}
	}

	if len(filtered) == 0 {
		return batch.Delete(key)
	}

	edgeBytes := make([][]byte, len(filtered))
	for i, id := range filtered {
		edgeBytes[i] = id[:]
	}
	data, err := json.Marshal(edgeBytes)
	if err != nil {
		return err
	}
	return batch.Put(key, data)
}

func (s *GraphStorage) addToLabelIndex(batch database.Batch, prefix []byte, label string, id []byte) error {
	key := append(prefix, []byte(label)...)

	var existing [][]byte
	data, err := s.db.Get(key)
	if err == nil {
		json.Unmarshal(data, &existing)
	}

	existing = append(existing, id)
	data, err = json.Marshal(existing)
	if err != nil {
		return err
	}
	return batch.Put(key, data)
}

func (s *GraphStorage) removeFromLabelIndex(batch database.Batch, prefix []byte, label string, id []byte) error {
	key := append(prefix, []byte(label)...)

	var existing [][]byte
	data, err := s.db.Get(key)
	if err != nil {
		return nil // Nothing to remove
	}
	if err := json.Unmarshal(data, &existing); err != nil {
		return err
	}

	filtered := make([][]byte, 0, len(existing))
	for _, eid := range existing {
		if !bytes.Equal(eid, id) {
			filtered = append(filtered, eid)
		}
	}

	if len(filtered) == 0 {
		return batch.Delete(key)
	}

	data, err = json.Marshal(filtered)
	if err != nil {
		return err
	}
	return batch.Put(key, data)
}

func (s *GraphStorage) getCount(key []byte) (uint64, error) {
	data, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return 0, nil
		}
		return 0, err
	}
	return BytesToUint64(data), nil
}

func (s *GraphStorage) incrementCount(batch database.Batch, key []byte) error {
	count, _ := s.getCount(key)
	return batch.Put(key, Uint64ToBytes(count+1))
}

func (s *GraphStorage) decrementCount(batch database.Batch, key []byte) error {
	count, _ := s.getCount(key)
	if count > 0 {
		count--
	}
	return batch.Put(key, Uint64ToBytes(count))
}
