// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package storage implements provable storage availability commitments
// for service nodes. It provides per-epoch store-root commitments,
// random audits, and integration with the DA infrastructure.
package storage

import (
	"bytes"
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
	commitmentPrefix = []byte("commit:")
	auditPrefix      = []byte("audit:")
	proofPrefix      = []byte("proof:")
)

// Manager handles storage availability tracking and verification
type Manager struct {
	db       database.Database
	config   *servicenodevm.Config

	// Commitments by epoch and node
	commitments map[uint64]map[ids.NodeID]*servicenodevm.StorageCommitment

	// Pending audits
	audits map[ids.ID]*StorageAudit

	mu sync.RWMutex
}

// NewManager creates a new storage manager
func NewManager(db database.Database, config *servicenodevm.Config) *Manager {
	return &Manager{
		db:          db,
		config:      config,
		commitments: make(map[uint64]map[ids.NodeID]*servicenodevm.StorageCommitment),
		audits:      make(map[ids.ID]*StorageAudit),
	}
}

// Load loads storage state from database
func (m *Manager) Load(ctx context.Context) error {
	// In a full implementation, load recent commitments and pending audits
	return nil
}

// SubmitCommitment records a storage commitment from a node
func (m *Manager) SubmitCommitment(ctx context.Context, commit *servicenodevm.StorageCommitment) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get or create epoch map
	epochCommits, exists := m.commitments[commit.EpochID]
	if !exists {
		epochCommits = make(map[ids.NodeID]*servicenodevm.StorageCommitment)
		m.commitments[commit.EpochID] = epochCommits
	}

	// Store commitment
	epochCommits[commit.NodeID] = commit

	// Persist
	return m.persistCommitment(commit)
}

// GetCommitment retrieves a commitment for a node and epoch
func (m *Manager) GetCommitment(epochID uint64, nodeID ids.NodeID) (*servicenodevm.StorageCommitment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	epochCommits, exists := m.commitments[epochID]
	if !exists {
		return nil, servicenodevm.ErrEpochNotFound
	}

	commit, exists := epochCommits[nodeID]
	if !exists {
		return nil, servicenodevm.ErrNodeNotFound
	}

	return commit, nil
}

// GetEpochCommitments retrieves all commitments for an epoch
func (m *Manager) GetEpochCommitments(epochID uint64) []*servicenodevm.StorageCommitment {
	m.mu.RLock()
	defer m.mu.RUnlock()

	epochCommits, exists := m.commitments[epochID]
	if !exists {
		return nil
	}

	commits := make([]*servicenodevm.StorageCommitment, 0, len(epochCommits))
	for _, commit := range epochCommits {
		commits = append(commits, commit)
	}

	return commits
}

// ComputeEpochRoot computes the Merkle root of all commitments for an epoch
func (m *Manager) ComputeEpochRoot(epochID uint64) [32]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	epochCommits, exists := m.commitments[epochID]
	if !exists {
		return [32]byte{}
	}

	// Collect and sort node IDs for deterministic ordering
	nodeIDs := make([]ids.NodeID, 0, len(epochCommits))
	for nodeID := range epochCommits {
		nodeIDs = append(nodeIDs, nodeID)
	}
	// Sort by node ID bytes for determinism
	for i := 0; i < len(nodeIDs)-1; i++ {
		for j := i + 1; j < len(nodeIDs); j++ {
			if bytes.Compare(nodeIDs[i][:], nodeIDs[j][:]) > 0 {
				nodeIDs[i], nodeIDs[j] = nodeIDs[j], nodeIDs[i]
			}
		}
	}

	// Collect commitment hashes in sorted order
	hashes := make([][]byte, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		commit := epochCommits[nodeID]
		commitHash := commit.Hash()
		hashes = append(hashes, commitHash[:])
	}

	return computeMerkleRoot(hashes)
}

