// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpcvm

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/bits"
	"sync"
	"time"

	"github.com/luxfi/geth/crypto"
	"github.com/luxfi/geth/log"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
)

var (
	// Database keys
	mpcKeySharePrefix     = []byte("mpc_key_share")
	mpcSessionPrefix      = []byte("mpc_session")
	mpcPublicKeyPrefix    = []byte("mpc_public_key")
	mpcSignaturePrefix    = []byte("mpc_signature")
)

// MPCManager manages MPC operations for the M-Chain
type MPCManager struct {
	db              database.Database
	config          MPCConfig
	
	// Key management
	keyShares       map[ids.ID]*KeyShare
	publicKeys      map[ids.ID]*PublicKey
	keyMutex        sync.RWMutex
	
	// Session management
	activeSessions  map[ids.ID]*MPCSession
	sessionMutex    sync.RWMutex
	
	// CGG21 protocol instances
	keyGenInstance  *CGG21KeyGen
	signInstance    *CGG21Sign
	reshareInstance *CGG21Reshare
	
	// Metrics
	totalKeysGenerated uint64
	totalSignatures    uint64
	failedOperations   uint64
}

// KeyShare represents a party's share of an MPC key
type KeyShare struct {
	KeyID           ids.ID
	PartyID         ids.NodeID
	Share           []byte
	PublicKey       *ecdsa.PublicKey
	Threshold       uint32
	TotalParties    uint32
	CreatedAt       time.Time
	LastUsed        time.Time
}

// PublicKey represents an MPC public key
type PublicKey struct {
	KeyID           ids.ID
	PublicKey       *ecdsa.PublicKey
	Threshold       uint32
	TotalParties    uint32
	CreatedAt       time.Time
}

// MPCSession represents an active MPC session
type MPCSession struct {
	SessionID       ids.ID
	Type            MPCSessionType
	KeyID           ids.ID
	Participants    []ids.NodeID
	Threshold       uint32
	State           SessionState
	StartTime       time.Time
	LastUpdate      time.Time
	Result          []byte
	Error           error
	
	// CGG21 specific
	round           uint32
	messages        map[uint32][]Message
	commitments     map[ids.NodeID][]byte
}

// MPCSessionType defines the type of MPC session
type MPCSessionType uint8

const (
	SessionTypeKeyGen MPCSessionType = iota
	SessionTypeSign
	SessionTypeReshare
	SessionTypeRefresh
)

// SessionState defines the state of an MPC session
type SessionState uint8

const (
	SessionStatePending SessionState = iota
	SessionStateActive
	SessionStateCompleted
	SessionStateFailed
	SessionStateAborted
)

// Message represents a message in the MPC protocol
type Message struct {
	From            ids.NodeID
	To              ids.NodeID
	Round           uint32
	Content         []byte
	Signature       []byte
}

// CGG21KeyGen implements the Canetti-Gennaro-Goldfeder 2021 key generation protocol
type CGG21KeyGen struct {
	manager         *MPCManager
	config          MPCConfig
}

// CGG21Sign implements the CGG21 signing protocol
type CGG21Sign struct {
	manager         *MPCManager
	config          MPCConfig
}

// CGG21Reshare implements the CGG21 resharing protocol
type CGG21Reshare struct {
	manager         *MPCManager
	config          MPCConfig
}

// NewMPCManager creates a new MPC manager
func NewMPCManager(db database.Database, config MPCConfig) *MPCManager {
	m := &MPCManager{
		db:             db,
		config:         config,
		keyShares:      make(map[ids.ID]*KeyShare),
		publicKeys:     make(map[ids.ID]*PublicKey),
		activeSessions: make(map[ids.ID]*MPCSession),
	}
	
	// Initialize protocol instances
	m.keyGenInstance = &CGG21KeyGen{manager: m, config: config}
	m.signInstance = &CGG21Sign{manager: m, config: config}
	m.reshareInstance = &CGG21Reshare{manager: m, config: config}
	
	// Load existing keys from database
	m.loadKeys()
	
	return m
}

// InitiateKeyGen starts a new key generation session
func (m *MPCManager) InitiateKeyGen(data []byte) error {
	// Parse key generation request
	var req KeyGenRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("failed to parse key gen request: %w", err)
	}
	
	// Create new session
	session := &MPCSession{
		SessionID:    ids.Empty.Prefix(uint64(time.Now().UnixNano())),
		Type:         SessionTypeKeyGen,
		KeyID:        ids.Empty.Prefix(uint64(time.Now().UnixNano()) + 1),
		Participants: req.Participants,
		Threshold:    uint32(m.config.Threshold),
		State:        SessionStatePending,
		StartTime:    time.Now(),
		LastUpdate:   time.Now(),
		messages:     make(map[uint32][]Message),
		commitments:  make(map[ids.NodeID][]byte),
	}
	
	// Store session
	m.sessionMutex.Lock()
	m.activeSessions[session.SessionID] = session
	m.sessionMutex.Unlock()
	
	// Start key generation protocol
	go m.keyGenInstance.Run(session)
	
	log.Info("Initiated key generation session",
		"sessionID", session.SessionID,
		"keyID", session.KeyID,
		"participants", len(session.Participants),
		"threshold", session.Threshold,
	)
	
	return nil
}

