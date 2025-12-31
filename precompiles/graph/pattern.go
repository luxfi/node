// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package graph

import (
	"bytes"
)

// SubgraphMatch finds all subgraphs matching the given pattern.
// Uses backtracking with constraint satisfaction.
func (s *GraphStorage) SubgraphMatch(pattern SubgraphPattern, maxResults int) ([]MatchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(pattern.Nodes) == 0 {
		return nil, ErrPatternInvalid
	}

	if maxResults <= 0 {
		maxResults = 100 // Default limit
	}

	results := make([]MatchResult, 0)

	// Build adjacency requirements from pattern edges
	adjacencyRequired := make(map[string][]struct {
		toVar string
		edge  PatternEdge
	})
	for _, edge := range pattern.Edges {
		adjacencyRequired[edge.FromVar] = append(adjacencyRequired[edge.FromVar], struct {
			toVar string
			edge  PatternEdge
		}{edge.ToVar, edge})
	}

	// Get candidate nodes for each pattern variable
	candidates := make(map[string][]NodeID)
	for _, pn := range pattern.Nodes {
		var nodeIDs []NodeID
		if pn.Label != "" {
			nodes, err := s.QueryNodesByLabel(pn.Label)
			if err != nil {
				return nil, err
			}
			for _, n := range nodes {
				if s.matchesProperties(n.Properties, pn.Properties) {
					nodeIDs = append(nodeIDs, n.ID)
				}
			}
		} else {
			// No label constraint - iterate all nodes
			iter := s.db.NewIteratorWithPrefix(prefixNode)
			defer iter.Release()
			for iter.Next() {
				node, err := DeserializeNode(iter.Value())
				if err != nil {
					continue
				}
				if s.matchesProperties(node.Properties, pn.Properties) {
					nodeIDs = append(nodeIDs, node.ID)
				}
			}
		}
		candidates[pn.Var] = nodeIDs
	}

	// Backtracking search
	bindings := make(map[string]NodeID)
	usedNodes := make(map[NodeID]bool)
	varOrder := make([]string, len(pattern.Nodes))
	for i, pn := range pattern.Nodes {
		varOrder[i] = pn.Var
	}

	var search func(depth int)
	search = func(depth int) {
		if len(results) >= maxResults {
			return
		}

		if depth == len(varOrder) {
			// All variables bound - check edge constraints
			if s.checkEdgeConstraints(bindings, pattern.Edges) {
				result := MatchResult{Bindings: make(map[string]NodeID)}
				for k, v := range bindings {
					result.Bindings[k] = v
				}
				results = append(results, result)
			}
			return
		}

		varName := varOrder[depth]
		for _, candidateID := range candidates[varName] {
			if usedNodes[candidateID] {
				continue // Ensure injective mapping
			}

			// Early pruning: check edges to already-bound variables
			valid := true
			if adjs, ok := adjacencyRequired[varName]; ok {
				for _, adj := range adjs {
					if boundID, bound := bindings[adj.toVar]; bound {
						if !s.hasMatchingEdge(candidateID, boundID, adj.edge) {
							valid = false
							break
						}
					}
				}
			}

			if !valid {
				continue
			}

			bindings[varName] = candidateID
			usedNodes[candidateID] = true
			search(depth + 1)
			delete(bindings, varName)
			delete(usedNodes, candidateID)
		}
	}

	search(0)
	return results, nil
}

// matchesProperties checks if actual properties satisfy required constraints.
func (s *GraphStorage) matchesProperties(actual, required map[string][]byte) bool {
	for key, reqValue := range required {
		actValue, exists := actual[key]
		if !exists {
			return false
		}
		if !bytes.Equal(actValue, reqValue) {
			return false
		}
	}
	return true
}

// checkEdgeConstraints verifies all edge constraints are satisfied.
func (s *GraphStorage) checkEdgeConstraints(bindings map[string]NodeID, edges []PatternEdge) bool {
	for _, pe := range edges {
		fromID, fromOK := bindings[pe.FromVar]
		toID, toOK := bindings[pe.ToVar]
		if !fromOK || !toOK {
			return false // Missing binding
		}
		if !s.hasMatchingEdge(fromID, toID, pe) {
			return false
		}
	}
	return true
}

