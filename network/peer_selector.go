// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
)

const (
	defaultVirtualNodes      = 150
	defaultMaxVirtualPerPeer = 4096
)

// SelectorKey is the scoping key for peer selection.
// In the Lux model this should be the SequencerID (validator-set identity).
// It can be different from a chainID (execution domain).
type SelectorKey = ids.ID

// ScopedConsistentHashSelector maintains independent rings keyed by SelectorKey.
// This enables proper peer selection based on the validator set authority
// (sequencerID) rather than just the chain/execution domain.
type ScopedConsistentHashSelector struct {
	mu sync.RWMutex

	virtualNodes      int
	maxVirtualPerPeer int

	// rings[key] -> ring
	rings map[SelectorKey]*consistentRing
}

type consistentRing struct {
	// Ring positions for each peer virtual node.
	ring map[uint64]ids.NodeID

	// Sorted ring positions for binary search.
	sorted []uint64

	// Peer metadata for quick removal.
	peers map[ids.NodeID]*peerMeta

	lastRebuilt time.Time
}

type peerMeta struct {
	positions []uint64
	weight    uint64
	updatedAt time.Time
}

// NewScopedConsistentHashSelector creates a scoped peer selector that maintains
// separate consistent hash rings per sequencerID (validator-set identity).
func NewScopedConsistentHashSelector(virtualNodes int) *ScopedConsistentHashSelector {
	if virtualNodes <= 0 {
		virtualNodes = defaultVirtualNodes
	}
	return &ScopedConsistentHashSelector{
		virtualNodes:      virtualNodes,
		maxVirtualPerPeer: defaultMaxVirtualPerPeer,
		rings:             make(map[SelectorKey]*consistentRing),
	}
}

func (s *ScopedConsistentHashSelector) ringFor(key SelectorKey) *consistentRing {
	r, ok := s.rings[key]
	if ok {
		return r
	}
	r = &consistentRing{
		ring:  make(map[uint64]ids.NodeID),
		peers: make(map[ids.NodeID]*peerMeta),
	}
	s.rings[key] = r
	return r
}

// UpsertPeer adds/updates a peer in the ring for [key].
// O(V log R) where V=virtual nodes for this peer, R=ring size.
// If you batch many updates, call Rebuild(key) once at the end to amortize.
func (s *ScopedConsistentHashSelector) UpsertPeer(key SelectorKey, nodeID ids.NodeID, weight uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r := s.ringFor(key)
	r.removePeerLocked(nodeID)

	if weight == 0 {
		weight = 1
	}

	numVirtual := s.virtualCount(weight)
	positions := make([]uint64, 0, numVirtual)

	for i := 0; i < numVirtual; i++ {
		pos := hashPosition(key, nodeID, uint32(i))
		r.ring[pos] = nodeID
		positions = append(positions, pos)
	}

	r.peers[nodeID] = &peerMeta{
		positions: positions,
		weight:    weight,
		updatedAt: time.Now(),
	}

	r.rebuildSortedLocked()
}

// RemovePeer removes a peer from the ring for [key].
func (s *ScopedConsistentHashSelector) RemovePeer(key SelectorKey, nodeID ids.NodeID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.rings[key]
	if !ok {
		return
	}
	r.removePeerLocked(nodeID)
	r.rebuildSortedLocked()

	// Cleanup empty rings
	if len(r.peers) == 0 {
		delete(s.rings, key)
	}
}

func (r *consistentRing) removePeerLocked(nodeID ids.NodeID) {
	meta, exists := r.peers[nodeID]
	if !exists {
		return
	}
	for _, pos := range meta.positions {
		delete(r.ring, pos)
	}
	delete(r.peers, nodeID)
}

func (r *consistentRing) rebuildSortedLocked() {
	r.sorted = make([]uint64, 0, len(r.ring))
	for pos := range r.ring {
		r.sorted = append(r.sorted, pos)
	}
	sort.Slice(r.sorted, func(i, j int) bool { return r.sorted[i] < r.sorted[j] })
	r.lastRebuilt = time.Now()
}

// SelectDeterministic selects up to n peers from the ring for [key] based on [msgKey].
// This is stable across nodes given the same ring content + msgKey.
// Exclude applies after mapping.
func (s *ScopedConsistentHashSelector) SelectDeterministic(
	key SelectorKey,
	msgKey []byte,
	n int,
	exclude set.Set[ids.NodeID],
) []ids.NodeID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.rings[key]
	if !ok || len(r.sorted) == 0 || n <= 0 {
		return nil
	}

	position := hash64(msgKey)
	idx := findClosest(r.sorted, position)

	selected := make([]ids.NodeID, 0, n)
	seen := set.NewSet[ids.NodeID](n)

	for i := 0; len(selected) < n && i < len(r.sorted); i++ {
		pos := r.sorted[(idx+i)%len(r.sorted)]
		nodeID := r.ring[pos]
		if exclude.Contains(nodeID) || seen.Contains(nodeID) {
			continue
		}
		seen.Add(nodeID)
		selected = append(selected, nodeID)
	}

	return selected
}

