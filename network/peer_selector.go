// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"hash/fnv"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/wrappers"
)

// ConsistentHashPeerSelector implements O(1) peer selection using consistent hashing
type ConsistentHashPeerSelector struct {
	mu sync.RWMutex

	// Ring positions for each peer
	ring map[uint64]ids.NodeID

	// Sorted ring positions for binary search
	sortedPositions []uint64

	// Peer metadata for quick lookups
	peers map[ids.NodeID]*peerMetadata

	// Virtual nodes per peer for better distribution
	virtualNodes int
}

type peerMetadata struct {
	nodeID      ids.NodeID
	positions   []uint64
	lastUpdated time.Time
	weight      uint64
}

// NewConsistentHashPeerSelector creates an optimized peer selector
func NewConsistentHashPeerSelector(virtualNodes int) *ConsistentHashPeerSelector {
	if virtualNodes <= 0 {
		virtualNodes = 150 // Default for good distribution
	}

	return &ConsistentHashPeerSelector{
		ring:         make(map[uint64]ids.NodeID),
		peers:        make(map[ids.NodeID]*peerMetadata),
		virtualNodes: virtualNodes,
	}
}

// AddPeer adds a peer to the consistent hash ring - O(log n)
func (s *ConsistentHashPeerSelector) AddPeer(nodeID ids.NodeID, weight uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing if present
	s.removePeerLocked(nodeID)

	if weight == 0 {
		weight = 1
	}

	metadata := &peerMetadata{
		nodeID:      nodeID,
		positions:   make([]uint64, 0, s.virtualNodes),
		lastUpdated: time.Now(),
		weight:      weight,
	}

	// Add virtual nodes proportional to weight
	numVirtual := int(weight) * s.virtualNodes / 100
	if numVirtual < 1 {
		numVirtual = 1
	}

	// Generate positions on the ring
	for i := 0; i < numVirtual; i++ {
		pos := s.hashPosition(nodeID, i)
		s.ring[pos] = nodeID
		metadata.positions = append(metadata.positions, pos)
	}

	s.peers[nodeID] = metadata
	s.rebuildSortedPositions()
}

// RemovePeer removes a peer from the ring - O(log n)
func (s *ConsistentHashPeerSelector) RemovePeer(nodeID ids.NodeID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removePeerLocked(nodeID)
}

func (s *ConsistentHashPeerSelector) removePeerLocked(nodeID ids.NodeID) {
	metadata, exists := s.peers[nodeID]
	if !exists {
		return
	}

	// Remove all virtual nodes
	for _, pos := range metadata.positions {
		delete(s.ring, pos)
	}

	delete(s.peers, nodeID)
	s.rebuildSortedPositions()
}

// SelectPeers selects n peers using consistent hashing - O(n log k) where k is ring size
func (s *ConsistentHashPeerSelector) SelectPeers(key []byte, n int, exclude set.Set[ids.NodeID]) []ids.NodeID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.sortedPositions) == 0 {
		return nil
	}

	// Hash the key to find position on ring
	h := fnv.New64a()
	h.Write(key)
	position := h.Sum64()

	// Binary search for closest position
	idx := s.findClosestPosition(position)

	selected := make([]ids.NodeID, 0, n)
	seen := set.NewSet[ids.NodeID](n)

	// Walk the ring to find n unique peers
	for i := 0; len(selected) < n && i < len(s.sortedPositions); i++ {
		pos := s.sortedPositions[(idx+i)%len(s.sortedPositions)]
		nodeID := s.ring[pos]

		if !seen.Contains(nodeID) && !exclude.Contains(nodeID) {
			selected = append(selected, nodeID)
			seen.Add(nodeID)
		}
	}

	return selected
}

// SelectRandomPeers selects n random peers efficiently - O(n)
func (s *ConsistentHashPeerSelector) SelectRandomPeers(n int, exclude set.Set[ids.NodeID]) []ids.NodeID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get eligible peers
	eligible := make([]ids.NodeID, 0, len(s.peers))
	for nodeID := range s.peers {
		if !exclude.Contains(nodeID) {
			eligible = append(eligible, nodeID)
		}
	}

	if len(eligible) <= n {
		return eligible
	}

	// Fisher-Yates shuffle for first n elements
	for i := 0; i < n; i++ {
		j := rand.Intn(len(eligible)-i) + i
		eligible[i], eligible[j] = eligible[j], eligible[i]
	}

	return eligible[:n]
}

// GetPeersByWeight returns peers sorted by weight - O(n log n) but cached
func (s *ConsistentHashPeerSelector) GetPeersByWeight(limit int) []ids.NodeID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type weightedPeer struct {
		nodeID ids.NodeID
		weight uint64
	}

	weighted := make([]weightedPeer, 0, len(s.peers))
	for nodeID, metadata := range s.peers {
		weighted = append(weighted, weightedPeer{
			nodeID: nodeID,
			weight: metadata.weight,
		})
	}

	// Sort by weight descending
	sort.Slice(weighted, func(i, j int) bool {
		return weighted[i].weight > weighted[j].weight
	})

	// Extract nodeIDs
	result := make([]ids.NodeID, 0, limit)
	for i := 0; i < len(weighted) && i < limit; i++ {
		result = append(result, weighted[i].nodeID)
	}

	return result
}

// findClosestPosition uses binary search to find the closest position on the ring
func (s *ConsistentHashPeerSelector) findClosestPosition(target uint64) int {
	left, right := 0, len(s.sortedPositions)-1

	for left <= right {
		mid := (left + right) / 2
		if s.sortedPositions[mid] == target {
			return mid
		}
		if s.sortedPositions[mid] < target {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	// Return the next position in clockwise direction
	if left >= len(s.sortedPositions) {
		return 0
	}
	return left
}

// hashPosition generates a position on the ring for a peer's virtual node
func (s *ConsistentHashPeerSelector) hashPosition(nodeID ids.NodeID, virtualIndex int) uint64 {
	h := fnv.New64a()
	h.Write(nodeID[:])

	// Write virtual index bytes
	indexBytes := []byte{
		byte(virtualIndex >> 24),
		byte(virtualIndex >> 16),
		byte(virtualIndex >> 8),
		byte(virtualIndex),
	}
	h.Write(indexBytes)

	return h.Sum64()
}

// rebuildSortedPositions rebuilds the sorted position list for binary search
func (s *ConsistentHashPeerSelector) rebuildSortedPositions() {
	s.sortedPositions = make([]uint64, 0, len(s.ring))
	for pos := range s.ring {
		s.sortedPositions = append(s.sortedPositions, pos)
	}
	sort.Slice(s.sortedPositions, func(i, j int) bool {
		return s.sortedPositions[i] < s.sortedPositions[j]
	})
}

// Size returns the number of unique peers
func (s *ConsistentHashPeerSelector) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.peers)
}

// Contains checks if a peer exists in the selector
func (s *ConsistentHashPeerSelector) Contains(nodeID ids.NodeID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.peers[nodeID]
	return exists
}