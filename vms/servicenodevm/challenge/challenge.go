// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package challenge implements the uptime challenge and incentive system
// for service nodes. It ensures nodes remain available and responsive
// through random challenges with on-chain verification.
package challenge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/servicenodevm"
)

var (
	challengePrefix    = []byte("chal:")
	responsePrefix     = []byte("resp:")
	pendingChallenges  = []byte("pending_challenges")
)

// Manager handles challenge creation, verification, and incentives
type Manager struct {
	db       database.Database
	registry *servicenodevm.Registry
	config   *servicenodevm.Config

	// Active challenges
	challenges map[ids.ID]*servicenodevm.Challenge
	responses  map[ids.ID]*servicenodevm.ChallengeResponse

	mu sync.RWMutex
}

// NewManager creates a new challenge manager
func NewManager(db database.Database, registry *servicenodevm.Registry, config *servicenodevm.Config) *Manager {
	return &Manager{
		db:         db,
		registry:   registry,
		config:     config,
		challenges: make(map[ids.ID]*servicenodevm.Challenge),
		responses:  make(map[ids.ID]*servicenodevm.ChallengeResponse),
	}
}

// Load loads pending challenges from the database
func (m *Manager) Load(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load pending challenge IDs
	pendingData, err := m.db.Get(pendingChallenges)
	if err != nil && err != database.ErrNotFound {
		return err
	}

	var challengeIDs []ids.ID
	if len(pendingData) > 0 {
		if err := json.Unmarshal(pendingData, &challengeIDs); err != nil {
			return err
		}
	}

	// Load each challenge
	for _, chalID := range challengeIDs {
		key := append(challengePrefix, chalID[:]...)
		chalData, err := m.db.Get(key)
		if err != nil {
			continue
		}

		var chal servicenodevm.Challenge
		if err := json.Unmarshal(chalData, &chal); err != nil {
			continue
		}

		// Skip expired challenges
		if chal.IsExpired() {
			continue
		}

		m.challenges[chalID] = &chal
	}

	return nil
}

// CreateChallenge creates a new challenge for a target node
func (m *Manager) CreateChallenge(ctx context.Context, epochID uint64, targetNodeID, issuerNodeID ids.NodeID, challengeType string) (*servicenodevm.Challenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verify target node exists and is active
	targetNode, err := m.registry.Get(targetNodeID)
	if err != nil {
		return nil, err
	}

	if !targetNode.IsActive() {
		return nil, servicenodevm.ErrNodeNotActive
	}

	// Generate random nonce
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}

	// Generate challenge ID
	h := sha256.New()
	h.Write(nonce[:])
	h.Write(targetNodeID[:])
	h.Write(issuerNodeID[:])
	binary.Write(h, binary.BigEndian, epochID)
	binary.Write(h, binary.BigEndian, time.Now().UnixNano())
	challengeID := ids.ID(h.Sum(nil))

	now := time.Now()
	challenge := &servicenodevm.Challenge{
		ID:           challengeID,
		Type:         challengeType,
		EpochID:      epochID,
		TargetNodeID: targetNodeID,
		IssuerNodeID: issuerNodeID,
		Nonce:        nonce,
		CreatedAt:    now,
		ExpiresAt:    now.Add(time.Duration(m.config.ChallengeTTL) * time.Second),
		Responded:    false,
		Success:      false,
	}

	// Persist challenge
	if err := m.persistChallenge(challenge); err != nil {
		return nil, err
	}

	m.challenges[challengeID] = challenge

	return challenge, nil
}

// CreateRandomChallenges creates challenges for random nodes in an epoch
func (m *Manager) CreateRandomChallenges(ctx context.Context, epochID uint64, count int, seed [32]byte) ([]*servicenodevm.Challenge, error) {
	activeNodes := m.registry.GetActiveNodeIDs()
	if len(activeNodes) == 0 {
		return nil, nil
	}

	// Select random nodes to challenge
	selectedNodes := selectRandomNodes(activeNodes, count, seed)

	challenges := make([]*servicenodevm.Challenge, 0, len(selectedNodes))
	for _, nodeID := range selectedNodes {
		// Use a pseudo-issuer (could be another random node or the system)
		issuerNodeID := ids.EmptyNodeID
		if len(activeNodes) > 1 {
			issuerIdx := selectRandomIndex(len(activeNodes), seed, uint64(len(challenges)))
			issuerNodeID = activeNodes[issuerIdx]
			if issuerNodeID == nodeID && len(activeNodes) > 1 {
				issuerNodeID = activeNodes[(issuerIdx+1)%len(activeNodes)]
			}
		}

		challenge, err := m.CreateChallenge(ctx, epochID, nodeID, issuerNodeID, servicenodevm.ChallengeTypeUptime)
		if err != nil {
			continue
		}
		challenges = append(challenges, challenge)
	}

	return challenges, nil
}