// SelectPseudoRandom selects n peers by hashing (seed || i) and walking the ring.
// Deterministic given seed + ring content (no global rand).
func (s *ScopedConsistentHashSelector) SelectPseudoRandom(
	key SelectorKey,
	seed []byte,
	n int,
	exclude set.Set[ids.NodeID],
) []ids.NodeID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.rings[key]
	if !ok || len(r.sorted) == 0 || n <= 0 {
		return nil
	}

	selected := make([]ids.NodeID, 0, n)
	seen := set.NewSet[ids.NodeID](n)

	// Try up to ring size * 2 steps to avoid infinite loops.
	tries := 0
	for len(selected) < n && tries < len(r.sorted)*2 {
		tries++

		k := make([]byte, len(seed)+4)
		copy(k, seed)
		binary.BigEndian.PutUint32(k[len(seed):], uint32(tries))

		position := hash64(k)
		idx := findClosest(r.sorted, position)

		// Walk until you hit an unseen eligible peer
		for step := 0; step < len(r.sorted); step++ {
			pos := r.sorted[(idx+step)%len(r.sorted)]
			nodeID := r.ring[pos]
			if exclude.Contains(nodeID) || seen.Contains(nodeID) {
				continue
			}
			seen.Add(nodeID)
			selected = append(selected, nodeID)
			break
		}
	}

	return selected
}

// TopByWeight returns peers sorted by weight in the ring for [key].
func (s *ScopedConsistentHashSelector) TopByWeight(key SelectorKey, limit int) []ids.NodeID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.rings[key]
	if !ok || limit <= 0 {
		return nil
	}

	type wp struct {
		id     ids.NodeID
		weight uint64
	}
	weighted := make([]wp, 0, len(r.peers))
	for id, meta := range r.peers {
		weighted = append(weighted, wp{id: id, weight: meta.weight})
	}

	sort.Slice(weighted, func(i, j int) bool { return weighted[i].weight > weighted[j].weight })

	if limit > len(weighted) {
		limit = len(weighted)
	}
	out := make([]ids.NodeID, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, weighted[i].id)
	}
	return out
}

// Size returns the number of unique peers in the ring for [key].
func (s *ScopedConsistentHashSelector) Size(key SelectorKey) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rings[key]
	if !ok {
		return 0
	}
	return len(r.peers)
}

// TotalSize returns the total number of unique peers across all rings.
func (s *ScopedConsistentHashSelector) TotalSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, r := range s.rings {
		total += len(r.peers)
	}
	return total
}

// Contains checks if a peer exists in the ring for [key].
func (s *ScopedConsistentHashSelector) Contains(key SelectorKey, nodeID ids.NodeID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rings[key]
	if !ok {
		return false
	}
	_, exists := r.peers[nodeID]
	return exists
}

// Keys returns all selector keys (sequencerIDs) with active rings.
func (s *ScopedConsistentHashSelector) Keys() []SelectorKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]SelectorKey, 0, len(s.rings))
	for k := range s.rings {
		keys = append(keys, k)
	}
	return keys
}

// Clear removes all peers from the ring for [key].
func (s *ScopedConsistentHashSelector) Clear(key SelectorKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rings, key)
}

// ClearAll removes all rings.
func (s *ScopedConsistentHashSelector) ClearAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rings = make(map[SelectorKey]*consistentRing)
}

// virtualCount maps stake/weight to a bounded number of virtual nodes.
// Uses log scale for stability - huge weights grow slowly, not explosively.
func (s *ScopedConsistentHashSelector) virtualCount(weight uint64) int {
	// Normalize weight using log scale for stability.
	// w=1 -> ~1.0
	// huge w -> grows slowly
	w := float64(weight)
	scale := math.Log2(w + 1)

	base := float64(s.virtualNodes)
	v := int(base * scale)

	if v < 1 {
		v = 1
	}
	if v > s.maxVirtualPerPeer {
		v = s.maxVirtualPerPeer
	}
	return v
}

func hash64(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

func hashPosition(key SelectorKey, nodeID ids.NodeID, v uint32) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(key[:])
	_, _ = h.Write(nodeID[:])

	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	_, _ = h.Write(buf[:])

	return h.Sum64()
}

func findClosest(sorted []uint64, target uint64) int {
	left, right := 0, len(sorted)-1
	for left <= right {
		mid := (left + right) / 2
		if sorted[mid] == target {
			return mid
		}
		if sorted[mid] < target {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}
	if left >= len(sorted) {
		return 0
	}
	return left
}

// Legacy compatibility: ConsistentHashPeerSelector is an alias for backward compatibility.
// Use ScopedConsistentHashSelector with the PrimaryNetworkID as key for equivalent behavior.
type ConsistentHashPeerSelector = ScopedConsistentHashSelector

// NewConsistentHashPeerSelector creates a new selector for backward compatibility.
// Deprecated: Use NewScopedConsistentHashSelector instead.
func NewConsistentHashPeerSelector(virtualNodes int) *ConsistentHashPeerSelector {
	return NewScopedConsistentHashSelector(virtualNodes)
}
