// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package servicenodevm implements the Service Node VM for Session-style
// private messaging infrastructure. It manages service node registration,
// stake/collateral, swarm assignments, and uptime challenges.
package servicenodevm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/luxfi/ids"
)

// Service node states
const (
	StateRegistered = "registered"
	StateActive     = "active"
	StateJailed     = "jailed"
	StateExiting    = "exiting"
	StateExited     = "exited"
)

// Challenge response types
const (
	ChallengeTypeUptime  = "uptime"
	ChallengeTypeStorage = "storage"
	ChallengeTypeRelay   = "relay"
)

// Errors
var (
	ErrNodeNotFound        = errors.New("service node not found")
	ErrNodeAlreadyExists   = errors.New("service node already exists")
	ErrInsufficientStake   = errors.New("insufficient stake")
	ErrNodeJailed          = errors.New("service node is jailed")
	ErrNodeNotActive       = errors.New("service node is not active")
	ErrInvalidSignature    = errors.New("invalid signature")
	ErrEpochNotFound       = errors.New("epoch not found")
	ErrSwarmNotFound       = errors.New("swarm not found")
	ErrChallengeNotFound   = errors.New("challenge not found")
	ErrChallengeFailed     = errors.New("challenge response failed")
	ErrChallengeExpired    = errors.New("challenge has expired")
	ErrMessageNotFound     = errors.New("message not found")
	ErrMessageExpired      = errors.New("message has expired")
	ErrQuotaExceeded       = errors.New("storage quota exceeded")
	ErrInvalidProof        = errors.New("invalid proof")
	ErrInvalidEpoch        = errors.New("invalid epoch")
)

// ServiceNode represents a registered service node
type ServiceNode struct {
	// Identity
	ID        ids.ID     `json:"id"`
	NodeID    ids.NodeID `json:"nodeID"`
	PublicKey []byte     `json:"publicKey"`

	// Contact info (hashed for privacy)
	EndpointHash [32]byte `json:"endpointHash"`

	// Stake
	StakeAmount  uint64 `json:"stakeAmount"`
	StakeLockEnd uint64 `json:"stakeLockEnd"` // Unix timestamp

	// State
	State       string    `json:"state"`
	JailReason  string    `json:"jailReason,omitempty"`
	JailedAt    time.Time `json:"jailedAt,omitempty"`
	JailRelease uint64    `json:"jailRelease,omitempty"` // Unix timestamp

	// Registration
	RegisteredAt   time.Time `json:"registeredAt"`
	LastActiveAt   time.Time `json:"lastActiveAt"`
	ActivationTime uint64    `json:"activationTime"` // Unix timestamp when became active

	// Performance metrics
	UptimeScore     uint64 `json:"uptimeScore"`     // 0-10000 (basis points)
	ChallengesPassed uint64 `json:"challengesPassed"`
	ChallengesFailed uint64 `json:"challengesFailed"`
	MessagesRelayed  uint64 `json:"messagesRelayed"`

	// Rewards
	PendingRewards uint64 `json:"pendingRewards"`
	TotalRewards   uint64 `json:"totalRewards"`
}