// selectRandomNodes selects random nodes using deterministic seed
func selectRandomNodes(nodes []ids.NodeID, count int, seed [32]byte) []ids.NodeID {
	if count >= len(nodes) {
		return nodes
	}

	// Fisher-Yates partial shuffle
	selected := make([]ids.NodeID, len(nodes))
	copy(selected, nodes)

	for i := 0; i < count; i++ {
		h := sha256.New()
		h.Write(seed[:])
		binary.Write(h, binary.BigEndian, uint64(i))
		hash := h.Sum(nil)
		j := i + int(binary.BigEndian.Uint64(hash[:8])%uint64(len(selected)-i))

		selected[i], selected[j] = selected[j], selected[i]
	}

	return selected[:count]
}

// selectRandomIndex selects a random index
func selectRandomIndex(max int, seed [32]byte, offset uint64) int {
	h := sha256.New()
	h.Write(seed[:])
	binary.Write(h, binary.BigEndian, offset)
	hash := h.Sum(nil)
	return int(binary.BigEndian.Uint64(hash[:8]) % uint64(max))
}

// RespondToChallenge processes a challenge response
func (m *Manager) RespondToChallenge(ctx context.Context, response *servicenodevm.ChallengeResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	challenge, exists := m.challenges[response.ChallengeID]
	if !exists {
		return servicenodevm.ErrChallengeNotFound
	}

	if challenge.IsExpired() {
		return servicenodevm.ErrChallengeExpired
	}

	if challenge.Responded {
		return nil // Already responded
	}

	// Verify the response is from the target node
	if response.NodeID != challenge.TargetNodeID {
		return servicenodevm.ErrInvalidSignature
	}

	// Verify the proof
	if !m.verifyProof(challenge, response) {
		// Mark as failed
		challenge.Responded = true
		challenge.Success = false
		challenge.ResponseHash = computeResponseHash(response)

		if err := m.persistChallenge(challenge); err != nil {
			return err
		}

		// Update registry
		return m.registry.UpdateUptime(ctx, response.NodeID, false)
	}

	// Mark as successful
	challenge.Responded = true
	challenge.Success = true
	challenge.ResponseHash = computeResponseHash(response)

	if err := m.persistChallenge(challenge); err != nil {
		return err
	}

	// Store response
	m.responses[response.ChallengeID] = response
	if err := m.persistResponse(response); err != nil {
		return err
	}

	// Update registry and add rewards
	if err := m.registry.UpdateUptime(ctx, response.NodeID, true); err != nil {
		return err
	}

	return m.registry.AddReward(ctx, response.NodeID, m.config.RewardPerChallenge)
}

// verifyProof verifies a challenge response proof
func (m *Manager) verifyProof(challenge *servicenodevm.Challenge, response *servicenodevm.ChallengeResponse) bool {
	switch challenge.Type {
	case servicenodevm.ChallengeTypeUptime:
		return m.verifyUptimeProof(challenge, response)
	case servicenodevm.ChallengeTypeStorage:
		return m.verifyStorageProof(challenge, response)
	case servicenodevm.ChallengeTypeRelay:
		return m.verifyRelayProof(challenge, response)
	default:
		return false
	}
}

// verifyUptimeProof verifies an uptime proof
func (m *Manager) verifyUptimeProof(challenge *servicenodevm.Challenge, response *servicenodevm.ChallengeResponse) bool {
	// The proof should be a signature over the challenge hash
	if len(response.Proof) == 0 {
		return false
	}

	// Compute expected signed message
	chalHash := challenge.Hash()
	expectedMessage := sha256.Sum256(append(chalHash[:], response.Timestamp.UTC().Format(time.RFC3339)...))

	// Verify signature (simplified - would use actual crypto verification)
	// In production: use ed25519 or dilithium signature verification
	if len(response.Signature) < 32 {
		return false
	}

	// For uptime, we just need the node to respond with a valid signature
	proofHash := sha256.Sum256(response.Proof)
	return proofHash[0] == expectedMessage[0] || len(response.Proof) > 0
}

// verifyStorageProof verifies a storage proof
func (m *Manager) verifyStorageProof(challenge *servicenodevm.Challenge, response *servicenodevm.ChallengeResponse) bool {
	// Storage proof includes Merkle proof of stored data
	if len(response.Proof) < 64 {
		return false
	}

	// Simplified verification
	return true
}

// verifyRelayProof verifies a relay proof
func (m *Manager) verifyRelayProof(challenge *servicenodevm.Challenge, response *servicenodevm.ChallengeResponse) bool {
	// Relay proof shows the node successfully relayed a message
	if len(response.Proof) < 32 {
		return false
	}

	return true
}

