// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package servicenodevm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
)

var (
	nodePrefix       = []byte("node:")
	nodeByPubKey     = []byte("nodepk:")
	activeNodesKey   = []byte("active_nodes")
)

// Registry manages service node registration and state
type Registry struct {
	db     database.Database
	config *Config

	// In-memory state
	nodes       map[ids.NodeID]*ServiceNode
	nodesByPK   map[[32]byte]ids.NodeID // publicKey hash -> nodeID
	activeNodes []ids.NodeID

	mu sync.RWMutex
}

// NewRegistry creates a new service node registry
func NewRegistry(db database.Database, config *Config) *Registry {
	return &Registry{
		db:          db,
		config:      config,
		nodes:       make(map[ids.NodeID]*ServiceNode),
		nodesByPK:   make(map[[32]byte]ids.NodeID),
		activeNodes: make([]ids.NodeID, 0),
	}
}

// Load loads the registry state from the database
func (r *Registry) Load(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Load active nodes list
	activeData, err := r.db.Get(activeNodesKey)
	if err != nil && err != database.ErrNotFound {
		return err
	}

	if len(activeData) > 0 {
		if err := json.Unmarshal(activeData, &r.activeNodes); err != nil {
			return err
		}
	}

	// Load each active node
	for _, nodeID := range r.activeNodes {
		key := append(nodePrefix, nodeID[:]...)
		nodeData, err := r.db.Get(key)
		if err != nil {
			continue
		}

		var node ServiceNode
		if err := json.Unmarshal(nodeData, &node); err != nil {
			continue
		}

		r.nodes[nodeID] = &node
		pkHash := sha256.Sum256(node.PublicKey)
		r.nodesByPK[pkHash] = nodeID
	}

	return nil
}

// Register registers a new service node
func (r *Registry) Register(ctx context.Context, tx *RegistrationTx) (*ServiceNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if node already exists
	if _, exists := r.nodes[tx.NodeID]; exists {
		return nil, ErrNodeAlreadyExists
	}

	// Check minimum stake
	if tx.StakeAmount < r.config.MinStake {
		return nil, ErrInsufficientStake
	}

	// Generate node ID from public key if not provided
	h := sha256.New()
	h.Write(tx.PublicKey)
	h.Write(tx.NodeID[:])
	binary.Write(h, binary.BigEndian, time.Now().UnixNano())
	nodeIDHash := h.Sum(nil)
	serviceNodeID := ids.ID{}
	copy(serviceNodeID[:], nodeIDHash)

	now := time.Now()
	node := &ServiceNode{
		ID:             serviceNodeID,
		NodeID:         tx.NodeID,
		PublicKey:      tx.PublicKey,
		EndpointHash:   tx.EndpointHash,
		StakeAmount:    tx.StakeAmount,
		StakeLockEnd:   tx.StakeLockEnd,
		State:          StateRegistered,
		RegisteredAt:   now,
		LastActiveAt:   now,
		UptimeScore:    10000, // Start with 100%
	}

	// Persist node
	nodeData, err := json.Marshal(node)
	if err != nil {
		return nil, err
	}

	key := append(nodePrefix, tx.NodeID[:]...)
	if err := r.db.Put(key, nodeData); err != nil {
		return nil, err
	}

	// Update in-memory state
	r.nodes[tx.NodeID] = node
	pkHash := sha256.Sum256(tx.PublicKey)
	r.nodesByPK[pkHash] = tx.NodeID

	return node, nil
}

// Activate activates a registered service node
func (r *Registry) Activate(ctx context.Context, nodeID ids.NodeID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	if node.State != StateRegistered && node.State != StateJailed {
		return ErrNodeNotActive
	}

	// Check if jail period has ended
	if node.State == StateJailed {
		if uint64(time.Now().Unix()) < node.JailRelease {
			return ErrNodeJailed
		}
	}

	node.State = StateActive
	node.ActivationTime = uint64(time.Now().Unix())
	node.LastActiveAt = time.Now()

	// Persist
	if err := r.persistNode(node); err != nil {
		return err
	}

	// Add to active list
	r.activeNodes = append(r.activeNodes, nodeID)
	return r.persistActiveNodes()
}