// CreateAudit creates a random storage audit for a node
func (m *Manager) CreateAudit(ctx context.Context, epochID uint64, nodeID ids.NodeID, seed [32]byte) (*StorageAudit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get the node's commitment
	epochCommits, exists := m.commitments[epochID]
	if !exists {
		return nil, servicenodevm.ErrEpochNotFound
	}

	commit, exists := epochCommits[nodeID]
	if !exists {
		return nil, servicenodevm.ErrNodeNotFound
	}

	// Generate random challenge
	var challenge [32]byte
	h := sha256.New()
	h.Write(seed[:])
	h.Write(nodeID[:])
	binary.Write(h, binary.BigEndian, epochID)
	binary.Write(h, binary.BigEndian, time.Now().UnixNano())
	copy(challenge[:], h.Sum(nil))

	// Generate audit ID
	var auditNonce [16]byte
	rand.Read(auditNonce[:])

	h = sha256.New()
	h.Write(challenge[:])
	h.Write(auditNonce[:])
	auditID := ids.ID(h.Sum(nil))

	// Select random chunks to audit (based on message count)
	chunkIndices := selectRandomChunks(commit.MessageCount, 16, challenge)

	audit := &StorageAudit{
		ID:           auditID,
		EpochID:      epochID,
		NodeID:       nodeID,
		Challenge:    challenge,
		ChunkIndices: chunkIndices,
		StoreRoot:    commit.StoreRoot,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(time.Duration(m.config.ChallengeTTL) * time.Second),
		Status:       AuditPending,
	}

	m.audits[auditID] = audit

	// Persist
	if err := m.persistAudit(audit); err != nil {
		return nil, err
	}

	return audit, nil
}

// selectRandomChunks selects random chunk indices
func selectRandomChunks(totalChunks uint64, count int, seed [32]byte) []uint64 {
	if totalChunks == 0 {
		return nil
	}

	if uint64(count) > totalChunks {
		count = int(totalChunks)
	}

	indices := make([]uint64, count)
	for i := 0; i < count; i++ {
		h := sha256.New()
		h.Write(seed[:])
		binary.Write(h, binary.BigEndian, uint64(i))
		hash := h.Sum(nil)
		indices[i] = binary.BigEndian.Uint64(hash[:8]) % totalChunks
	}

	return indices
}

// SubmitAuditProof submits a proof in response to an audit
func (m *Manager) SubmitAuditProof(ctx context.Context, proof *AuditProof) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	audit, exists := m.audits[proof.AuditID]
	if !exists {
		return servicenodevm.ErrChallengeNotFound
	}

	if audit.Status != AuditPending {
		return nil // Already processed
	}

	if time.Now().After(audit.ExpiresAt) {
		audit.Status = AuditExpired
		return servicenodevm.ErrChallengeExpired
	}

	// Verify the proof
	if !m.verifyAuditProof(audit, proof) {
		audit.Status = AuditFailed
		audit.FailureReason = "invalid proof"
		return m.persistAudit(audit)
	}

	audit.Status = AuditPassed
	audit.ResponseProof = proof.ProofData
	audit.RespondedAt = time.Now()

	return m.persistAudit(audit)
}

// verifyAuditProof verifies an audit proof
func (m *Manager) verifyAuditProof(audit *StorageAudit, proof *AuditProof) bool {
	// Verify node matches
	if proof.NodeID != audit.NodeID {
		return false
	}

	// Verify chunk proofs cover requested indices
	if len(proof.ChunkProofs) != len(audit.ChunkIndices) {
		return false
	}

	// Verify each chunk proof
	for i, chunkProof := range proof.ChunkProofs {
		expectedIndex := audit.ChunkIndices[i]
		if chunkProof.Index != expectedIndex {
			return false
		}

		// Verify Merkle proof against store root
		if !verifyMerkleProof(chunkProof.Data, chunkProof.MerkleProof, audit.StoreRoot, chunkProof.Index) {
			return false
		}
	}

	return true
}

// verifyMerkleProof verifies a Merkle proof
func verifyMerkleProof(data []byte, proof [][]byte, root [32]byte, index uint64) bool {
	if len(proof) == 0 {
		return false
	}

	// Compute leaf hash
	hash := sha256.Sum256(data)
	currentHash := hash[:]

	// Walk up the tree
	for i, sibling := range proof {
		h := sha256.New()
		if (index>>uint(i))&1 == 0 {
			h.Write(currentHash)
			h.Write(sibling)
		} else {
			h.Write(sibling)
			h.Write(currentHash)
		}
		currentHash = h.Sum(nil)
	}

	// Compare with root
	var computedRoot [32]byte
	copy(computedRoot[:], currentHash)
	return computedRoot == root
}

// GetAudit retrieves an audit by ID
func (m *Manager) GetAudit(auditID ids.ID) (*StorageAudit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	audit, exists := m.audits[auditID]
	if !exists {
		return nil, servicenodevm.ErrChallengeNotFound
	}

	return audit, nil
}