// Hash returns a deterministic hash of the service node
func (sn *ServiceNode) Hash() [32]byte {
	h := sha256.New()
	h.Write(sn.ID[:])
	h.Write(sn.NodeID[:])
	h.Write(sn.PublicKey)
	h.Write(sn.EndpointHash[:])
	binary.Write(h, binary.BigEndian, sn.StakeAmount)
	binary.Write(h, binary.BigEndian, sn.StakeLockEnd)
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// IsActive returns whether the node is in active state
func (sn *ServiceNode) IsActive() bool {
	return sn.State == StateActive
}

// CanReceiveMessages returns whether the node can receive messages
func (sn *ServiceNode) CanReceiveMessages() bool {
	return sn.State == StateActive || sn.State == StateExiting
}

// Epoch represents a time period for swarm assignments
type Epoch struct {
	ID                 uint64    `json:"id"`
	StartHeight        uint64    `json:"startHeight"`
	EndHeight          uint64    `json:"endHeight"`
	StartTime          time.Time `json:"startTime"`
	EndTime            time.Time `json:"endTime"`
	RegistrySnapshot   [32]byte  `json:"registrySnapshot"`  // Root of service node registry at epoch start
	AssignmentRoot     [32]byte  `json:"assignmentRoot"`    // Root of swarm assignments
	RandomnessSource   [32]byte  `json:"randomnessSource"`  // From block hash or VRF
	ActiveNodeCount    uint32    `json:"activeNodeCount"`
	SwarmCount         uint32    `json:"swarmCount"`
	NodesPerSwarm      uint32    `json:"nodesPerSwarm"`
}

// Hash returns a deterministic hash of the epoch
func (e *Epoch) Hash() [32]byte {
	h := sha256.New()
	binary.Write(h, binary.BigEndian, e.ID)
	binary.Write(h, binary.BigEndian, e.StartHeight)
	binary.Write(h, binary.BigEndian, e.EndHeight)
	h.Write(e.RegistrySnapshot[:])
	h.Write(e.AssignmentRoot[:])
	h.Write(e.RandomnessSource[:])
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// Swarm represents a group of service nodes serving a set of accounts
type Swarm struct {
	ID         uint64       `json:"id"`
	EpochID    uint64       `json:"epochID"`
	NodeIDs    []ids.NodeID `json:"nodeIDs"`
	AccountIDs [][32]byte   `json:"accountIDs"` // Account ID hashes assigned to this swarm
}

// Hash returns a deterministic hash of the swarm
func (s *Swarm) Hash() [32]byte {
	h := sha256.New()
	binary.Write(h, binary.BigEndian, s.ID)
	binary.Write(h, binary.BigEndian, s.EpochID)
	for _, nodeID := range s.NodeIDs {
		h.Write(nodeID[:])
	}
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// ContainsNode returns whether the swarm contains the given node
func (s *Swarm) ContainsNode(nodeID ids.NodeID) bool {
	for _, nid := range s.NodeIDs {
		if nid == nodeID {
			return true
		}
	}
	return false
}

// Challenge represents an uptime/storage challenge
type Challenge struct {
	ID           ids.ID       `json:"id"`
	Type         string       `json:"type"`
	EpochID      uint64       `json:"epochID"`
	TargetNodeID ids.NodeID   `json:"targetNodeID"`
	IssuerNodeID ids.NodeID   `json:"issuerNodeID"`
	Nonce        [32]byte     `json:"nonce"`
	CreatedAt    time.Time    `json:"createdAt"`
	ExpiresAt    time.Time    `json:"expiresAt"`
	Responded    bool         `json:"responded"`
	ResponseHash [32]byte     `json:"responseHash,omitempty"`
	Success      bool         `json:"success"`
}

// Hash returns a deterministic hash of the challenge
func (c *Challenge) Hash() [32]byte {
	h := sha256.New()
	h.Write(c.ID[:])
	h.Write([]byte(c.Type))
	binary.Write(h, binary.BigEndian, c.EpochID)
	h.Write(c.TargetNodeID[:])
	h.Write(c.Nonce[:])
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// IsExpired returns whether the challenge has expired
func (c *Challenge) IsExpired() bool {
	return time.Now().After(c.ExpiresAt)
}

// ChallengeResponse represents a response to a challenge
type ChallengeResponse struct {
	ChallengeID ids.ID     `json:"challengeID"`
	NodeID      ids.NodeID `json:"nodeID"`
	Proof       []byte     `json:"proof"`
	Signature   []byte     `json:"signature"`
	Timestamp   time.Time  `json:"timestamp"`
}

// UptimeProof represents a signed proof of uptime
type UptimeProof struct {
	NodeID      ids.NodeID `json:"nodeID"`
	EpochID     uint64     `json:"epochID"`
	BlockHeight uint64     `json:"blockHeight"`
	Timestamp   time.Time  `json:"timestamp"`
	Signature   []byte     `json:"signature"`
}

// Hash returns a deterministic hash of the uptime proof
func (p *UptimeProof) Hash() [32]byte {
	h := sha256.New()
	h.Write(p.NodeID[:])
	binary.Write(h, binary.BigEndian, p.EpochID)
	binary.Write(h, binary.BigEndian, p.BlockHeight)
	binary.Write(h, binary.BigEndian, p.Timestamp.Unix())
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// StoredMessage represents a message stored in the inbox
type StoredMessage struct {
	ID          ids.ID     `json:"id"`
	RecipientID [32]byte   `json:"recipientID"` // Account ID hash
	SenderID    [32]byte   `json:"senderID"`    // Account ID hash (encrypted)
	SwarmID     uint64     `json:"swarmID"`
	Payload     []byte     `json:"payload"` // Encrypted message content
	TTL         uint64     `json:"ttl"`     // Seconds
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	Size        uint64     `json:"size"`
	Delivered   bool       `json:"delivered"`
	DeliveredAt time.Time  `json:"deliveredAt,omitempty"`
}

// IsExpired returns whether the message has expired
func (m *StoredMessage) IsExpired() bool {
	return time.Now().After(m.ExpiresAt)
}

// Hash returns a deterministic hash of the message
func (m *StoredMessage) Hash() [32]byte {
	h := sha256.New()
	h.Write(m.ID[:])
	h.Write(m.RecipientID[:])
	h.Write(m.Payload)
	binary.Write(h, binary.BigEndian, m.CreatedAt.Unix())
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// StorageCommitment represents a commitment to stored data
type StorageCommitment struct {
	NodeID        ids.NodeID `json:"nodeID"`
	EpochID       uint64     `json:"epochID"`
	StoreRoot     [32]byte   `json:"storeRoot"`     // Merkle root of stored messages
	MessageCount  uint64     `json:"messageCount"`
	TotalSize     uint64     `json:"totalSize"`
	Timestamp     time.Time  `json:"timestamp"`
	Signature     []byte     `json:"signature"`
}

// Hash returns a deterministic hash of the storage commitment
func (sc *StorageCommitment) Hash() [32]byte {
	h := sha256.New()
	h.Write(sc.NodeID[:])
	binary.Write(h, binary.BigEndian, sc.EpochID)
	h.Write(sc.StoreRoot[:])
	binary.Write(h, binary.BigEndian, sc.MessageCount)
	binary.Write(h, binary.BigEndian, sc.TotalSize)
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// RegistrationTx represents a service node registration transaction
type RegistrationTx struct {
	NodeID       ids.NodeID `json:"nodeID"`
	PublicKey    []byte     `json:"publicKey"`
	EndpointHash [32]byte   `json:"endpointHash"`
	StakeAmount  uint64     `json:"stakeAmount"`
	StakeLockEnd uint64     `json:"stakeLockEnd"`
	Signature    []byte     `json:"signature"`
}

// ExitTx represents a service node exit transaction
type ExitTx struct {
	NodeID    ids.NodeID `json:"nodeID"`
	Timestamp time.Time  `json:"timestamp"`
	Signature []byte     `json:"signature"`
}

// SlashTx represents a slashing transaction for misbehavior
type SlashTx struct {
	NodeID     ids.NodeID `json:"nodeID"`
	Reason     string     `json:"reason"`
	Evidence   []byte     `json:"evidence"`
	SlashAmount uint64    `json:"slashAmount"`
	Timestamp  time.Time  `json:"timestamp"`
}