// Deactivate deactivates a service node (starts exit process)
func (r *Registry) Deactivate(ctx context.Context, nodeID ids.NodeID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	if node.State != StateActive {
		return ErrNodeNotActive
	}

	node.State = StateExiting

	// Persist
	if err := r.persistNode(node); err != nil {
		return err
	}

	return nil
}

// Exit completes the exit process for a node
func (r *Registry) Exit(ctx context.Context, nodeID ids.NodeID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Must be in exiting state and stake lock must have expired
	if node.State != StateExiting {
		return ErrNodeNotActive
	}

	if uint64(time.Now().Unix()) < node.StakeLockEnd {
		return ErrInsufficientStake // Stake still locked
	}

	node.State = StateExited

	// Persist
	if err := r.persistNode(node); err != nil {
		return err
	}

	// Remove from active list
	r.removeFromActiveList(nodeID)
	return r.persistActiveNodes()
}

// Jail jails a service node for misbehavior
func (r *Registry) Jail(ctx context.Context, nodeID ids.NodeID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	node.State = StateJailed
	node.JailReason = reason
	node.JailedAt = time.Now()
	node.JailRelease = uint64(time.Now().Unix()) + uint64(r.config.JailDuration)

	// Persist
	if err := r.persistNode(node); err != nil {
		return err
	}

	// Remove from active list
	r.removeFromActiveList(nodeID)
	return r.persistActiveNodes()
}

// Slash slashes stake from a misbehaving node
func (r *Registry) Slash(ctx context.Context, nodeID ids.NodeID, amount uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	if amount > node.StakeAmount {
		amount = node.StakeAmount
	}

	node.StakeAmount -= amount

	// If stake drops below minimum, jail the node
	if node.StakeAmount < r.config.MinStake {
		node.State = StateJailed
		node.JailReason = "insufficient stake after slashing"
		node.JailedAt = time.Now()
		r.removeFromActiveList(nodeID)
		if err := r.persistActiveNodes(); err != nil {
			return err
		}
	}

	return r.persistNode(node)
}

// UpdateUptime updates the uptime metrics for a node
func (r *Registry) UpdateUptime(ctx context.Context, nodeID ids.NodeID, passed bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	if passed {
		node.ChallengesPassed++
		// Increase uptime score (max 10000)
		if node.UptimeScore < 10000 {
			node.UptimeScore += 10
			if node.UptimeScore > 10000 {
				node.UptimeScore = 10000
			}
		}
	} else {
		node.ChallengesFailed++
		// Decrease uptime score
		if node.UptimeScore > 100 {
			node.UptimeScore -= 100
		} else {
			node.UptimeScore = 0
		}

		// Check if should be jailed
		if node.ChallengesFailed >= uint64(r.config.MaxFailedChallenges) {
			node.State = StateJailed
			node.JailReason = "too many failed challenges"
			node.JailedAt = time.Now()
			node.JailRelease = uint64(time.Now().Unix()) + uint64(r.config.JailDuration)
			r.removeFromActiveList(nodeID)
			if err := r.persistActiveNodes(); err != nil {
				return err
			}
		}
	}

	node.LastActiveAt = time.Now()
	return r.persistNode(node)
}

// AddReward adds pending rewards to a node
func (r *Registry) AddReward(ctx context.Context, nodeID ids.NodeID, amount uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	node.PendingRewards += amount
	return r.persistNode(node)
}

// ClaimRewards claims and resets pending rewards
func (r *Registry) ClaimRewards(ctx context.Context, nodeID ids.NodeID) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return 0, ErrNodeNotFound
	}

	rewards := node.PendingRewards
	node.PendingRewards = 0
	node.TotalRewards += rewards

	if err := r.persistNode(node); err != nil {
		return 0, err
	}

	return rewards, nil
}

// Get retrieves a service node by node ID
func (r *Registry) Get(nodeID ids.NodeID) (*ServiceNode, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return nil, ErrNodeNotFound
	}

	return node, nil
}

