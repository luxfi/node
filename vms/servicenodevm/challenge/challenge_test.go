// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package challenge

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/servicenodevm"
)

func setupTestManager(t *testing.T, nodeCount int) (*Manager, *servicenodevm.Registry) {
	db := memdb.New()
	config := servicenodevm.DefaultConfig()

	registry := servicenodevm.NewRegistry(db, config)
	ctx := context.Background()
	registry.Load(ctx)

	// Register and activate nodes
	for i := 0; i < nodeCount; i++ {
		nodeID := ids.GenerateTestNodeID()
		tx := &servicenodevm.RegistrationTx{
			NodeID:       nodeID,
			PublicKey:    []byte("test-key"),
			StakeAmount:  config.MinStake,
			StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
		}
		registry.Register(ctx, tx)
		registry.Activate(ctx, nodeID)
	}

	manager := NewManager(db, registry, config)

	return manager, registry
}

func TestManagerCreateChallenge(t *testing.T) {
	manager, registry := setupTestManager(t, 5)
	ctx := context.Background()

	activeNodes := registry.GetActiveNodes()
	if len(activeNodes) < 2 {
		t.Fatal("need at least 2 active nodes for test")
	}

	targetNode := activeNodes[0]
	issuerNode := activeNodes[1]

	challenge, err := manager.CreateChallenge(ctx, 1, targetNode.NodeID, issuerNode.NodeID, servicenodevm.ChallengeTypeUptime)
	if err != nil {
		t.Fatalf("failed to create challenge: %v", err)
	}

	if challenge.TargetNodeID != targetNode.NodeID {
		t.Errorf("wrong target node ID")
	}

	if challenge.IssuerNodeID != issuerNode.NodeID {
		t.Errorf("wrong issuer node ID")
	}

	if challenge.Type != servicenodevm.ChallengeTypeUptime {
		t.Errorf("expected challenge type %s, got %s", servicenodevm.ChallengeTypeUptime, challenge.Type)
	}

	// Challenge should not be responded yet
	if challenge.Responded {
		t.Errorf("new challenge should not be marked as responded")
	}

	if challenge.Nonce == [32]byte{} {
		t.Errorf("nonce not generated")
	}
}

func TestManagerRespondToChallenge(t *testing.T) {
	manager, registry := setupTestManager(t, 5)
	ctx := context.Background()

	activeNodes := registry.GetActiveNodes()
	targetNode := activeNodes[0]
	issuerNode := activeNodes[1]

	// Create challenge
	challenge, _ := manager.CreateChallenge(ctx, 1, targetNode.NodeID, issuerNode.NodeID, servicenodevm.ChallengeTypeUptime)

	// Create valid response using the correct struct fields
	// Signature needs to be at least 32 bytes for verification
	signature := make([]byte, 32)
	copy(signature, "test signature padding to 32bytes")
	response := &servicenodevm.ChallengeResponse{
		ChallengeID: challenge.ID,
		NodeID:      targetNode.NodeID,
		Proof:       []byte("test proof data"),
		Signature:   signature,
		Timestamp:   time.Now(),
	}

	// Submit response
	if err := manager.RespondToChallenge(ctx, response); err != nil {
		t.Fatalf("failed to respond to challenge: %v", err)
	}

	// Verify challenge is responded
	retrieved, _ := manager.GetChallenge(challenge.ID)
	if !retrieved.Responded {
		t.Errorf("challenge should be marked as responded")
	}

	if !retrieved.Success {
		t.Errorf("challenge response should be successful")
	}
}

func TestManagerChallengeExpiry(t *testing.T) {
	db := memdb.New()
	config := servicenodevm.DefaultConfig()
	config.ChallengeTTL = 1 // 1 second TTL

	registry := servicenodevm.NewRegistry(db, config)
	ctx := context.Background()
	registry.Load(ctx)

	// Register nodes
	nodeIDs := make([]ids.NodeID, 2)
	for i := 0; i < 2; i++ {
		nodeIDs[i] = ids.GenerateTestNodeID()
		tx := &servicenodevm.RegistrationTx{
			NodeID:       nodeIDs[i],
			PublicKey:    []byte("test-key"),
			StakeAmount:  config.MinStake,
			StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
		}
		registry.Register(ctx, tx)
		registry.Activate(ctx, nodeIDs[i])
	}

	manager := NewManager(db, registry, config)

	// Create challenge
	challenge, _ := manager.CreateChallenge(ctx, 1, nodeIDs[0], nodeIDs[1], servicenodevm.ChallengeTypeUptime)

	// Wait for expiry
	time.Sleep(2 * time.Second)

	// Process expired
	err := manager.ProcessExpiredChallenges(ctx)
	if err != nil {
		t.Fatalf("failed to process expired: %v", err)
	}

	// Verify challenge is failed (responded but not successful)
	retrieved, _ := manager.GetChallenge(challenge.ID)
	if !retrieved.Responded {
		t.Errorf("expired challenge should be marked as responded")
	}
	if retrieved.Success {
		t.Errorf("expired challenge should not be successful")
	}
}

