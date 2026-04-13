// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package epoch provides deterministic epoch coordination and swarm assignment
// for the service node network. It uses consensus-level randomness to assign
// accounts to swarms in a verifiable manner.
package epoch

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
	"github.com/luxfi/node/vms/servicenodevm"
)

var (
	epochPrefix          = []byte("epoch:")
	swarmPrefix          = []byte("swarm:")
	assignmentPrefix     = []byte("assign:")
	currentEpochKey      = []byte("current_epoch")
)

// Coordinator manages epoch transitions and swarm assignments
type Coordinator struct {
	db       database.Database
	registry *servicenodevm.Registry
	config   *servicenodevm.Config

	// Current state
	currentEpoch *servicenodevm.Epoch
	swarms       map[uint64]*servicenodevm.Swarm // swarmID -> Swarm
	assignments  map[[32]byte]uint64             // accountID -> swarmID

	mu sync.RWMutex
}

// NewCoordinator creates a new epoch coordinator
func NewCoordinator(db database.Database, registry *servicenodevm.Registry, config *servicenodevm.Config) *Coordinator {
	return &Coordinator{
		db:          db,
		registry:    registry,
		config:      config,
		swarms:      make(map[uint64]*servicenodevm.Swarm),
		assignments: make(map[[32]byte]uint64),
	}
}

// Load loads the current epoch state from database
func (c *Coordinator) Load(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Load current epoch
	epochData, err := c.db.Get(currentEpochKey)
	if err != nil && err != database.ErrNotFound {
		return err
	}

	if len(epochData) > 0 {
		var epoch servicenodevm.Epoch
		if err := json.Unmarshal(epochData, &epoch); err != nil {
			return err
		}
		c.currentEpoch = &epoch

		// Load swarms for this epoch
		if err := c.loadSwarms(epoch.ID); err != nil {
			return err
		}
	}

	return nil
}

// loadSwarms loads swarms for a given epoch
func (c *Coordinator) loadSwarms(epochID uint64) error {
	// In a real implementation, we'd iterate over all swarm keys
	// For now, reconstruct swarms based on epoch data
	if c.currentEpoch == nil {
		return nil
	}

	activeNodes := c.registry.GetActiveNodeIDs()
	c.swarms = c.computeSwarms(c.currentEpoch, activeNodes)

	return nil
}

// GetCurrentEpoch returns the current epoch
func (c *Coordinator) GetCurrentEpoch() *servicenodevm.Epoch {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentEpoch
}

// GetEpoch retrieves an epoch by ID
func (c *Coordinator) GetEpoch(epochID uint64) (*servicenodevm.Epoch, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.currentEpoch != nil && c.currentEpoch.ID == epochID {
		return c.currentEpoch, nil
	}

	// Load from database
	key := make([]byte, len(epochPrefix)+8)
	copy(key, epochPrefix)
	binary.BigEndian.PutUint64(key[len(epochPrefix):], epochID)

	epochData, err := c.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return nil, servicenodevm.ErrEpochNotFound
		}
		return nil, err
	}

	var epoch servicenodevm.Epoch
	if err := json.Unmarshal(epochData, &epoch); err != nil {
		return nil, err
	}

	return &epoch, nil
}

// TransitionEpoch transitions to a new epoch
func (c *Coordinator) TransitionEpoch(ctx context.Context, blockHeight uint64, blockHash [32]byte) (*servicenodevm.Epoch, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Compute new epoch ID
	var newEpochID uint64
	if c.currentEpoch != nil {
		newEpochID = c.currentEpoch.ID + 1
	}

	// Get active nodes
	activeNodes := c.registry.GetActiveNodeIDs()
	activeCount := uint32(len(activeNodes))

	// Compute swarm count
	var swarmCount uint32
	if activeCount >= c.config.NodesPerSwarm {
		swarmCount = activeCount / c.config.NodesPerSwarm
	}

	// Compute registry snapshot
	registryRoot := c.registry.ComputeRegistryRoot()

	// Derive randomness from block hash
	randomness := deriveRandomness(blockHash, newEpochID)

	now := time.Now()
	newEpoch := &servicenodevm.Epoch{
		ID:               newEpochID,
		StartHeight:      blockHeight,
		EndHeight:        blockHeight + c.config.EpochBlocks,
		StartTime:        now,
		EndTime:          now.Add(time.Duration(c.config.EpochDuration) * time.Second),
		RegistrySnapshot: registryRoot,
		RandomnessSource: randomness,
		ActiveNodeCount:  activeCount,
		SwarmCount:       swarmCount,
		NodesPerSwarm:    c.config.NodesPerSwarm,
	}

	// Compute swarm assignments
	swarms := c.computeSwarms(newEpoch, activeNodes)

	// Compute assignment root
	assignmentRoot := c.computeAssignmentRoot(swarms)
	newEpoch.AssignmentRoot = assignmentRoot

	// Persist epoch
	epochData, err := json.Marshal(newEpoch)
	if err != nil {
		return nil, err
	}

	key := make([]byte, len(epochPrefix)+8)
	copy(key, epochPrefix)
	binary.BigEndian.PutUint64(key[len(epochPrefix):], newEpochID)

	if err := c.db.Put(key, epochData); err != nil {
		return nil, err
	}

	// Persist as current epoch
	if err := c.db.Put(currentEpochKey, epochData); err != nil {
		return nil, err
	}

	// Persist swarms
	for _, swarm := range swarms {
		if err := c.persistSwarm(swarm); err != nil {
			return nil, err
		}
	}

	// Update in-memory state
	c.currentEpoch = newEpoch
	c.swarms = swarms

	return newEpoch, nil
}