// Run executes the key generation protocol
func (kg *CGG21KeyGen) Run(session *MPCSession) {
	ctx, cancel := context.WithTimeout(context.Background(), kg.config.KeyGenTimeout)
	defer cancel()
	
	// Round 1: Generate and broadcast commitments
	if err := kg.round1(ctx, session); err != nil {
		session.Error = fmt.Errorf("round 1 failed: %w", err)
		session.State = SessionStateFailed
		return
	}
	
	// Round 2: Generate and share secret shares
	if err := kg.round2(ctx, session); err != nil {
		session.Error = fmt.Errorf("round 2 failed: %w", err)
		session.State = SessionStateFailed
		return
	}
	
	// Round 3: Verify shares and compute public key
	if err := kg.round3(ctx, session); err != nil {
		session.Error = fmt.Errorf("round 3 failed: %w", err)
		session.State = SessionStateFailed
		return
	}
	
	// Key generation completed successfully
	session.State = SessionStateCompleted
	kg.manager.totalKeysGenerated++
	
	log.Info("Key generation completed",
		"sessionID", session.SessionID,
		"keyID", session.KeyID,
	)
}

// round1 implements the first round of CGG21 key generation
func (kg *CGG21KeyGen) round1(ctx context.Context, session *MPCSession) error {
	// Generate random polynomial coefficients for Shamir secret sharing
	coefficients := make([]*big.Int, session.Threshold)
	for i := range coefficients {
		// Generate a random private key and extract the D value
		key, _ := crypto.GenerateKey()
		coefficients[i] = key.D
	}
	
	// Compute commitments to coefficients
	commitments := make([][]byte, len(coefficients))
	for i, coeff := range coefficients {
		commitments[i] = crypto.Keccak256(coeff.Bytes())
	}
	
	// Broadcast commitments to all participants
	// Implementation would use the network layer
	
	return nil
}

// round2 implements the second round of CGG21 key generation
func (kg *CGG21KeyGen) round2(ctx context.Context, session *MPCSession) error {
	// Generate shares for each participant using Shamir secret sharing
	// Send encrypted shares to respective participants
	// Implementation details omitted for brevity
	
	return nil
}

// round3 implements the third round of CGG21 key generation
func (kg *CGG21KeyGen) round3(ctx context.Context, session *MPCSession) error {
	// Verify received shares
	// Compute public key from commitments
	// Store key share securely
	// Implementation details omitted for brevity
	
	return nil
}

// SignXChainTx signs an X-Chain transaction using MPC
func (m *MPCManager) SignXChainTx(tx interface{}) (interface{}, error) {
	// Serialize transaction
	txBytes, err := serializeXChainTx(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize transaction: %w", err)
	}
	
	// Get appropriate key for X-Chain operations
	keyID := m.getXChainKeyID()
	
	// Create signing session
	session := &MPCSession{
		SessionID:    ids.Empty.Prefix(uint64(time.Now().UnixNano())),
		Type:         SessionTypeSign,
		KeyID:        keyID,
		Participants: m.getActiveParticipants(),
		Threshold:    uint32(m.config.Threshold),
		State:        SessionStatePending,
		StartTime:    time.Now(),
		LastUpdate:   time.Now(),
		messages:     make(map[uint32][]Message),
	}
	
	// Store session
	m.sessionMutex.Lock()
	m.activeSessions[session.SessionID] = session
	m.sessionMutex.Unlock()
	
	// Run signing protocol
	ctx, cancel := context.WithTimeout(context.Background(), m.config.SignTimeout)
	defer cancel()
	
	if err := m.signInstance.Sign(ctx, session, txBytes); err != nil {
		m.failedOperations++
		return nil, fmt.Errorf("signing failed: %w", err)
	}
	
	m.totalSignatures++
	
	// Return signed transaction
	return deserializeSignedXChainTx(session.Result)
}

// Sign executes the CGG21 signing protocol
func (cs *CGG21Sign) Sign(ctx context.Context, session *MPCSession, message []byte) error {
	// CGG21 signing protocol implementation
	// This is a simplified version - actual implementation would be more complex
	
	// Round 1: Generate random nonce shares
	// Round 2: Compute partial signatures
	// Round 3: Combine partial signatures
	
	// For now, return a placeholder
	session.Result = []byte("signed_transaction")
	session.State = SessionStateCompleted
	
	return nil
}

