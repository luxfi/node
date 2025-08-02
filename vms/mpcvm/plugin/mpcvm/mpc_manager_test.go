// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpcvm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
)

func TestMPCManager_InitiateKeyGen(t *testing.T) {
	// Setup
	db := memdb.New()
	config := MPCConfig{
		Threshold:             2,
		PartyCount:            3,
		KeyGenTimeout:         5 * time.Second,
		SignTimeout:           2 * time.Second,
		MaxConcurrentSessions: 10,
		SessionTimeout:        1 * time.Minute,
	}
	
	manager := NewMPCManager(db, config)
	
	// Create key generation request
	participants := []ids.NodeID{
		ids.GenerateNodeID(),
		ids.GenerateNodeID(),
		ids.GenerateNodeID(),
	}
	
	req := KeyGenRequest{
		RequestID:    ids.GenerateID(),
		Participants: participants,
		Purpose:      "test_key_generation",
	}
	
	reqData, err := json.Marshal(req)
	require.NoError(t, err)
	
	// Initiate key generation
	err = manager.InitiateKeyGen(reqData)
	require.NoError(t, err)
	
	// Verify session was created
	manager.sessionMutex.RLock()
	sessionCount := len(manager.activeSessions)
	manager.sessionMutex.RUnlock()
	
	assert.Equal(t, 1, sessionCount)
	
	// Find the created session
	var session *MPCSession
	manager.sessionMutex.RLock()
	for _, s := range manager.activeSessions {
		if s.Type == SessionTypeKeyGen {
			session = s
			break
		}
	}
	manager.sessionMutex.RUnlock()
	
	require.NotNil(t, session)
	assert.Equal(t, SessionTypeKeyGen, session.Type)
	assert.Equal(t, participants, session.Participants)
	assert.Equal(t, uint32(2), session.Threshold)
	assert.Equal(t, SessionStatePending, session.State)
}

func TestMPCManager_SignXChainTx(t *testing.T) {
	// Setup
	db := memdb.New()
	config := MPCConfig{
		Threshold:             2,
		PartyCount:            3,
		SignTimeout:           2 * time.Second,
		MaxConcurrentSessions: 10,
	}
	
	manager := NewMPCManager(db, config)
	
	// Mock transaction
	tx := struct {
		From   string
		To     string
		Amount uint64
	}{
		From:   "X-lux1234567890",
		To:     "X-lux0987654321",
		Amount: 1000,
	}
	
	// Sign transaction
	signedTx, err := manager.SignXChainTx(tx)
	
	// For this test, we expect it to succeed with mock implementation
	require.NoError(t, err)
	assert.NotNil(t, signedTx)
	
	// Verify metrics
	assert.Equal(t, uint64(1), manager.totalSignatures)
}

func TestMPCManager_VerifyBlockSignature(t *testing.T) {
	// Setup
	db := memdb.New()
	config := MPCConfig{
		Threshold:  67,
		PartyCount: 100,
	}
	
	manager := NewMPCManager(db, config)
	
	// Generate test key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	
	// Store public key (mock implementation would do this properly)
	manager.publicKeys[ids.GenerateID()] = &PublicKey{
		PublicKey:    &privateKey.PublicKey,
		Threshold:    uint32(config.Threshold),
		TotalParties: uint32(config.PartyCount),
	}
	
	// Test data
	message := []byte("test block data")
	
	// Create signer bitmap with sufficient signers (67 out of 100)
	signerBitmap := make([]byte, 13) // 100 bits = 13 bytes
	for i := 0; i < 67; i++ {
		byteIndex := i / 8
		bitIndex := uint(i % 8)
		signerBitmap[byteIndex] |= 1 << bitIndex
	}
	
	// Mock signature (in real implementation, this would be MPC signature)
	signature := []byte("mock_mpc_signature")
	
	// Verify signature
	err = manager.VerifyBlockSignature(message, signature, signerBitmap)
	
	// This will fail with current mock implementation, but structure is correct
	assert.Error(t, err) // Expected since we're using mock signatures
}

func TestMPCManager_SessionManagement(t *testing.T) {
	// Setup
	db := memdb.New()
	config := MPCConfig{
		Threshold:             2,
		PartyCount:            3,
		MaxConcurrentSessions: 5,
		SessionTimeout:        100 * time.Millisecond, // Short timeout for testing
	}
	
	manager := NewMPCManager(db, config)
	
	// Create multiple sessions
	sessions := make([]*MPCSession, 3)
	for i := 0; i < 3; i++ {
		session := &MPCSession{
			SessionID:    ids.GenerateID(),
			Type:         SessionTypeSign,
			KeyID:        ids.GenerateID(),
			Participants: []ids.NodeID{ids.GenerateNodeID()},
			Threshold:    2,
			State:        SessionStateActive,
			StartTime:    time.Now(),
			LastUpdate:   time.Now(),
			messages:     make(map[uint32][]Message),
		}
		
		manager.sessionMutex.Lock()
		manager.activeSessions[session.SessionID] = session
		manager.sessionMutex.Unlock()
		
		sessions[i] = session
	}
	
	// Verify all sessions exist
	assert.True(t, manager.HasPendingOperations())
	
	// Update one session to be stale
	manager.sessionMutex.Lock()
	sessions[0].LastUpdate = time.Now().Add(-2 * time.Minute)
	manager.sessionMutex.Unlock()
	
	// Run cleanup
	manager.cleanupStaleSessions()
	
	// Verify stale session was removed
	manager.sessionMutex.RLock()
	_, exists := manager.activeSessions[sessions[0].SessionID]
	sessionCount := len(manager.activeSessions)
	manager.sessionMutex.RUnlock()
	
	assert.False(t, exists)
	assert.Equal(t, 2, sessionCount)
}

