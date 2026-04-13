// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package epoch

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/servicenodevm"
)

func setupTestCoordinator(t *testing.T, nodeCount int) (*Coordinator, *servicenodevm.Registry) {
	db := memdb.New()
	config := servicenodevm.DefaultConfig()
	config.NodesPerSwarm = 3

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

	coordinator := NewCoordinator(db, registry, config)

	return coordinator, registry
}

func TestCoordinatorTransitionEpoch(t *testing.T) {
	coordinator, _ := setupTestCoordinator(t, 9)
	ctx := context.Background()

	// Transition to first epoch
	var blockHash [32]byte
	copy(blockHash[:], "test-block-hash")

	epoch, err := coordinator.TransitionEpoch(ctx, 100, blockHash)
	if err != nil {
		t.Fatalf("failed to transition epoch: %v", err)
	}

	if epoch.ID != 0 {
		t.Errorf("expected epoch ID 0, got %d", epoch.ID)
	}

	if epoch.StartHeight != 100 {
		t.Errorf("expected start height 100, got %d", epoch.StartHeight)
	}

	if epoch.ActiveNodeCount != 9 {
		t.Errorf("expected 9 active nodes, got %d", epoch.ActiveNodeCount)
	}

	// With 9 nodes and 3 per swarm, expect 3 swarms
	if epoch.SwarmCount != 3 {
		t.Errorf("expected 3 swarms, got %d", epoch.SwarmCount)
	}

	// Verify current epoch is set
	current := coordinator.GetCurrentEpoch()
	if current == nil || current.ID != epoch.ID {
		t.Errorf("current epoch not set correctly")
	}
}

func TestCoordinatorSwarmAssignment(t *testing.T) {
	coordinator, _ := setupTestCoordinator(t, 9)
	ctx := context.Background()

	// Create epoch
	var blockHash [32]byte
	copy(blockHash[:], "test-block-hash")
	coordinator.TransitionEpoch(ctx, 100, blockHash)

	// Test swarm assignment for multiple accounts
	accounts := make([][32]byte, 10)
	for i := 0; i < 10; i++ {
		copy(accounts[i][:], []byte{byte(i)})
	}

	swarmAssignments := make(map[uint64]int)
	for _, accountID := range accounts {
		swarmID, err := coordinator.GetSwarmForAccount(accountID)
		if err != nil {
			t.Fatalf("failed to get swarm for account: %v", err)
		}
		swarmAssignments[swarmID]++
	}

	// Verify all assignments are to valid swarms
	for swarmID := range swarmAssignments {
		if swarmID >= 3 {
			t.Errorf("invalid swarm ID %d, expected < 3", swarmID)
		}
	}
}

func TestCoordinatorGetSwarmNodes(t *testing.T) {
	coordinator, _ := setupTestCoordinator(t, 9)
	ctx := context.Background()

	// Create epoch
	var blockHash [32]byte
	copy(blockHash[:], "test-block-hash")
	coordinator.TransitionEpoch(ctx, 100, blockHash)

	// Get nodes for each swarm
	for swarmID := uint64(0); swarmID < 3; swarmID++ {
		nodes, err := coordinator.GetSwarmNodes(swarmID)
		if err != nil {
			t.Fatalf("failed to get swarm nodes: %v", err)
		}

		if len(nodes) < 3 {
			t.Errorf("swarm %d has %d nodes, expected >= 3", swarmID, len(nodes))
		}
	}
}

func TestCoordinatorDeterministicAssignment(t *testing.T) {
	coordinator1, _ := setupTestCoordinator(t, 9)
	coordinator2, _ := setupTestCoordinator(t, 9)
	ctx := context.Background()

	// Use same block hash for both
	var blockHash [32]byte
	copy(blockHash[:], "deterministic-seed")

	coordinator1.TransitionEpoch(ctx, 100, blockHash)
	coordinator2.TransitionEpoch(ctx, 100, blockHash)

	// Test that same account gets same swarm
	var accountID [32]byte
	copy(accountID[:], "test-account")

	swarm1, _ := coordinator1.GetSwarmForAccount(accountID)
	swarm2, _ := coordinator2.GetSwarmForAccount(accountID)

	if swarm1 != swarm2 {
		t.Errorf("swarm assignment not deterministic: %d vs %d", swarm1, swarm2)
	}
}