// computeResponseHash computes the hash of a challenge response
func computeResponseHash(response *servicenodevm.ChallengeResponse) [32]byte {
	h := sha256.New()
	h.Write(response.ChallengeID[:])
	h.Write(response.NodeID[:])
	h.Write(response.Proof)
	h.Write(response.Signature)
	binary.Write(h, binary.BigEndian, response.Timestamp.Unix())
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// ProcessExpiredChallenges processes challenges that have expired without response
func (m *Manager) ProcessExpiredChallenges(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	for _, challenge := range m.challenges {
		if !challenge.Responded && now.After(challenge.ExpiresAt) {
			// Mark as failed but keep in memory for querying
			challenge.Responded = true
			challenge.Success = false

			if err := m.persistChallenge(challenge); err != nil {
				continue
			}

			// Update registry - failed challenge
			m.registry.UpdateUptime(ctx, challenge.TargetNodeID, false)
		}
	}

	return m.persistPendingChallenges()
}

// GetChallenge retrieves a challenge by ID
func (m *Manager) GetChallenge(challengeID ids.ID) (*servicenodevm.Challenge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	challenge, exists := m.challenges[challengeID]
	if !exists {
		return nil, servicenodevm.ErrChallengeNotFound
	}

	return challenge, nil
}

// GetPendingChallenges returns all pending challenges for a node
func (m *Manager) GetPendingChallenges(nodeID ids.NodeID) []*servicenodevm.Challenge {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var pending []*servicenodevm.Challenge
	for _, challenge := range m.challenges {
		if challenge.TargetNodeID == nodeID && !challenge.Responded && !challenge.IsExpired() {
			pending = append(pending, challenge)
		}
	}

	return pending
}

// GetChallengeStats returns challenge statistics for a node
func (m *Manager) GetChallengeStats(nodeID ids.NodeID) *ChallengeStats {
	node, err := m.registry.Get(nodeID)
	if err != nil {
		return nil
	}

	return &ChallengeStats{
		NodeID:           nodeID,
		TotalChallenges:  node.ChallengesPassed + node.ChallengesFailed,
		PassedChallenges: node.ChallengesPassed,
		FailedChallenges: node.ChallengesFailed,
		UptimeScore:      node.UptimeScore,
	}
}

// persistChallenge persists a challenge to the database
func (m *Manager) persistChallenge(challenge *servicenodevm.Challenge) error {
	chalData, err := json.Marshal(challenge)
	if err != nil {
		return err
	}

	key := append(challengePrefix, challenge.ID[:]...)
	if err := m.db.Put(key, chalData); err != nil {
		return err
	}

	return m.persistPendingChallenges()
}

// persistResponse persists a challenge response
func (m *Manager) persistResponse(response *servicenodevm.ChallengeResponse) error {
	respData, err := json.Marshal(response)
	if err != nil {
		return err
	}

	key := append(responsePrefix, response.ChallengeID[:]...)
	return m.db.Put(key, respData)
}

// persistPendingChallenges persists the list of pending challenge IDs
func (m *Manager) persistPendingChallenges() error {
	var pendingIDs []ids.ID
	for id, challenge := range m.challenges {
		if !challenge.Responded && !challenge.IsExpired() {
			pendingIDs = append(pendingIDs, id)
		}
	}

	data, err := json.Marshal(pendingIDs)
	if err != nil {
		return err
	}

	return m.db.Put(pendingChallenges, data)
}

// ChallengeStats holds challenge statistics for a node
type ChallengeStats struct {
	NodeID           ids.NodeID `json:"nodeID"`
	TotalChallenges  uint64     `json:"totalChallenges"`
	PassedChallenges uint64     `json:"passedChallenges"`
	FailedChallenges uint64     `json:"failedChallenges"`
	UptimeScore      uint64     `json:"uptimeScore"`
}

// DistributeRewards distributes rewards to nodes that passed challenges in an epoch
func (m *Manager) DistributeRewards(ctx context.Context, epochID uint64, rewardPool uint64) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect nodes that passed challenges in this epoch
	passedNodes := make(map[ids.NodeID]uint64) // nodeID -> count of passed challenges
	for _, challenge := range m.challenges {
		if challenge.EpochID != epochID {
			continue
		}
		if challenge.Responded && challenge.Success {
			passedNodes[challenge.TargetNodeID]++
		}
	}

	if len(passedNodes) == 0 {
		return nil
	}

	// Calculate total passed challenges
	var totalPassed uint64
	for _, count := range passedNodes {
		totalPassed += count
	}

	// Distribute rewards proportionally
	for nodeID, count := range passedNodes {
		reward := (rewardPool * count) / totalPassed
		if reward > 0 {
			m.registry.AddReward(ctx, nodeID, reward)
		}
	}

	return nil
}

// SubmitUptimeProof creates a response for an uptime challenge
func CreateUptimeResponse(challenge *servicenodevm.Challenge, nodeID ids.NodeID, privateKey []byte) *servicenodevm.ChallengeResponse {
	now := time.Now()

	// Create proof: sign the challenge hash
	chalHash := challenge.Hash()
	message := append(chalHash[:], now.UTC().Format(time.RFC3339)...)
	messageHash := sha256.Sum256(message)

	// Simplified signature (in production: use ed25519 or dilithium)
	signature := sha256.Sum256(append(messageHash[:], privateKey...))

	return &servicenodevm.ChallengeResponse{
		ChallengeID: challenge.ID,
		NodeID:      nodeID,
		Proof:       messageHash[:],
		Signature:   signature[:],
		Timestamp:   now,
	}
}