// GetPendingAudits returns all pending audits for a node
func (m *Manager) GetPendingAudits(nodeID ids.NodeID) []*StorageAudit {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var pending []*StorageAudit
	for _, audit := range m.audits {
		if audit.NodeID == nodeID && audit.Status == AuditPending && !time.Now().After(audit.ExpiresAt) {
			pending = append(pending, audit)
		}
	}

	return pending
}

// ProcessExpiredAudits marks expired audits as failed
func (m *Manager) ProcessExpiredAudits(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	processed := 0

	for _, audit := range m.audits {
		if audit.Status == AuditPending && now.After(audit.ExpiresAt) {
			audit.Status = AuditExpired
			audit.FailureReason = "timeout"
			m.persistAudit(audit)
			processed++
		}
	}

	return processed, nil
}

// persistCommitment persists a storage commitment
func (m *Manager) persistCommitment(commit *servicenodevm.StorageCommitment) error {
	data, err := json.Marshal(commit)
	if err != nil {
		return err
	}

	key := make([]byte, len(commitmentPrefix)+8+len(commit.NodeID))
	copy(key, commitmentPrefix)
	binary.BigEndian.PutUint64(key[len(commitmentPrefix):], commit.EpochID)
	copy(key[len(commitmentPrefix)+8:], commit.NodeID[:])

	return m.db.Put(key, data)
}

// persistAudit persists a storage audit
func (m *Manager) persistAudit(audit *StorageAudit) error {
	data, err := json.Marshal(audit)
	if err != nil {
		return err
	}

	key := append(auditPrefix, audit.ID[:]...)
	return m.db.Put(key, data)
}

// computeMerkleRoot computes a Merkle root from hashes
func computeMerkleRoot(hashes [][]byte) [32]byte {
	if len(hashes) == 0 {
		return [32]byte{}
	}

	if len(hashes) == 1 {
		var root [32]byte
		copy(root[:], hashes[0])
		return root
	}

	// Pad to power of 2
	for len(hashes)&(len(hashes)-1) != 0 {
		hashes = append(hashes, hashes[len(hashes)-1])
	}

	// Build tree
	for len(hashes) > 1 {
		newLevel := make([][]byte, 0, len(hashes)/2)
		for i := 0; i < len(hashes); i += 2 {
			h := sha256.New()
			h.Write(hashes[i])
			h.Write(hashes[i+1])
			newLevel = append(newLevel, h.Sum(nil))
		}
		hashes = newLevel
	}

	var root [32]byte
	copy(root[:], hashes[0])
	return root
}

// AuditStatus represents the status of a storage audit
type AuditStatus string

const (
	AuditPending AuditStatus = "pending"
	AuditPassed  AuditStatus = "passed"
	AuditFailed  AuditStatus = "failed"
	AuditExpired AuditStatus = "expired"
)

// StorageAudit represents a random audit of stored data
type StorageAudit struct {
	ID            ids.ID      `json:"id"`
	EpochID       uint64      `json:"epochID"`
	NodeID        ids.NodeID  `json:"nodeID"`
	Challenge     [32]byte    `json:"challenge"`
	ChunkIndices  []uint64    `json:"chunkIndices"`
	StoreRoot     [32]byte    `json:"storeRoot"`
	CreatedAt     time.Time   `json:"createdAt"`
	ExpiresAt     time.Time   `json:"expiresAt"`
	Status        AuditStatus `json:"status"`
	ResponseProof []byte      `json:"responseProof,omitempty"`
	RespondedAt   time.Time   `json:"respondedAt,omitempty"`
	FailureReason string      `json:"failureReason,omitempty"`
}

// Hash returns the hash of the audit
func (a *StorageAudit) Hash() [32]byte {
	h := sha256.New()
	h.Write(a.ID[:])
	binary.Write(h, binary.BigEndian, a.EpochID)
	h.Write(a.NodeID[:])
	h.Write(a.Challenge[:])
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// AuditProof represents a proof in response to a storage audit
type AuditProof struct {
	AuditID     ids.ID       `json:"auditID"`
	NodeID      ids.NodeID   `json:"nodeID"`
	ChunkProofs []ChunkProof `json:"chunkProofs"`
	Signature   []byte       `json:"signature"`
	Timestamp   time.Time    `json:"timestamp"`
	ProofData   []byte       `json:"proofData"`
}

// ChunkProof represents a proof for a single chunk
type ChunkProof struct {
	Index       uint64   `json:"index"`
	Data        []byte   `json:"data"`
	MerkleProof [][]byte `json:"merkleProof"`
}
