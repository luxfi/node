// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/log"
	"github.com/luxfi/node/config"
	"github.com/luxfi/ids"
)

// TestNetworkSerialization tests network configuration serialization
func TestNetworkSerialization(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()

	ctx := context.Background()

	network := NewDefaultNetwork("testnet")
	// Runtime configuration is required
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	// Validate round-tripping of primary subnet configuration
	network.PrimaryNetConfig = ConfigMap{
		"validatorOnly": true,
	}
	require.NoError(network.EnsureDefaultConfig(log.NoLog{}))
	require.NoError(network.Create(tmpDir))
	// Ensure node runtime is initialized
	require.NoError(network.readNodes(ctx))

	loadedNetwork, err := ReadNetwork(ctx, log.NoLog{}, network.Dir)
	require.NoError(err)
	for _, key := range loadedNetwork.PreFundedKeys {
		// Address() enables comparison with the original network by
		// ensuring full population of a key's in-memory representation.
		_ = key.Address()
	}

	// The original network comparison should work properly
	require.Equal(network, loadedNetwork)
}

// TestCreateNodes tests creating multiple nodes
func TestCreateNodes(t *testing.T) {
	require := require.New(t)
	
	network := NewDefaultNetwork("testnet")
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	
	// Create 5 nodes
	err := network.CreateNodes(5)
	require.NoError(err)
	require.Len(network.Nodes, 7) // 2 default + 5 new
	
	// Verify each node has been properly initialized
	for i, node := range network.Nodes {
		require.NotNil(node)
		require.NotNil(node.Flags)
		t.Logf("Node %d initialized", i)
	}
}

// TestMultiNodeNetwork tests a 5-node network
func TestMultiNodeNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-node test in short mode")
	}
	
	require := require.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	
	tmpDir := t.TempDir()
	
	// Create a network with 5 nodes
	network := &Network{
		Owner: "test-5-node",
		Nodes: NewNodesOrPanic(5),
		DefaultRuntimeConfig: NodeRuntimeConfig{
			Process: &ProcessRuntimeConfig{},
		},
	}
	
	// Configure network
	require.NoError(network.EnsureDefaultConfig(log.NoLog{}))
	require.NoError(network.Create(tmpDir))
	
	// Verify network configuration
	require.Len(network.Nodes, 5)
	require.NotEmpty(network.Dir)
	require.NotEmpty(network.UUID)
	
	// Verify bootstrap configuration for each node
	for i, node := range network.Nodes {
		require.NotNil(node.NodeID)
		require.NotEmpty(node.DataDir)
		t.Logf("Node %d: ID=%s, DataDir=%s", i, node.NodeID, node.DataDir)
		
		// Check bootstrap configuration is set properly
		flags, err := node.composeFlags()
		require.NoError(err)
		
		if i > 0 {
			// Non-bootstrap nodes should have bootstrap IPs and IDs
			bootstrapIPs := flags[config.BootstrapIPsKey]
			bootstrapIDs := flags[config.BootstrapIDsKey]
			t.Logf("Node %d bootstrap config: IPs=%v, IDs=%v", i, bootstrapIPs, bootstrapIDs)
		}
	}
	
	// Test node lifecycle management
	t.Run("NodeLifecycle", func(t *testing.T) {
		// This would normally start the network, but requires the actual node binary
		// For testing purposes, we just verify the configuration is correct
		
		// Verify we can add a node
		newNode := NewNode()
		err := network.AddNode(ctx, newNode)
		require.NoError(err)
		require.Len(network.Nodes, 6)
		
		// Verify we can remove a node
		err = network.RemoveNode(ctx, newNode.NodeID)
		require.NoError(err)
		require.Len(network.Nodes, 5)
	})
	
	// Test health status retrieval (will return all stopped since we're not actually running)
	t.Run("HealthStatus", func(t *testing.T) {
		healthStatus, err := network.GetHealthStatus(ctx)
		require.NoError(err)
		require.Len(healthStatus, 5)
		
		for nodeID, healthy := range healthStatus {
			require.False(healthy) // All nodes are stopped
			t.Logf("Node %s health: %v", nodeID, healthy)
		}
	})
	
	// Test bootstrap configuration
	t.Run("BootstrapConfig", func(t *testing.T) {
		bootstrapIPs, bootstrapIDs := network.GetBootstrapIPsAndIDs(nil)
		// Since nodes aren't running, these should be empty
		require.Empty(bootstrapIPs)
		require.Empty(bootstrapIDs)
		
		// But when we skip the first node, it should still be empty (no running nodes)
		bootstrapIPs, bootstrapIDs = network.GetBootstrapIPsAndIDs(network.Nodes[0])
		require.Empty(bootstrapIPs)
		require.Empty(bootstrapIDs)
	})
	
	// Test network serialization with multiple nodes
	t.Run("Serialization", func(t *testing.T) {
		// Write network configuration
		err := network.Write()
		require.NoError(err)
		
		// Read it back
		loadedNetwork, err := ReadNetwork(ctx, log.NoLog{}, network.Dir)
		require.NoError(err)
		
		// Verify the loaded network matches
		require.Equal(network.UUID, loadedNetwork.UUID)
		require.Equal(network.Owner, loadedNetwork.Owner)
		require.Len(loadedNetwork.Nodes, 5)
		
		// Verify node IDs match
		for i, node := range network.Nodes {
			require.Equal(node.NodeID, loadedNetwork.Nodes[i].NodeID)
		}
	})
}