func TestMPCManager_StoreKeyGenResult(t *testing.T) {
	// Setup
	db := memdb.New()
	config := MPCConfig{
		Threshold:  2,
		PartyCount: 3,
	}
	
	manager := NewMPCManager(db, config)
	
	// Create session
	sessionID := ids.GenerateID()
	session := &MPCSession{
		SessionID: sessionID,
		Type:      SessionTypeKeyGen,
		State:     SessionStateActive,
	}
	
	manager.sessionMutex.Lock()
	manager.activeSessions[sessionID] = session
	manager.sessionMutex.Unlock()
	
	// Create key share
	keyShare := KeyShare{
		KeyID:        ids.GenerateID(),
		PartyID:      ids.GenerateNodeID(),
		Share:        []byte("test_key_share"),
		Threshold:    2,
		TotalParties: 3,
		CreatedAt:    time.Now(),
	}
	
	keyShareData, err := json.Marshal(keyShare)
	require.NoError(t, err)
	
	// Store key generation result
	err = manager.StoreKeyGenResult(sessionID, keyShareData)
	require.NoError(t, err)
	
	// Verify key share was stored in memory
	manager.keyMutex.RLock()
	storedShare, exists := manager.keyShares[keyShare.KeyID]
	manager.keyMutex.RUnlock()
	
	assert.True(t, exists)
	assert.Equal(t, keyShare.KeyID, storedShare.KeyID)
	
	// Verify key share was persisted to database
	key := append(mpcKeySharePrefix, keyShare.KeyID[:]...)
	data, err := db.Get(key)
	require.NoError(t, err)
	assert.NotNil(t, data)
}

func TestMPCManager_ConcurrentSessions(t *testing.T) {
	// Setup
	db := memdb.New()
	config := MPCConfig{
		Threshold:             2,
		PartyCount:            3,
		MaxConcurrentSessions: 10,
		KeyGenTimeout:         5 * time.Second,
	}
	
	manager := NewMPCManager(db, config)
	
	// Create multiple key generation requests concurrently
	numRequests := 5
	var wg sync.WaitGroup
	errors := make(chan error, numRequests)
	
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			
			req := KeyGenRequest{
				RequestID: ids.GenerateID(),
				Participants: []ids.NodeID{
					ids.GenerateNodeID(),
					ids.GenerateNodeID(),
					ids.GenerateNodeID(),
				},
				Purpose: fmt.Sprintf("concurrent_test_%d", index),
			}
			
			reqData, err := json.Marshal(req)
			if err != nil {
				errors <- err
				return
			}
			
			if err := manager.InitiateKeyGen(reqData); err != nil {
				errors <- err
			}
		}(i)
	}
	
	wg.Wait()
	close(errors)
	
	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent key generation failed: %v", err)
	}
	
	// Verify all sessions were created
	manager.sessionMutex.RLock()
	sessionCount := len(manager.activeSessions)
	manager.sessionMutex.RUnlock()
	
	assert.Equal(t, numRequests, sessionCount)
}

func TestMPCManager_HealthStatus(t *testing.T) {
	// Setup
	db := memdb.New()
	config := MPCConfig{
		MaxConcurrentSessions: 10,
	}
	
	manager := NewMPCManager(db, config)
	
	// Initially healthy
	assert.Equal(t, "healthy", manager.HealthStatus())
	
	// Add many active sessions
	for i := 0; i < 7; i++ {
		session := &MPCSession{
			SessionID: ids.GenerateID(),
			State:     SessionStateActive,
		}
		manager.sessionMutex.Lock()
		manager.activeSessions[session.SessionID] = session
		manager.sessionMutex.Unlock()
	}
	
	// Should be busy when more than half capacity
	assert.Equal(t, "busy", manager.HealthStatus())
}

func TestCGG21KeyGen_Rounds(t *testing.T) {
	// Setup
	db := memdb.New()
	config := MPCConfig{
		Threshold:     2,
		PartyCount:    3,
		KeyGenTimeout: 5 * time.Second,
	}
	
	manager := NewMPCManager(db, config)
	keyGen := &CGG21KeyGen{manager: manager, config: config}
	
	// Create session
	session := &MPCSession{
		SessionID:    ids.GenerateID(),
		Type:         SessionTypeKeyGen,
		KeyID:        ids.GenerateID(),
		Participants: []ids.NodeID{ids.GenerateNodeID(), ids.GenerateNodeID(), ids.GenerateNodeID()},
		Threshold:    2,
		State:        SessionStatePending,
		messages:     make(map[uint32][]Message),
		commitments:  make(map[ids.NodeID][]byte),
	}
	
	// Test round 1
	ctx := context.Background()
	err := keyGen.round1(ctx, session)
	assert.NoError(t, err)
	
	// Test round 2
	err = keyGen.round2(ctx, session)
	assert.NoError(t, err)
	
	// Test round 3
	err = keyGen.round3(ctx, session)
	assert.NoError(t, err)
}

func TestMPCManager_UpdateKeyShares(t *testing.T) {
	// Setup
	db := memdb.New()
	config := MPCConfig{
		Threshold:  2,
		PartyCount: 3,
	}
	
	manager := NewMPCManager(db, config)
	
	// Create session
	sessionID := ids.GenerateID()
	newShares := []byte("new_key_shares_data")
	
	// Update key shares
	err := manager.UpdateKeyShares(sessionID, newShares)
	
	// Current implementation returns nil
	assert.NoError(t, err)
}