// hasMatchingEdge checks if there's an edge from fromID to toID matching constraints.
func (s *GraphStorage) hasMatchingEdge(fromID, toID NodeID, constraint PatternEdge) bool {
	edgeIDs, err := s.getEdgeList(prefixOutgoing, fromID)
	if err != nil {
		return false
	}

	for _, edgeID := range edgeIDs {
		edge, err := s.getEdgeUnlocked(edgeID)
		if err != nil {
			continue
		}
		if edge.To != toID {
			continue
		}
		if constraint.Label != "" && edge.Label != constraint.Label {
			continue
		}
		if !s.matchesProperties(edge.Properties, constraint.Properties) {
			continue
		}
		return true
	}
	return false
}

// TriangleCount counts the number of triangles in the graph.
// A triangle is a cycle of exactly 3 nodes.
func (s *GraphStorage) TriangleCount() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect all nodes
	nodeIDs := make([]NodeID, 0)
	iter := s.db.NewIteratorWithPrefix(prefixNode)
	defer iter.Release()
	for iter.Next() {
		node, err := DeserializeNode(iter.Value())
		if err != nil {
			continue
		}
		nodeIDs = append(nodeIDs, node.ID)
	}

	// Build adjacency set for efficient lookup
	adjacency := make(map[NodeID]map[NodeID]bool)
	for _, id := range nodeIDs {
		adjacency[id] = make(map[NodeID]bool)
		neighbors, _ := s.getNeighbors(id, DirectionBoth, "")
		for _, n := range neighbors {
			adjacency[id][n] = true
		}
	}

	// Count triangles (each triangle counted 6 times, divide by 6)
	var count uint64
	for i, a := range nodeIDs {
		for j := i + 1; j < len(nodeIDs); j++ {
			b := nodeIDs[j]
			if !adjacency[a][b] {
				continue
			}
			for k := j + 1; k < len(nodeIDs); k++ {
				c := nodeIDs[k]
				if adjacency[a][c] && adjacency[b][c] {
					count++
				}
			}
		}
	}

	return count, nil
}

// ConnectedComponents returns the number of connected components.
// Uses union-find algorithm.
func (s *GraphStorage) ConnectedComponents() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect all nodes
	nodeIDs := make([]NodeID, 0)
	nodeIndex := make(map[NodeID]int)
	iter := s.db.NewIteratorWithPrefix(prefixNode)
	defer iter.Release()
	for iter.Next() {
		node, err := DeserializeNode(iter.Value())
		if err != nil {
			continue
		}
		nodeIndex[node.ID] = len(nodeIDs)
		nodeIDs = append(nodeIDs, node.ID)
	}

	if len(nodeIDs) == 0 {
		return 0, nil
	}

	// Union-Find with path compression
	parent := make([]int, len(nodeIDs))
	rank := make([]int, len(nodeIDs))
	for i := range parent {
		parent[i] = i
	}

	var find func(i int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i]) // Path compression
		}
		return parent[i]
	}

	union := func(i, j int) {
		pi, pj := find(i), find(j)
		if pi == pj {
			return
		}
		if rank[pi] < rank[pj] {
			parent[pi] = pj
		} else if rank[pi] > rank[pj] {
			parent[pj] = pi
		} else {
			parent[pj] = pi
			rank[pi]++
		}
	}

	// Union all connected pairs
	for i, id := range nodeIDs {
		neighbors, _ := s.getNeighbors(id, DirectionBoth, "")
		for _, n := range neighbors {
			if j, ok := nodeIndex[n]; ok {
				union(i, j)
			}
		}
	}

	// Count unique roots
	roots := make(map[int]bool)
	for i := range nodeIDs {
		roots[find(i)] = true
	}

	return len(roots), nil
}