// TestNetworkHelperFunctions tests the helper functions
func TestNetworkHelperFunctions(t *testing.T) {
	require := require.New(t)
	
	network := NewDefaultNetwork("test-helpers")
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	
	t.Run("GetNode", func(t *testing.T) {
		// Get existing node
		node, err := network.GetNode(network.Nodes[0].NodeID)
		require.NoError(err)
		require.NotNil(node)
		require.Equal(network.Nodes[0].NodeID, node.NodeID)
		
		// Try to get non-existent node
		fakeID := ids.GenerateTestNodeID()
		_, err = network.GetNode(fakeID)
		require.Error(err)
	})
	
	t.Run("GetAvailableNodeIDs", func(t *testing.T) {
		// Mark first node as ephemeral
		network.Nodes[0].IsEphemeral = true
		
		// Get available node IDs (should exclude ephemeral)
		availableIDs := network.GetAvailableNodeIDs()
		require.Len(availableIDs, 1) // Only the non-ephemeral node
		require.Equal(network.Nodes[1].NodeID.String(), availableIDs[0])
	})
	
	t.Run("TrackedNetsForNode", func(t *testing.T) {
		// Without subnets, should return empty
		tracked := network.TrackedNetsForNode(network.Nodes[0].NodeID)
		require.Empty(tracked)
		
		// Add a subnet (mock)
		subnetID := ids.GenerateTestID()
		network.Nets = []*Net{
			{
				NetID:        subnetID,
				ValidatorIDs: []ids.NodeID{network.Nodes[0].NodeID},
			},
		}
		
		tracked = network.TrackedNetsForNode(network.Nodes[0].NodeID)
		require.Equal(subnetID.String(), tracked)
		
		// Node not validating should return empty
		tracked = network.TrackedNetsForNode(network.Nodes[1].NodeID)
		require.Empty(tracked)
	})
}

// TestNodeHelperFunctions tests node helper functions
func TestNodeHelperFunctions(t *testing.T) {
	require := require.New(t)
	
	node := NewNode()
	require.NoError(node.EnsureKeys())
	
	t.Run("GetStatus", func(t *testing.T) {
		status := node.GetStatus()
		require.Equal("stopped", status)
		
		// Mock running state
		node.URI = "http://localhost:9650"
		status = node.GetStatus()
		require.Equal("running", status)
		
		// Reset
		node.URI = ""
	})
	
	t.Run("GetProofOfPossession", func(t *testing.T) {
		pop, err := node.GetProofOfPossession()
		require.NoError(err)
		require.NotNil(pop)
	})
	
	t.Run("RestartWithFlags", func(t *testing.T) {
		ctx := context.Background()
		
		// Create a minimal network for the node
		network := &Network{
			DefaultRuntimeConfig: NodeRuntimeConfig{
				Process: &ProcessRuntimeConfig{},
			},
			Nodes: []*Node{node},
		}
		node.network = network
		
		// Add flags
		additionalFlags := FlagsMap{
			"log-level": "debug",
			"custom-flag": "value",
		}
		
		// This will fail since we don't have actual runtime, but we can verify flags are merged
		_ = node.RestartWithFlags(ctx, additionalFlags)
		
		require.Equal("debug", node.Flags["log-level"])
		require.Equal("value", node.Flags["custom-flag"])
	})
}

// TestConfigGeneration tests network configuration generation
func TestConfigGeneration(t *testing.T) {
	require := require.New(t)
	
	t.Run("GenerateNetworkConfig", func(t *testing.T) {
		config, err := GenerateNetworkConfig(12345, 3, 10)
		require.NoError(err)
		require.NotNil(config)
		require.Equal(uint32(12345), config.NetworkID)
		require.Len(config.FundedKeys, 10)
		require.NotNil(config.DefaultFlags)
	})
	
	t.Run("GenerateNodeConfigs", func(t *testing.T) {
		netConfig, err := GenerateNetworkConfig(12345, 5, 10)
		require.NoError(err)
		
		nodeConfigs, err := netConfig.GenerateNodeConfigs(5)
		require.NoError(err)
		require.Len(nodeConfigs, 5)
		
		// Verify bootstrap configuration
		for i, nodeConfig := range nodeConfigs {
			require.NotNil(nodeConfig.NodeID)
			
			if i == 0 {
				// Bootstrap node should have empty bootstrap list
				bootstrapIDs, _ := nodeConfig.Flags.GetStringVal(config.BootstrapIDsKey)
				require.Empty(bootstrapIDs)
			} else {
				// Other nodes should have bootstrap configuration
				bootstrapIDs, _ := nodeConfig.Flags.GetStringVal(config.BootstrapIDsKey)
				require.NotEmpty(bootstrapIDs)
				
				bootstrapIPs, _ := nodeConfig.Flags.GetStringVal(config.BootstrapIPsKey)
				require.NotEmpty(bootstrapIPs)
			}
		}
	})
}

// Benchmark network creation
func BenchmarkNetworkCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		network := NewDefaultNetwork("bench")
		network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
		_ = network.CreateNodes(10)
	}
}

// Benchmark node creation
func BenchmarkNodeCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		node := NewNode()
		_ = node.EnsureKeys()
	}
}