func TestManagerInvalidResponse(t *testing.T) {
	manager, registry := setupTestManager(t, 5)
	ctx := context.Background()

	activeNodes := registry.GetActiveNodes()
	targetNode := activeNodes[0]
	issuerNode := activeNodes[1]
	otherNode := activeNodes[2]

	// Create challenge
	challenge, _ := manager.CreateChallenge(ctx, 1, targetNode.NodeID, issuerNode.NodeID, servicenodevm.ChallengeTypeUptime)

	// Create response from wrong node
	response := &servicenodevm.ChallengeResponse{
		ChallengeID: challenge.ID,
		NodeID:      otherNode.NodeID, // Wrong node
		Proof:       []byte("test proof"),
		Signature:   []byte("test signature"),
		Timestamp:   time.Now(),
	}

	err := manager.RespondToChallenge(ctx, response)
	if err != servicenodevm.ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestManagerGetPendingChallenges(t *testing.T) {
	manager, registry := setupTestManager(t, 5)
	ctx := context.Background()

	activeNodes := registry.GetActiveNodes()
	targetNode := activeNodes[0]

	// Create multiple challenges for same target
	for i := 0; i < 3; i++ {
		manager.CreateChallenge(ctx, 1, targetNode.NodeID, activeNodes[i+1].NodeID, servicenodevm.ChallengeTypeUptime)
	}

	// Get pending challenges
	pending := manager.GetPendingChallenges(targetNode.NodeID)
	if len(pending) != 3 {
		t.Errorf("expected 3 pending challenges, got %d", len(pending))
	}
}

func TestManagerDistributeRewards(t *testing.T) {
	manager, registry := setupTestManager(t, 5)
	ctx := context.Background()

	activeNodes := registry.GetActiveNodes()

	// Create and pass challenges for first 3 nodes
	for i := 0; i < 3; i++ {
		targetNode := activeNodes[i]
		issuerNode := activeNodes[(i+1)%5]

		challenge, _ := manager.CreateChallenge(ctx, 1, targetNode.NodeID, issuerNode.NodeID, servicenodevm.ChallengeTypeUptime)

		// Create valid signature (at least 32 bytes)
		sig := make([]byte, 32)
		copy(sig, "valid signature for reward test")
		response := &servicenodevm.ChallengeResponse{
			ChallengeID: challenge.ID,
			NodeID:      targetNode.NodeID,
			Proof:       []byte("proof"),
			Signature:   sig,
			Timestamp:   time.Now(),
		}
		manager.RespondToChallenge(ctx, response)
	}

	// Distribute rewards
	rewardPool := uint64(1000)
	err := manager.DistributeRewards(ctx, 1, rewardPool)
	if err != nil {
		t.Fatalf("failed to distribute rewards: %v", err)
	}

	// Verify rewards were distributed
	for i := 0; i < 3; i++ {
		node, _ := registry.Get(activeNodes[i].NodeID)
		if node.PendingRewards == 0 {
			t.Errorf("node %d should have rewards", i)
		}
	}
}

func TestManagerStorageChallenge(t *testing.T) {
	manager, registry := setupTestManager(t, 5)
	ctx := context.Background()

	activeNodes := registry.GetActiveNodes()
	targetNode := activeNodes[0]
	issuerNode := activeNodes[1]

	// Create storage challenge
	challenge, err := manager.CreateChallenge(ctx, 1, targetNode.NodeID, issuerNode.NodeID, servicenodevm.ChallengeTypeStorage)
	if err != nil {
		t.Fatalf("failed to create storage challenge: %v", err)
	}

	if challenge.Type != servicenodevm.ChallengeTypeStorage {
		t.Errorf("expected storage challenge type")
	}

	// Respond to storage challenge
	// Storage proof requires at least 64 bytes
	storageProof := make([]byte, 64)
	copy(storageProof, "storage proof data with merkle path padding to 64 bytes")
	response := &servicenodevm.ChallengeResponse{
		ChallengeID: challenge.ID,
		NodeID:      targetNode.NodeID,
		Proof:       storageProof,
		Signature:   make([]byte, 32),
		Timestamp:   time.Now(),
	}

	if err := manager.RespondToChallenge(ctx, response); err != nil {
		t.Fatalf("failed to respond to storage challenge: %v", err)
	}
}

func TestManagerChallengeNotFound(t *testing.T) {
	manager, _ := setupTestManager(t, 5)

	// Try to get non-existent challenge
	_, err := manager.GetChallenge(ids.Empty)
	if err != servicenodevm.ErrChallengeNotFound {
		t.Errorf("expected ErrChallengeNotFound, got %v", err)
	}
}

func TestManagerRespondToNonExistentChallenge(t *testing.T) {
	manager, _ := setupTestManager(t, 5)
	ctx := context.Background()

	response := &servicenodevm.ChallengeResponse{
		ChallengeID: ids.Empty,
		Timestamp:   time.Now(),
	}

	err := manager.RespondToChallenge(ctx, response)
	if err != servicenodevm.ErrChallengeNotFound {
		t.Errorf("expected ErrChallengeNotFound, got %v", err)
	}
}

func TestManagerMultipleEpochs(t *testing.T) {
	manager, registry := setupTestManager(t, 5)
	ctx := context.Background()

	activeNodes := registry.GetActiveNodes()

	// Create challenges in epoch 1
	challenge1, _ := manager.CreateChallenge(ctx, 1, activeNodes[0].NodeID, activeNodes[1].NodeID, servicenodevm.ChallengeTypeUptime)

	// Create challenges in epoch 2
	challenge2, _ := manager.CreateChallenge(ctx, 2, activeNodes[0].NodeID, activeNodes[1].NodeID, servicenodevm.ChallengeTypeUptime)

	if challenge1.EpochID != 1 {
		t.Errorf("expected epoch 1, got %d", challenge1.EpochID)
	}

	if challenge2.EpochID != 2 {
		t.Errorf("expected epoch 2, got %d", challenge2.EpochID)
	}
}