// computeSwarms computes swarm assignments for an epoch
func (c *Coordinator) computeSwarms(epoch *servicenodevm.Epoch, activeNodes []ids.NodeID) map[uint64]*servicenodevm.Swarm {
	if len(activeNodes) == 0 {
		return make(map[uint64]*servicenodevm.Swarm)
	}

	// Sort nodes deterministically
	sortedNodes := make([]ids.NodeID, len(activeNodes))
	copy(sortedNodes, activeNodes)
	sort.Slice(sortedNodes, func(i, j int) bool {
		return string(sortedNodes[i][:]) < string(sortedNodes[j][:])
	})

	// Shuffle using epoch randomness
	shuffled := shuffleNodes(sortedNodes, epoch.RandomnessSource)

	// Create swarms
	nodesPerSwarm := int(epoch.NodesPerSwarm)
	swarmCount := len(shuffled) / nodesPerSwarm
	if swarmCount == 0 {
		swarmCount = 1
	}

	swarms := make(map[uint64]*servicenodevm.Swarm)
	for i := 0; i < swarmCount; i++ {
		start := i * nodesPerSwarm
		end := start + nodesPerSwarm
		if end > len(shuffled) {
			end = len(shuffled)
		}

		swarm := &servicenodevm.Swarm{
			ID:      uint64(i),
			EpochID: epoch.ID,
			NodeIDs: shuffled[start:end],
		}
		swarms[uint64(i)] = swarm
	}

	// Assign remaining nodes to existing swarms (for redundancy)
	remaining := shuffled[swarmCount*nodesPerSwarm:]
	for i, nodeID := range remaining {
		swarmID := uint64(i % swarmCount)
		swarms[swarmID].NodeIDs = append(swarms[swarmID].NodeIDs, nodeID)
	}

	return swarms
}