func TestCoordinatorAssignmentProof(t *testing.T) {
	coordinator, _ := setupTestCoordinator(t, 9)
	ctx := context.Background()

	// Create epoch
	var blockHash [32]byte
	copy(blockHash[:], "test-block-hash")
	coordinator.TransitionEpoch(ctx, 100, blockHash)

	// Generate assignment proof
	var accountID [32]byte
	copy(accountID[:], "test-account")

	proof, err := coordinator.GenerateAssignmentProof(accountID)
	if err != nil {
		t.Fatalf("failed to generate assignment proof: %v", err)
	}

	if proof.AccountID != accountID {
		t.Errorf("proof has wrong account ID")
	}

	if len(proof.NodeIDs) < 3 {
		t.Errorf("proof has %d nodes, expected >= 3", len(proof.NodeIDs))
	}

	if proof.AssignmentRoot == [32]byte{} {
		t.Errorf("proof has empty assignment root")
	}

	// Verify the proof
	if !coordinator.VerifyAssignmentProof(proof) {
		t.Errorf("valid proof failed verification")
	}
}

func TestCoordinatorNoActiveNodes(t *testing.T) {
	db := memdb.New()
	config := servicenodevm.DefaultConfig()
	registry := servicenodevm.NewRegistry(db, config)
	ctx := context.Background()
	registry.Load(ctx)

	coordinator := NewCoordinator(db, registry, config)

	// Try to transition epoch with no nodes
	var blockHash [32]byte
	epoch, err := coordinator.TransitionEpoch(ctx, 100, blockHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if epoch.SwarmCount != 0 {
		t.Errorf("expected 0 swarms with no nodes, got %d", epoch.SwarmCount)
	}

	// Try to get swarm for account
	var accountID [32]byte
	_, err = coordinator.GetSwarmForAccount(accountID)
	if err != servicenodevm.ErrSwarmNotFound {
		t.Errorf("expected ErrSwarmNotFound, got %v", err)
	}
}

func TestCoordinatorEpochTransition(t *testing.T) {
	coordinator, _ := setupTestCoordinator(t, 9)
	ctx := context.Background()

	// First epoch
	var blockHash1 [32]byte
	copy(blockHash1[:], "block-hash-1")
	epoch1, _ := coordinator.TransitionEpoch(ctx, 100, blockHash1)

	// Second epoch
	var blockHash2 [32]byte
	copy(blockHash2[:], "block-hash-2")
	epoch2, _ := coordinator.TransitionEpoch(ctx, 200, blockHash2)

	if epoch2.ID != epoch1.ID+1 {
		t.Errorf("expected epoch ID %d, got %d", epoch1.ID+1, epoch2.ID)
	}

	if epoch2.StartHeight != 200 {
		t.Errorf("expected start height 200, got %d", epoch2.StartHeight)
	}

	// Verify randomness is different
	if epoch2.RandomnessSource == epoch1.RandomnessSource {
		t.Errorf("randomness should be different between epochs")
	}
}

func TestCoordinatorSwarmNodeDistribution(t *testing.T) {
	coordinator, _ := setupTestCoordinator(t, 15)
	ctx := context.Background()

	var blockHash [32]byte
	copy(blockHash[:], "test-hash")
	epoch, _ := coordinator.TransitionEpoch(ctx, 100, blockHash)

	// With 15 nodes and 3 per swarm, expect 5 swarms
	if epoch.SwarmCount != 5 {
		t.Errorf("expected 5 swarms, got %d", epoch.SwarmCount)
	}

	// Verify each swarm has nodes
	totalNodes := 0
	for swarmID := uint64(0); swarmID < uint64(epoch.SwarmCount); swarmID++ {
		nodes, err := coordinator.GetSwarmNodes(swarmID)
		if err != nil {
			t.Fatalf("failed to get nodes for swarm %d: %v", swarmID, err)
		}
		if len(nodes) == 0 {
			t.Errorf("swarm %d has no nodes", swarmID)
		}
		totalNodes += len(nodes)
	}

	if totalNodes != 15 {
		t.Errorf("expected 15 total nodes across swarms, got %d", totalNodes)
	}
}
