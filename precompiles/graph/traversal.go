// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package graph

import (
	"container/list"
)

// TraversalDirection specifies the direction of edge traversal.
type TraversalDirection int

const (
	DirectionOutgoing TraversalDirection = iota
	DirectionIncoming
	DirectionBoth
)

// TraversalOptions configures graph traversal behavior.
type TraversalOptions struct {
	Direction TraversalDirection
	MaxDepth  int
	MaxNodes  int
	EdgeLabel string // Filter by edge label (empty = all)
}

// DefaultTraversalOptions returns sensible defaults.
func DefaultTraversalOptions() TraversalOptions {
	return TraversalOptions{
		Direction: DirectionOutgoing,
		MaxDepth:  10,
		MaxNodes:  1000,
	}
}

// BFS performs breadth-first search starting from the given node.
// Returns all visited nodes in BFS order with their depths.
func (s *GraphStorage) BFS(startID NodeID, opts TraversalOptions) ([]TraversalResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Verify start node exists
	if _, err := s.getNodeUnlocked(startID); err != nil {
		return nil, err
	}

	visited := make(map[NodeID]bool)
	results := make([]TraversalResult, 0)
	queue := list.New()

	// Initialize with start node
	queue.PushBack(struct {
		id    NodeID
		path  []NodeID
		depth int
	}{startID, []NodeID{startID}, 0})
	visited[startID] = true

	for queue.Len() > 0 && len(results) < opts.MaxNodes {
		elem := queue.Front()
		queue.Remove(elem)
		current := elem.Value.(struct {
			id    NodeID
			path  []NodeID
			depth int
		})

		// Add to results
		results = append(results, TraversalResult{
			Path:  current.path,
			Depth: current.depth,
		})

		// Check depth limit
		if current.depth >= opts.MaxDepth {
			continue
		}

		// Get neighbors based on direction
		neighbors, err := s.getNeighbors(current.id, opts.Direction, opts.EdgeLabel)
		if err != nil {
			continue
		}

		for _, neighborID := range neighbors {
			if !visited[neighborID] {
				visited[neighborID] = true
				newPath := make([]NodeID, len(current.path)+1)
				copy(newPath, current.path)
				newPath[len(current.path)] = neighborID
				queue.PushBack(struct {
					id    NodeID
					path  []NodeID
					depth int
				}{neighborID, newPath, current.depth + 1})
			}
		}
	}

	return results, nil
}

// DFS performs depth-first search starting from the given node.
// Returns all visited nodes in DFS order with their depths.
func (s *GraphStorage) DFS(startID NodeID, opts TraversalOptions) ([]TraversalResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Verify start node exists
	if _, err := s.getNodeUnlocked(startID); err != nil {
		return nil, err
	}

	visited := make(map[NodeID]bool)
	results := make([]TraversalResult, 0)

	var dfsRecursive func(id NodeID, path []NodeID, depth int)
	dfsRecursive = func(id NodeID, path []NodeID, depth int) {
		if visited[id] || len(results) >= opts.MaxNodes {
			return
		}

		visited[id] = true
		results = append(results, TraversalResult{
			Path:  path,
			Depth: depth,
		})

		if depth >= opts.MaxDepth {
			return
		}

		neighbors, err := s.getNeighbors(id, opts.Direction, opts.EdgeLabel)
		if err != nil {
			return
		}

		for _, neighborID := range neighbors {
			if !visited[neighborID] {
				newPath := make([]NodeID, len(path)+1)
				copy(newPath, path)
				newPath[len(path)] = neighborID
				dfsRecursive(neighborID, newPath, depth+1)
			}
		}
	}

	dfsRecursive(startID, []NodeID{startID}, 0)
	return results, nil
}