// VerifyBlockSignature verifies an MPC signature on a block
func (m *MPCManager) VerifyBlockSignature(message []byte, signature []byte, signerBitmap []byte) error {
	// Get the public key for block signing
	pubKey := m.getBlockSigningPublicKey()
	if pubKey == nil {
		return errors.New("block signing public key not found")
	}
	
	// Verify the signature
	hash := crypto.Keccak256(message)
	if !crypto.VerifySignature(pubKey, hash, signature) {
		return errors.New("invalid block signature")
	}
	
	// Verify signer bitmap meets threshold
	signerCount := 0
	for _, b := range signerBitmap {
		signerCount += bits.OnesCount8(uint8(b))
	}
	
	if signerCount < m.config.Threshold {
		return fmt.Errorf("insufficient signers: %d < %d", signerCount, m.config.Threshold)
	}
	
	return nil
}

// StoreKeyGenResult stores the result of key generation
func (m *MPCManager) StoreKeyGenResult(sessionID ids.ID, result []byte) error {
	m.sessionMutex.Lock()
	_, exists := m.activeSessions[sessionID]
	m.sessionMutex.Unlock()
	
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	
	// Parse and store the key share
	var keyShare KeyShare
	if err := json.Unmarshal(result, &keyShare); err != nil {
		return fmt.Errorf("failed to parse key share: %w", err)
	}
	
	// Store in memory
	m.keyMutex.Lock()
	m.keyShares[keyShare.KeyID] = &keyShare
	m.keyMutex.Unlock()
	
	// Persist to database
	key := append(mpcKeySharePrefix, keyShare.KeyID[:]...)
	return m.db.Put(key, result)
}

// StoreSignature stores a signature result
func (m *MPCManager) StoreSignature(sessionID ids.ID, signature []byte) error {
	m.sessionMutex.Lock()
	_, exists := m.activeSessions[sessionID]
	m.sessionMutex.Unlock()
	
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	
	// Store signature
	key := append(mpcSignaturePrefix, sessionID[:]...)
	return m.db.Put(key, signature)
}

// UpdateKeyShares updates key shares after resharing
func (m *MPCManager) UpdateKeyShares(sessionID ids.ID, newShares []byte) error {
	// Implementation for updating key shares after resharing
	return nil
}

// HasPendingOperations returns true if there are pending MPC operations
func (m *MPCManager) HasPendingOperations() bool {
	m.sessionMutex.RLock()
	defer m.sessionMutex.RUnlock()
	
	for _, session := range m.activeSessions {
		if session.State == SessionStatePending || session.State == SessionStateActive {
			return true
		}
	}
	
	return false
}

// CleanupSession cleans up a session
func (m *MPCManager) CleanupSession(sessionID ids.ID) {
	m.sessionMutex.Lock()
	defer m.sessionMutex.Unlock()
	
	delete(m.activeSessions, sessionID)
}

// Run starts the MPC manager background tasks
func (m *MPCManager) Run(shutdown <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	
	// Periodic cleanup of stale sessions
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			m.cleanupStaleSessions()
			
		case <-shutdown:
			log.Info("MPC manager shutting down")
			return
		}
	}
}

// cleanupStaleSessions removes sessions that have timed out
func (m *MPCManager) cleanupStaleSessions() {
	m.sessionMutex.Lock()
	defer m.sessionMutex.Unlock()
	
	now := time.Now()
	for id, session := range m.activeSessions {
		if now.Sub(session.LastUpdate) > m.config.SessionTimeout {
			log.Warn("Cleaning up stale session", "sessionID", id)
			delete(m.activeSessions, id)
		}
	}
}

// loadKeys loads existing keys from the database
func (m *MPCManager) loadKeys() {
	// Implementation to load keys from database on startup
}

// getXChainKeyID returns the key ID for X-Chain operations
func (m *MPCManager) getXChainKeyID() ids.ID {
	// Implementation to get the appropriate key for X-Chain operations
	return ids.Empty.Prefix(uint64(time.Now().UnixNano()))
}

// getActiveParticipants returns the list of active MPC participants
func (m *MPCManager) getActiveParticipants() []ids.NodeID {
	// Implementation to get active validators participating in MPC
	return []ids.NodeID{}
}

// getBlockSigningPublicKey returns the public key for block signing
func (m *MPCManager) getBlockSigningPublicKey() []byte {
	// Implementation to get the block signing public key
	return nil
}

// HealthStatus returns the health status of the MPC manager
func (m *MPCManager) HealthStatus() string {
	m.sessionMutex.RLock()
	activeCount := 0
	for _, session := range m.activeSessions {
		if session.State == SessionStateActive {
			activeCount++
		}
	}
	m.sessionMutex.RUnlock()
	
	if activeCount > m.config.MaxConcurrentSessions/2 {
		return "busy"
	}
	
	return "healthy"
}

// Helper structures

type KeyGenRequest struct {
	RequestID     ids.ID
	Participants  []ids.NodeID
	Purpose       string
}

func serializeXChainTx(tx interface{}) ([]byte, error) {
	// Implementation to serialize X-Chain transaction
	return json.Marshal(tx)
}

func deserializeSignedXChainTx(data []byte) (interface{}, error) {
	// Implementation to deserialize signed X-Chain transaction
	return data, nil
}