// GetByPublicKey retrieves a service node by public key
func (r *Registry) GetByPublicKey(publicKey []byte) (*ServiceNode, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pkHash := sha256.Sum256(publicKey)
	nodeID, exists := r.nodesByPK[pkHash]
	if !exists {
		return nil, ErrNodeNotFound
	}

	return r.nodes[nodeID], nil
}

// GetActiveNodes returns all active service nodes
func (r *Registry) GetActiveNodes() []*ServiceNode {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]*ServiceNode, 0, len(r.activeNodes))
	for _, nodeID := range r.activeNodes {
		if node, exists := r.nodes[nodeID]; exists && node.IsActive() {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// GetActiveNodeIDs returns the IDs of all active service nodes
func (r *Registry) GetActiveNodeIDs() []ids.NodeID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodeIDs := make([]ids.NodeID, 0, len(r.activeNodes))
	for _, nodeID := range r.activeNodes {
		if node, exists := r.nodes[nodeID]; exists && node.IsActive() {
			nodeIDs = append(nodeIDs, nodeID)
		}
	}

	return nodeIDs
}

// GetActiveNodeCount returns the count of active nodes
func (r *Registry) GetActiveNodeCount() uint32 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := uint32(0)
	for _, nodeID := range r.activeNodes {
		if node, exists := r.nodes[nodeID]; exists && node.IsActive() {
			count++
		}
	}

	return count
}

// ComputeRegistryRoot computes the Merkle root of the registry
func (r *Registry) ComputeRegistryRoot() [32]byte {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Sort node IDs for deterministic ordering
	sortedIDs := make([]ids.NodeID, 0, len(r.activeNodes))
	for _, nodeID := range r.activeNodes {
		if node, exists := r.nodes[nodeID]; exists && node.IsActive() {
			sortedIDs = append(sortedIDs, nodeID)
		}
	}

	sort.Slice(sortedIDs, func(i, j int) bool {
		return string(sortedIDs[i][:]) < string(sortedIDs[j][:])
	})

	// Compute leaf hashes
	leaves := make([][]byte, len(sortedIDs))
	for i, nodeID := range sortedIDs {
		node := r.nodes[nodeID]
		nodeHash := node.Hash()
		leaves[i] = nodeHash[:]
	}

	return computeMerkleRoot(leaves)
}

// persistNode persists a node to the database
func (r *Registry) persistNode(node *ServiceNode) error {
	nodeData, err := json.Marshal(node)
	if err != nil {
		return err
	}

	key := append(nodePrefix, node.NodeID[:]...)
	return r.db.Put(key, nodeData)
}

// persistActiveNodes persists the active nodes list
func (r *Registry) persistActiveNodes() error {
	data, err := json.Marshal(r.activeNodes)
	if err != nil {
		return err
	}

	return r.db.Put(activeNodesKey, data)
}

// removeFromActiveList removes a node from the active list
func (r *Registry) removeFromActiveList(nodeID ids.NodeID) {
	for i, id := range r.activeNodes {
		if id == nodeID {
			r.activeNodes = append(r.activeNodes[:i], r.activeNodes[i+1:]...)
			return
		}
	}
}

// computeMerkleRoot computes a Merkle root from leaf hashes
func computeMerkleRoot(leaves [][]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}

	if len(leaves) == 1 {
		var root [32]byte
		copy(root[:], leaves[0])
		return root
	}

	// Pad to power of 2
	for len(leaves)&(len(leaves)-1) != 0 {
		leaves = append(leaves, leaves[len(leaves)-1])
	}

	// Build tree
	for len(leaves) > 1 {
		newLevel := make([][]byte, 0, len(leaves)/2)
		for i := 0; i < len(leaves); i += 2 {
			h := sha256.New()
			h.Write(leaves[i])
			h.Write(leaves[i+1])
			newLevel = append(newLevel, h.Sum(nil))
		}
		leaves = newLevel
	}

	var root [32]byte
	copy(root[:], leaves[0])
	return root
}