// ShortestPath finds the shortest path between two nodes using BFS.
// Returns the path and an error if no path exists.
func (s *GraphStorage) ShortestPath(fromID, toID NodeID, opts TraversalOptions) ([]NodeID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Verify both nodes exist
	if _, err := s.getNodeUnlocked(fromID); err != nil {
		return nil, err
	}
	if _, err := s.getNodeUnlocked(toID); err != nil {
		return nil, err
	}

	if fromID == toID {
		return []NodeID{fromID}, nil
	}

	visited := make(map[NodeID]bool)
	queue := list.New()

	queue.PushBack(struct {
		id   NodeID
		path []NodeID
	}{fromID, []NodeID{fromID}})
	visited[fromID] = true

	for queue.Len() > 0 {
		elem := queue.Front()
		queue.Remove(elem)
		current := elem.Value.(struct {
			id   NodeID
			path []NodeID
		})

		if len(current.path) > opts.MaxDepth {
			continue
		}

		neighbors, err := s.getNeighbors(current.id, opts.Direction, opts.EdgeLabel)
		if err != nil {
			continue
		}

		for _, neighborID := range neighbors {
			if neighborID == toID {
				return append(current.path, neighborID), nil
			}
			if !visited[neighborID] {
				visited[neighborID] = true
				newPath := make([]NodeID, len(current.path)+1)
				copy(newPath, current.path)
				newPath[len(current.path)] = neighborID
				queue.PushBack(struct {
					id   NodeID
					path []NodeID
				}{neighborID, newPath})
			}
		}
	}

	return nil, ErrNodeNotFound
}

// getNeighbors returns neighbor node IDs based on traversal direction.
func (s *GraphStorage) getNeighbors(nodeID NodeID, direction TraversalDirection, labelFilter string) ([]NodeID, error) {
	neighbors := make([]NodeID, 0)

	if direction == DirectionOutgoing || direction == DirectionBoth {
		outEdges, err := s.getEdgeList(prefixOutgoing, nodeID)
		if err != nil {
			return nil, err
		}
		for _, edgeID := range outEdges {
			edge, err := s.getEdgeUnlocked(edgeID)
			if err != nil {
				continue
			}
			if labelFilter == "" || edge.Label == labelFilter {
				neighbors = append(neighbors, edge.To)
			}
		}
	}

	if direction == DirectionIncoming || direction == DirectionBoth {
		inEdges, err := s.getEdgeList(prefixIncoming, nodeID)
		if err != nil {
			return nil, err
		}
		for _, edgeID := range inEdges {
			edge, err := s.getEdgeUnlocked(edgeID)
			if err != nil {
				continue
			}
			if labelFilter == "" || edge.Label == labelFilter {
				neighbors = append(neighbors, edge.From)
			}
		}
	}

	return neighbors, nil
}

// ReachableNodes returns all nodes reachable from the start node within maxDepth.
func (s *GraphStorage) ReachableNodes(startID NodeID, opts TraversalOptions) ([]NodeID, error) {
	results, err := s.BFS(startID, opts)
	if err != nil {
		return nil, err
	}

	// Extract unique node IDs from paths (last node in each path)
	nodeSet := make(map[NodeID]bool)
	for _, result := range results {
		if len(result.Path) > 0 {
			nodeSet[result.Path[len(result.Path)-1]] = true
		}
	}

	nodes := make([]NodeID, 0, len(nodeSet))
	for id := range nodeSet {
		nodes = append(nodes, id)
	}
	return nodes, nil
}

// HasCycle checks if there is a cycle in the graph starting from the given node.
func (s *GraphStorage) HasCycle(startID NodeID) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Verify start node exists
	if _, err := s.getNodeUnlocked(startID); err != nil {
		return false, err
	}

	// Track visiting state: 0 = unvisited, 1 = in current path, 2 = fully visited
	state := make(map[NodeID]int)

	var hasCycle func(id NodeID) bool
	hasCycle = func(id NodeID) bool {
		if state[id] == 1 {
			return true // Back edge found
		}
		if state[id] == 2 {
			return false // Already fully explored
		}

		state[id] = 1 // Mark as in current path

		neighbors, err := s.getNeighbors(id, DirectionOutgoing, "")
		if err == nil {
			for _, neighborID := range neighbors {
				if hasCycle(neighborID) {
					return true
				}
			}
		}

		state[id] = 2 // Mark as fully visited
		return false
	}

	return hasCycle(startID), nil
}
