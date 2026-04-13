// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package servicenodevm

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
)

func TestRegistryRegister(t *testing.T) {
	db := memdb.New()
	config := DefaultConfig()
	registry := NewRegistry(db, config)

	ctx := context.Background()
	if err := registry.Load(ctx); err != nil {
		t.Fatalf("failed to load registry: %v", err)
	}

	// Create registration transaction
	nodeID := ids.GenerateTestNodeID()
	publicKey := make([]byte, 32)
	copy(publicKey, "test-public-key-12345")

	tx := &RegistrationTx{
		NodeID:       nodeID,
		PublicKey:    publicKey,
		StakeAmount:  config.MinStake,
		StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
	}

	// Register node
	node, err := registry.Register(ctx, tx)
	if err != nil {
		t.Fatalf("failed to register node: %v", err)
	}

	if node.NodeID != nodeID {
		t.Errorf("expected nodeID %v, got %v", nodeID, node.NodeID)
	}

	if node.State != StateRegistered {
		t.Errorf("expected state %s, got %s", StateRegistered, node.State)
	}

	if node.StakeAmount != config.MinStake {
		t.Errorf("expected stake %d, got %d", config.MinStake, node.StakeAmount)
	}

	// Verify node can be retrieved
	retrieved, err := registry.Get(nodeID)
	if err != nil {
		t.Fatalf("failed to get node: %v", err)
	}

	if retrieved.ID != node.ID {
		t.Errorf("expected node ID %v, got %v", node.ID, retrieved.ID)
	}
}

func TestRegistryInsufficientStake(t *testing.T) {
	db := memdb.New()
	config := DefaultConfig()
	registry := NewRegistry(db, config)

	ctx := context.Background()
	registry.Load(ctx)

	tx := &RegistrationTx{
		NodeID:       ids.GenerateTestNodeID(),
		PublicKey:    []byte("test-key"),
		StakeAmount:  config.MinStake - 1, // Below minimum
		StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
	}

	_, err := registry.Register(ctx, tx)
	if err != ErrInsufficientStake {
		t.Errorf("expected ErrInsufficientStake, got %v", err)
	}
}

func TestRegistryActivateDeactivate(t *testing.T) {
	db := memdb.New()
	config := DefaultConfig()
	registry := NewRegistry(db, config)

	ctx := context.Background()
	registry.Load(ctx)

	// Register node
	nodeID := ids.GenerateTestNodeID()
	tx := &RegistrationTx{
		NodeID:       nodeID,
		PublicKey:    []byte("test-key"),
		StakeAmount:  config.MinStake,
		StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
	}

	registry.Register(ctx, tx)

	// Activate node
	if err := registry.Activate(ctx, nodeID); err != nil {
		t.Fatalf("failed to activate node: %v", err)
	}

	node, _ := registry.Get(nodeID)
	if node.State != StateActive {
		t.Errorf("expected state %s, got %s", StateActive, node.State)
	}

	// Verify active nodes count
	activeNodes := registry.GetActiveNodes()
	if len(activeNodes) != 1 {
		t.Errorf("expected 1 active node, got %d", len(activeNodes))
	}

	// Deactivate node
	if err := registry.Deactivate(ctx, nodeID); err != nil {
		t.Fatalf("failed to deactivate node: %v", err)
	}

	node, _ = registry.Get(nodeID)
	if node.State != StateExiting {
		t.Errorf("expected state %s, got %s", StateExiting, node.State)
	}
}

func TestRegistryJail(t *testing.T) {
	db := memdb.New()
	config := DefaultConfig()
	registry := NewRegistry(db, config)

	ctx := context.Background()
	registry.Load(ctx)

	// Register and activate node
	nodeID := ids.GenerateTestNodeID()
	tx := &RegistrationTx{
		NodeID:       nodeID,
		PublicKey:    []byte("test-key"),
		StakeAmount:  config.MinStake,
		StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
	}

	registry.Register(ctx, tx)
	registry.Activate(ctx, nodeID)

	// Jail node
	if err := registry.Jail(ctx, nodeID, "test reason"); err != nil {
		t.Fatalf("failed to jail node: %v", err)
	}

	node, _ := registry.Get(nodeID)
	if node.State != StateJailed {
		t.Errorf("expected state %s, got %s", StateJailed, node.State)
	}

	if node.JailReason != "test reason" {
		t.Errorf("expected jail reason 'test reason', got %s", node.JailReason)
	}

	// Verify node removed from active list
	activeNodes := registry.GetActiveNodes()
	if len(activeNodes) != 0 {
		t.Errorf("expected 0 active nodes, got %d", len(activeNodes))
	}
}

func TestRegistrySlash(t *testing.T) {
	db := memdb.New()
	config := DefaultConfig()
	registry := NewRegistry(db, config)

	ctx := context.Background()
	registry.Load(ctx)

	// Register and activate node
	nodeID := ids.GenerateTestNodeID()
	initialStake := config.MinStake * 2 // Extra stake
	tx := &RegistrationTx{
		NodeID:       nodeID,
		PublicKey:    []byte("test-key"),
		StakeAmount:  initialStake,
		StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
	}

	registry.Register(ctx, tx)
	registry.Activate(ctx, nodeID)

	// Slash node
	slashAmount := config.MinStake / 2
	if err := registry.Slash(ctx, nodeID, slashAmount); err != nil {
		t.Fatalf("failed to slash node: %v", err)
	}

	node, _ := registry.Get(nodeID)
	expectedStake := initialStake - slashAmount
	if node.StakeAmount != expectedStake {
		t.Errorf("expected stake %d, got %d", expectedStake, node.StakeAmount)
	}

	// Node should still be active (above minimum)
	if node.State != StateActive {
		t.Errorf("expected state %s, got %s", StateActive, node.State)
	}
}