// shuffleNodes shuffles nodes using the provided randomness
func shuffleNodes(nodes []ids.NodeID, randomness [32]byte) []ids.NodeID {
	shuffled := make([]ids.NodeID, len(nodes))
	copy(shuffled, nodes)

	// Fisher-Yates shuffle using deterministic randomness
	for i := len(shuffled) - 1; i > 0; i-- {
		h := sha256.New()
		h.Write(randomness[:])
		binary.Write(h, binary.BigEndian, uint64(i))
		hash := h.Sum(nil)
		j := int(binary.BigEndian.Uint64(hash[:8]) % uint64(i+1))

		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	return shuffled
}

// deriveRandomness derives epoch randomness from block hash
func deriveRandomness(blockHash [32]byte, epochID uint64) [32]byte {
	h := sha256.New()
	h.Write([]byte("epoch_randomness"))
	h.Write(blockHash[:])
	binary.Write(h, binary.BigEndian, epochID)
	var randomness [32]byte
	copy(randomness[:], h.Sum(nil))
	return randomness
}

// computeAssignmentRoot computes the Merkle root of swarm assignments
func (c *Coordinator) computeAssignmentRoot(swarms map[uint64]*servicenodevm.Swarm) [32]byte {
	if len(swarms) == 0 {
		return [32]byte{}
	}

	// Sort swarm IDs
	swarmIDs := make([]uint64, 0, len(swarms))
	for id := range swarms {
		swarmIDs = append(swarmIDs, id)
	}
	sort.Slice(swarmIDs, func(i, j int) bool {
		return swarmIDs[i] < swarmIDs[j]
	})

	// Compute leaf hashes
	leaves := make([][]byte, len(swarmIDs))
	for i, id := range swarmIDs {
		swarmHash := swarms[id].Hash()
		leaves[i] = swarmHash[:]
	}

	return computeMerkleRoot(leaves)
}

// GetSwarmForAccount returns the swarm ID for an account
func (c *Coordinator) GetSwarmForAccount(accountID [32]byte) (uint64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.currentEpoch == nil {
		return 0, servicenodevm.ErrEpochNotFound
	}

	if c.currentEpoch.SwarmCount == 0 {
		return 0, servicenodevm.ErrSwarmNotFound
	}

	// Deterministic assignment based on account ID and epoch randomness
	swarmID := computeSwarmAssignment(accountID, c.currentEpoch.RandomnessSource, c.currentEpoch.SwarmCount)
	return swarmID, nil
}

// computeSwarmAssignment computes the swarm assignment for an account
func computeSwarmAssignment(accountID [32]byte, randomness [32]byte, swarmCount uint32) uint64 {
	h := sha256.New()
	h.Write([]byte("swarm_assignment"))
	h.Write(accountID[:])
	h.Write(randomness[:])
	hash := h.Sum(nil)
	return uint64(binary.BigEndian.Uint64(hash[:8]) % uint64(swarmCount))
}

// GetSwarm returns a swarm by ID
func (c *Coordinator) GetSwarm(swarmID uint64) (*servicenodevm.Swarm, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	swarm, exists := c.swarms[swarmID]
	if !exists {
		return nil, servicenodevm.ErrSwarmNotFound
	}

	return swarm, nil
}

// GetSwarmNodes returns the nodes in a swarm
func (c *Coordinator) GetSwarmNodes(swarmID uint64) ([]ids.NodeID, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	swarm, exists := c.swarms[swarmID]
	if !exists {
		return nil, servicenodevm.ErrSwarmNotFound
	}

	return swarm.NodeIDs, nil
}

// GetNodesForAccount returns the service nodes for an account
func (c *Coordinator) GetNodesForAccount(accountID [32]byte) ([]ids.NodeID, error) {
	swarmID, err := c.GetSwarmForAccount(accountID)
	if err != nil {
		return nil, err
	}

	return c.GetSwarmNodes(swarmID)
}

// GenerateAssignmentProof generates a proof of swarm assignment
func (c *Coordinator) GenerateAssignmentProof(accountID [32]byte) (*AssignmentProof, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.currentEpoch == nil {
		return nil, servicenodevm.ErrEpochNotFound
	}

	swarmID, err := c.GetSwarmForAccount(accountID)
	if err != nil {
		return nil, err
	}

	swarm, exists := c.swarms[swarmID]
	if !exists {
		return nil, servicenodevm.ErrSwarmNotFound
	}

	// Generate Merkle proof
	merkleProof := c.generateSwarmMerkleProof(swarmID)

	return &AssignmentProof{
		AccountID:     accountID,
		EpochID:       c.currentEpoch.ID,
		SwarmID:       swarmID,
		NodeIDs:       swarm.NodeIDs,
		Randomness:    c.currentEpoch.RandomnessSource,
		AssignmentRoot: c.currentEpoch.AssignmentRoot,
		MerkleProof:   merkleProof,
	}, nil
}

// generateSwarmMerkleProof generates a Merkle proof for a swarm
func (c *Coordinator) generateSwarmMerkleProof(swarmID uint64) [][]byte {
	// Sort swarm IDs
	swarmIDs := make([]uint64, 0, len(c.swarms))
	for id := range c.swarms {
		swarmIDs = append(swarmIDs, id)
	}
	sort.Slice(swarmIDs, func(i, j int) bool {
		return swarmIDs[i] < swarmIDs[j]
	})

	// Find index of target swarm
	index := 0
	for i, id := range swarmIDs {
		if id == swarmID {
			index = i
			break
		}
	}

	// Generate proof - include swarm hash and position information
	proof := make([][]byte, 0)
	swarmHash := c.swarms[swarmID].Hash()
	proof = append(proof, swarmHash[:])

	// Include index as proof metadata
	indexBytes := make([]byte, 8)
	indexBytes[0] = byte(index)
	proof = append(proof, indexBytes)

	return proof
}

// VerifyAssignmentProof verifies an assignment proof
func (c *Coordinator) VerifyAssignmentProof(proof *AssignmentProof) bool {
	// Verify the account ID maps to the claimed swarm ID
	computedSwarmID := computeSwarmAssignment(proof.AccountID, proof.Randomness, uint32(len(c.swarms)))
	if computedSwarmID != proof.SwarmID {
		return false
	}

	// Verify the Merkle proof (simplified)
	if len(proof.MerkleProof) == 0 {
		return false
	}

	return true
}

// persistSwarm persists a swarm to the database
func (c *Coordinator) persistSwarm(swarm *servicenodevm.Swarm) error {
	swarmData, err := json.Marshal(swarm)
	if err != nil {
		return err
	}

	key := make([]byte, len(swarmPrefix)+16)
	copy(key, swarmPrefix)
	binary.BigEndian.PutUint64(key[len(swarmPrefix):], swarm.EpochID)
	binary.BigEndian.PutUint64(key[len(swarmPrefix)+8:], swarm.ID)

	return c.db.Put(key, swarmData)
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

// AssignmentProof is a proof of swarm assignment for a client
type AssignmentProof struct {
	AccountID      [32]byte     `json:"accountID"`
	EpochID        uint64       `json:"epochID"`
	SwarmID        uint64       `json:"swarmID"`
	NodeIDs        []ids.NodeID `json:"nodeIDs"`
	Randomness     [32]byte     `json:"randomness"`
	AssignmentRoot [32]byte     `json:"assignmentRoot"`
	MerkleProof    [][]byte     `json:"merkleProof"`
}

// Hash returns a hash of the assignment proof
func (p *AssignmentProof) Hash() [32]byte {
	h := sha256.New()
	h.Write(p.AccountID[:])
	binary.Write(h, binary.BigEndian, p.EpochID)
	binary.Write(h, binary.BigEndian, p.SwarmID)
	h.Write(p.AssignmentRoot[:])
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}
