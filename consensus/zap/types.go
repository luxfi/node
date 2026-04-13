// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// QueryState represents the state of a consensus query
type QueryState struct {
	ID        string
	Content   []byte
	Submitter *DID
	Timestamp time.Time
	Responses map[string]*Response
	Votes     map[string][]*DID // ResponseID -> list of voter DIDs
	Finalized string            // ResponseID if finalized
}

// Response represents a response to a query
type Response struct {
	ID        string
	QueryID   string
	Content   []byte
	Responder *DID
	Timestamp time.Time
}

// ConsensusResult represents the outcome of consensus voting
type ConsensusResult struct {
	Response    *Response
	Votes       int
	TotalVoters int
	Confidence  float64
}

// Query represents an agentic consensus query
type Query struct {
	ID        string
	Content   []byte
	Submitter *DID
	Timestamp int64
}

// NewQuery creates a query with auto-generated ID
func NewQuery(content []byte, submitter *DID) *Query {
	timestamp := time.Now().Unix()
	data := append(content, []byte(submitter.String())...)
	data = append(data, int64ToBytes(timestamp)...)
	hash := sha256.Sum256(data)

	return &Query{
		ID:        hex.EncodeToString(hash[:]),
		Content:   content,
		Submitter: submitter,
		Timestamp: timestamp,
	}
}

// NewResponse creates a response with auto-generated ID
func NewResponse(queryID string, content []byte, responder *DID) *Response {
	timestamp := time.Now()
	queryIDBytes, _ := hex.DecodeString(queryID)
	data := append(queryIDBytes, content...)
	data = append(data, []byte(responder.String())...)
	data = append(data, int64ToBytes(timestamp.Unix())...)
	hash := sha256.Sum256(data)

	return &Response{
		ID:        hex.EncodeToString(hash[:]),
		QueryID:   queryID,
		Content:   content,
		Responder: responder,
		Timestamp: timestamp,
	}
}

// Vote represents a vote cast by a validator
type Vote struct {
	QueryID    string
	ResponseID string
	Voter      *DID
	Timestamp  int64
	Signature  []byte // Optional post-quantum signature
}

// NewVote creates a new vote
func NewVote(queryID, responseID string, voter *DID) *Vote {
	return &Vote{
		QueryID:    queryID,
		ResponseID: responseID,
		Voter:      voter,
		Timestamp:  time.Now().Unix(),
	}
}

// FinalityProof represents proof of agentic consensus finality
type FinalityProof struct {
	QueryID     string
	ResponseID  string
	Votes       []Vote
	TotalVoters int
	Confidence  float64
	Timestamp   int64
	Signature   []byte // Quasar hybrid signature
}

// ValidatorWeight represents a validator's weight in consensus
type ValidatorWeight struct {
	DID    *DID
	Weight uint64
	Stake  uint64
	Active bool
}

// AgentMessage represents a ZAP protocol message for agentic consensus
type AgentMessage struct {
	Type      AgentMessageType
	Query     *Query
	Response  *Response
	Vote      *Vote
	Signature []byte
}

// AgentMessageType identifies the type of agent message
type AgentMessageType uint8

const (
	AgentMessageTypeQuery AgentMessageType = iota
	AgentMessageTypeResponse
	AgentMessageTypeVote
	AgentMessageTypeFinality
)

// String returns the message type name
func (t AgentMessageType) String() string {
	switch t {
	case AgentMessageTypeQuery:
		return "Query"
	case AgentMessageTypeResponse:
		return "Response"
	case AgentMessageTypeVote:
		return "Vote"
	case AgentMessageTypeFinality:
		return "Finality"
	default:
		return "Unknown"
	}
}

func int64ToBytes(n int64) []byte {
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(n >> (i * 8))
	}
	return b
}

// PQSignatureType identifies the post-quantum signature algorithm
type PQSignatureType uint8

const (
	// PQSignatureTypeMLDSA65 is NIST FIPS 204 ML-DSA-65
	PQSignatureTypeMLDSA65 PQSignatureType = iota
	// PQSignatureTypeCorona is Ring-LWE based threshold signatures
	PQSignatureTypeCorona
	// PQSignatureTypeHybrid combines classical and post-quantum
	PQSignatureTypeHybrid
)

// PQSignature wraps a post-quantum signature
type PQSignature struct {
	Type      PQSignatureType
	Signature []byte
	PublicKey []byte
}

// PQKeypair represents a post-quantum keypair
type PQKeypair struct {
	Type       PQSignatureType
	PublicKey  []byte
	PrivateKey []byte
}

// Sign signs a message using the keypair (stub - real impl in pqcrypto)
func (k *PQKeypair) Sign(message []byte) (*PQSignature, error) {
	// Stub: returns SHA-256 hash as fixed-size signature for testing
	hash := sha256.Sum256(append(message, k.PrivateKey...))
	return &PQSignature{
		Type:      k.Type,
		Signature: hash[:],
		PublicKey: k.PublicKey,
	}, nil
}

// Verify verifies a signature (stub - real impl in pqcrypto)
func (k *PQKeypair) Verify(message []byte, sig *PQSignature) bool {
	// Stub: accepts any non-empty signature
	return sig != nil && len(sig.Signature) > 0
}

// StakeRegistry interface for validator stake tracking
type StakeRegistry interface {
	GetStake(did *DID) (uint64, error)
	SetStake(did *DID, amount uint64) error
	TotalStake() uint64
	HasSufficientStake(did *DID, minimum uint64) (bool, error)
	StakeWeight(did *DID) (float64, error)
}

// InMemoryStakeRegistry is a simple in-memory stake registry for testing
type InMemoryStakeRegistry struct {
	stakes map[string]uint64
}

// NewInMemoryStakeRegistry creates a new in-memory stake registry
func NewInMemoryStakeRegistry() *InMemoryStakeRegistry {
	return &InMemoryStakeRegistry{
		stakes: make(map[string]uint64),
	}
}

// GetStake returns the stake for a DID
func (r *InMemoryStakeRegistry) GetStake(did *DID) (uint64, error) {
	return r.stakes[did.String()], nil
}

// SetStake sets the stake for a DID
func (r *InMemoryStakeRegistry) SetStake(did *DID, amount uint64) error {
	r.stakes[did.String()] = amount
	return nil
}

// TotalStake returns the total stake across all validators
func (r *InMemoryStakeRegistry) TotalStake() uint64 {
	var total uint64
	for _, stake := range r.stakes {
		total += stake
	}
	return total
}

// HasSufficientStake checks if a DID has at least the minimum stake
func (r *InMemoryStakeRegistry) HasSufficientStake(did *DID, minimum uint64) (bool, error) {
	stake, _ := r.GetStake(did)
	return stake >= minimum, nil
}

// StakeWeight returns the stake weight as a fraction of total stake
func (r *InMemoryStakeRegistry) StakeWeight(did *DID) (float64, error) {
	stake, _ := r.GetStake(did)
	total := r.TotalStake()
	if total == 0 {
		return 0.0, nil
	}
	return float64(stake) / float64(total), nil
}