func TestRegistrySlashBelowMinimum(t *testing.T) {
	db := memdb.New()
	config := DefaultConfig()
	registry := NewRegistry(db, config)

	ctx := context.Background()
	registry.Load(ctx)

	// Register and activate node with minimum stake
	nodeID := ids.GenerateTestNodeID()
	tx := &RegistrationTx{
		NodeID:       nodeID,
		PublicKey:    []byte("test-key"),
		StakeAmount:  config.MinStake,
		StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
	}

	registry.Register(ctx, tx)
	registry.Activate(ctx, nodeID)

	// Slash node below minimum
	if err := registry.Slash(ctx, nodeID, config.MinStake/2); err != nil {
		t.Fatalf("failed to slash node: %v", err)
	}

	node, _ := registry.Get(nodeID)
	// Node should be jailed due to insufficient stake
	if node.State != StateJailed {
		t.Errorf("expected state %s, got %s", StateJailed, node.State)
	}
}

func TestRegistryUptimeTracking(t *testing.T) {
	db := memdb.New()
	config := DefaultConfig()
	registry := NewRegistry(db, config)

	ctx := context.Background()
	registry.Load(ctx)

	// Register and activate node
	nodeID := ids.GenerateTestNodeID()
	tx := &RegistrationTx{
		NodeID:       nodeID,
		PublicKey:    []byte("test-key"),
		StakeAmount:  config.MinStake,
		StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
	}

	registry.Register(ctx, tx)
	registry.Activate(ctx, nodeID)

	// Record passed challenges
	for i := 0; i < 5; i++ {
		registry.UpdateUptime(ctx, nodeID, true)
	}

	node, _ := registry.Get(nodeID)
	if node.ChallengesPassed != 5 {
		t.Errorf("expected 5 passed challenges, got %d", node.ChallengesPassed)
	}

	// Record failed challenges (but not enough to jail)
	registry.UpdateUptime(ctx, nodeID, false)
	registry.UpdateUptime(ctx, nodeID, false)

	node, _ = registry.Get(nodeID)
	if node.ChallengesFailed != 2 {
		t.Errorf("expected 2 failed challenges, got %d", node.ChallengesFailed)
	}

	// Still active
	if node.State != StateActive {
		t.Errorf("expected state %s, got %s", StateActive, node.State)
	}
}

func TestRegistryRewards(t *testing.T) {
	db := memdb.New()
	config := DefaultConfig()
	registry := NewRegistry(db, config)

	ctx := context.Background()
	registry.Load(ctx)

	// Register and activate node
	nodeID := ids.GenerateTestNodeID()
	tx := &RegistrationTx{
		NodeID:       nodeID,
		PublicKey:    []byte("test-key"),
		StakeAmount:  config.MinStake,
		StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
	}

	registry.Register(ctx, tx)
	registry.Activate(ctx, nodeID)

	// Add rewards
	registry.AddReward(ctx, nodeID, 100)
	registry.AddReward(ctx, nodeID, 200)

	node, _ := registry.Get(nodeID)
	if node.PendingRewards != 300 {
		t.Errorf("expected 300 pending rewards, got %d", node.PendingRewards)
	}

	// Claim rewards
	claimed, err := registry.ClaimRewards(ctx, nodeID)
	if err != nil {
		t.Fatalf("failed to claim rewards: %v", err)
	}

	if claimed != 300 {
		t.Errorf("expected to claim 300, got %d", claimed)
	}

	node, _ = registry.Get(nodeID)
	if node.PendingRewards != 0 {
		t.Errorf("expected 0 pending rewards after claim, got %d", node.PendingRewards)
	}

	if node.TotalRewards != 300 {
		t.Errorf("expected 300 total rewards, got %d", node.TotalRewards)
	}
}

func TestRegistryComputeRoot(t *testing.T) {
	db := memdb.New()
	config := DefaultConfig()
	registry := NewRegistry(db, config)

	ctx := context.Background()
	registry.Load(ctx)

	// Empty registry should have zero root
	root := registry.ComputeRegistryRoot()
	if root != [32]byte{} {
		t.Errorf("expected empty root for empty registry")
	}

	// Register and activate nodes
	for i := 0; i < 3; i++ {
		nodeID := ids.GenerateTestNodeID()
		tx := &RegistrationTx{
			NodeID:       nodeID,
			PublicKey:    []byte("test-key"),
			StakeAmount:  config.MinStake,
			StakeLockEnd: uint64(time.Now().Add(7 * 24 * time.Hour).Unix()),
		}
		registry.Register(ctx, tx)
		registry.Activate(ctx, nodeID)
	}

	// Root should be non-zero
	root = registry.ComputeRegistryRoot()
	if root == [32]byte{} {
		t.Errorf("expected non-empty root for non-empty registry")
	}

	// Root should be deterministic
	root2 := registry.ComputeRegistryRoot()
	if root != root2 {
		t.Errorf("registry root not deterministic")
	}
}
