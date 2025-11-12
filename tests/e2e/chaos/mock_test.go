// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chaos_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/tests/e2e/chaos"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

// TestChaosInjectorBasic tests basic chaos injector functionality
func TestChaosInjectorBasic(t *testing.T) {
	require := require.New(t)
	
	// Create mock network
	network := &tmpnet.Network{
		Nodes: []*tmpnet.Node{
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
		},
	}
	
	logger := log.NewNoOpLogger()
	
	// Create chaos injector
	injector := chaos.NewChaosInjector(network, logger)
	defer injector.Stop()
	
	// Test that injector can be created
	require.NotNil(injector)
	
	// Test getting active faults (should be empty)
	faults := injector.GetActiveFaults()
	require.Empty(faults)
	
	t.Log("Chaos injector basic test passed")
}

// TestNetworkPartitionerCreation tests network partitioner creation
func TestNetworkPartitionerCreation(t *testing.T) {
	require := require.New(t)
	
	// Create mock network
	network := &tmpnet.Network{
		Nodes: []*tmpnet.Node{
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
		},
	}
	
	logger := log.NewNoOpLogger()
	injector := chaos.NewChaosInjector(network, logger)
	defer injector.Stop()
	
	// Create network partitioner
	partitioner := chaos.NewNetworkPartitioner(network, logger, injector)
	require.NotNil(partitioner)
	
	// Test getting active partitions (should be empty)
	partitions := partitioner.GetActivePartitions()
	require.Empty(partitions)
	
	t.Log("Network partitioner creation test passed")
}

// TestByzantineInjectorCreation tests Byzantine injector creation
func TestByzantineInjectorCreation(t *testing.T) {
	require := require.New(t)
	
	// Create mock network
	network := &tmpnet.Network{
		Nodes: []*tmpnet.Node{
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
		},
	}
	
	logger := log.NewNoOpLogger()
	injector := chaos.NewChaosInjector(network, logger)
	defer injector.Stop()
	
	// Create Byzantine injector
	byzantineInjector := chaos.NewByzantineInjector(network, logger, injector)
	require.NotNil(byzantineInjector)
	
	// Test getting active Byzantine faults (should be empty)
	faults := byzantineInjector.GetActiveByzantineFaults()
	require.Empty(faults)
	
	t.Log("Byzantine injector creation test passed")
}

// TestRecoveryVerifierCreation tests recovery verifier creation
func TestRecoveryVerifierCreation(t *testing.T) {
	require := require.New(t)
	
	// Create mock network
	network := &tmpnet.Network{
		Nodes: []*tmpnet.Node{
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
		},
	}
	
	logger := log.NewNoOpLogger()
	
	// Create recovery verifier
	verifier := chaos.NewRecoveryVerifier(network, logger)
	require.NotNil(verifier)
	
	t.Log("Recovery verifier creation test passed")
}

// TestFaultConfigValidation tests fault configuration validation
func TestFaultConfigValidation(t *testing.T) {
	require := require.New(t)
	
	// Create mock network
	network := &tmpnet.Network{
		Nodes: []*tmpnet.Node{
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
		},
	}
	
	logger := log.NewNoOpLogger()
	injector := chaos.NewChaosInjector(network, logger)
	defer injector.Stop()
	
	ctx := context.Background()
	
	// Test with valid config
	validConfig := chaos.FaultConfig{
		Type:        chaos.FaultTypeCrash,
		TargetNodes: []*tmpnet.Node{network.Nodes[0]},
		Duration:    10 * time.Second,
		Probability: 0.5,
	}
	
	// This should not return an error (but might not do anything without real nodes)
	err := injector.InjectFault(ctx, validConfig)
	require.NoError(err)
	
	t.Log("Fault config validation test passed")
}

// TestByzantineConfigValidation tests Byzantine configuration validation
func TestByzantineConfigValidation(t *testing.T) {
	require := require.New(t)
	
	// Create mock network
	network := &tmpnet.Network{
		Nodes: []*tmpnet.Node{
			{NodeID: ids.GenerateTestNodeID()},
			{NodeID: ids.GenerateTestNodeID()},
		},
	}
	
	logger := log.NewNoOpLogger()
	injector := chaos.NewChaosInjector(network, logger)
	defer injector.Stop()
	
	byzantineInjector := chaos.NewByzantineInjector(network, logger, injector)
	
	ctx := context.Background()
	
	// Test with invalid config (no target nodes)
	invalidConfig := chaos.ByzantineConfig{
		Behavior:    chaos.ByzantineEquivocation,
		TargetNodes: []ids.NodeID{},
		Duration:    10 * time.Second,
		Intensity:   0.5,
	}
	
	err := byzantineInjector.InjectByzantineBehavior(ctx, invalidConfig)
	require.Error(err)
	require.Contains(err.Error(), "no target nodes")
	
	// Test with invalid intensity
	invalidConfig2 := chaos.ByzantineConfig{
		Behavior:    chaos.ByzantineEquivocation,
		TargetNodes: []ids.NodeID{network.Nodes[0].NodeID},
		Duration:    10 * time.Second,
		Intensity:   1.5, // Invalid: > 1.0
	}
	
	err = byzantineInjector.InjectByzantineBehavior(ctx, invalidConfig2)
	require.Error(err)
	require.Contains(err.Error(), "intensity must be between 0 and 1")
	
	t.Log("Byzantine config validation test passed")
}

// TestSnapshotComparison tests health snapshot comparison
func TestSnapshotComparison(t *testing.T) {
	require := require.New(t)
	
	// Create test snapshots
	snapshot1 := &chaos.NetworkHealthSnapshot{
		Timestamp:    time.Now(),
		HealthyNodes: 5,
		TotalNodes:   5,
		Connectivity: map[ids.NodeID]int{
			ids.GenerateTestNodeID(): 4,
			ids.GenerateTestNodeID(): 4,
		},
	}
	
	snapshot2 := &chaos.NetworkHealthSnapshot{
		Timestamp:    time.Now().Add(1 * time.Minute),
		HealthyNodes: 5,
		TotalNodes:   5,
		Connectivity: map[ids.NodeID]int{
			ids.GenerateTestNodeID(): 4,
			ids.GenerateTestNodeID(): 4,
		},
	}
	
	// Test comparison - should be recovered
	recovered := chaos.CompareSnapshots(snapshot1, snapshot2)
	require.True(recovered)
	
	// Test with degraded health
	snapshot3 := &chaos.NetworkHealthSnapshot{
		Timestamp:    time.Now().Add(2 * time.Minute),
		HealthyNodes: 3, // Degraded
		TotalNodes:   5,
		Connectivity: map[ids.NodeID]int{
			ids.GenerateTestNodeID(): 2,
		},
	}
	
	recovered = chaos.CompareSnapshots(snapshot1, snapshot3)
	require.False(recovered)
	
	t.Log("Snapshot comparison test passed")
}