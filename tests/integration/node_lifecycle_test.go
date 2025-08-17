// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/utils/logging"
)

// TestNodeLifecycle tests the complete lifecycle of a node:
// start -> bootstrap -> healthy -> shutdown
func TestNodeLifecycle(t *testing.T) {
	require := require.New(t)
	
	// Create a test network configuration
	network := &tmpnet.Network{
		Owner: "integration-test",
	}
	
	// Create a single node for testing
	node := &tmpnet.Node{
		NodeID:      tmpnet.GenerateNodeID(),
		IsEphemeral: false,
		RuntimeConfig: &tmpnet.ProcessRuntimeConfig{
			GlobalNodeConfig: tmpnet.GlobalNodeConfig{
				NetworkID: 96369, // LUX mainnet ID
				LogLevel:  logging.Info.String(),
			},
		},
	}
	
	network.Nodes = []*tmpnet.Node{node}
	
	// Test node startup
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	t.Log("Starting node...")
	err := node.Start(ctxWithTimeout)
	require.NoError(err, "Failed to start node")
	
	// Ensure node stops on test completion
	defer func() {
		t.Log("Stopping node...")
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = node.Stop(stopCtx)
	}()
	
	// Wait for node to become healthy
	t.Log("Waiting for node to become healthy...")
	err = tmpnet.WaitForHealthy(ctxWithTimeout, node)
	require.NoError(err, "Node failed to become healthy")
	
	// Verify node is actually healthy
	healthy, err := node.IsHealthy(ctx)
	require.NoError(err, "Failed to check node health")
	require.True(healthy, "Node reports unhealthy after WaitForHealthy succeeded")
	
	// Test node info retrieval
	t.Log("Retrieving node info...")
	info, err := node.GetAPIClient().InfoAPI()
	require.NoError(err, "Failed to get info API client")
	
	nodeID, err := info.GetNodeID(ctx)
	require.NoError(err, "Failed to get node ID")
	require.Equal(node.NodeID.String(), nodeID.String(), "Node ID mismatch")
	
	// Test blockchain ID retrieval
	blockchainID, err := info.GetBlockchainID(ctx, "C")
	require.NoError(err, "Failed to get C-Chain blockchain ID")
	require.NotEmpty(blockchainID, "Blockchain ID should not be empty")
	
	t.Logf("Node lifecycle test completed successfully. Node ID: %s, C-Chain ID: %s", 
		nodeID, blockchainID)
}

// TestMultiNodeBootstrap tests bootstrapping with multiple nodes
func TestMultiNodeBootstrap(t *testing.T) {
	require := require.New(t)
	
	// Create a test network with 3 nodes
	network := &tmpnet.Network{
		Owner: "multi-node-test",
	}
	
	nodeCount := 3
	nodes := make([]*tmpnet.Node, nodeCount)
	
	for i := 0; i < nodeCount; i++ {
		nodes[i] = &tmpnet.Node{
			NodeID:      tmpnet.GenerateNodeID(),
			IsEphemeral: false,
			RuntimeConfig: &tmpnet.ProcessRuntimeConfig{
				GlobalNodeConfig: tmpnet.GlobalNodeConfig{
					NetworkID: 96369,
					LogLevel:  logging.Info.String(),
				},
			},
		}
	}
	
	network.Nodes = nodes
	
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	
	// Start bootstrap node first
	t.Log("Starting bootstrap node...")
	err := nodes[0].Start(ctxWithTimeout)
	require.NoError(err, "Failed to start bootstrap node")
	
	defer func() {
		for _, node := range nodes {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = node.Stop(stopCtx)
			stopCancel()
		}
	}()
	
	// Wait for bootstrap node to be healthy
	err = tmpnet.WaitForHealthy(ctxWithTimeout, nodes[0])
	require.NoError(err, "Bootstrap node failed to become healthy")
	
	// Get bootstrap node's address for other nodes
	bootstrapIP := nodes[0].URI
	require.NotEmpty(bootstrapIP, "Bootstrap node URI is empty")
	
	// Start remaining nodes with bootstrap configuration
	for i := 1; i < nodeCount; i++ {
		t.Logf("Starting node %d with bootstrap IP: %s", i, bootstrapIP)
		
		if nodes[i].Flags == nil {
			nodes[i].Flags = make(map[string]string)
		}
		nodes[i].Flags["bootstrap-ips"] = bootstrapIP
		
		err := nodes[i].Start(ctxWithTimeout)
		require.NoError(err, "Failed to start node %d", i)
	}
	
	// Wait for all nodes to become healthy
	t.Log("Waiting for all nodes to become healthy...")
	for i, node := range nodes {
		err := tmpnet.WaitForHealthy(ctxWithTimeout, node)
		require.NoError(err, "Node %d failed to become healthy", i)
	}
	
	// Verify all nodes are connected
	t.Log("Verifying node connectivity...")
	for i, node := range nodes {
		healthy, err := node.IsHealthy(ctx)
		require.NoError(err, "Failed to check health for node %d", i)
		require.True(healthy, "Node %d is not healthy", i)
		
		// Get peer info to verify connectivity
		info, err := node.GetAPIClient().InfoAPI()
		require.NoError(err, "Failed to get info API for node %d", i)
		
		peers, err := info.Peers(ctx)
		require.NoError(err, "Failed to get peers for node %d", i)
		
		// Each node should have at least nodeCount-1 peers
		require.GreaterOrEqual(len(peers), nodeCount-1, 
			"Node %d has insufficient peers: %d", i, len(peers))
		
		t.Logf("Node %d has %d peers", i, len(peers))
	}
	
	t.Log("Multi-node bootstrap test completed successfully")
}

// TestNodeRestart tests node restart capability
func TestNodeRestart(t *testing.T) {
	require := require.New(t)
	
	node := &tmpnet.Node{
		NodeID:      tmpnet.GenerateNodeID(),
		IsEphemeral: false,
		RuntimeConfig: &tmpnet.ProcessRuntimeConfig{
			GlobalNodeConfig: tmpnet.GlobalNodeConfig{
				NetworkID: 96369,
				LogLevel:  logging.Info.String(),
			},
		},
	}
	
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	// Start node
	t.Log("Starting node for restart test...")
	err := node.Start(ctxWithTimeout)
	require.NoError(err, "Failed to start node")
	
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = node.Stop(stopCtx)
	}()
	
	// Wait for healthy state
	err = tmpnet.WaitForHealthy(ctxWithTimeout, node)
	require.NoError(err, "Node failed to become healthy")
	
	// Get initial node ID
	info, err := node.GetAPIClient().InfoAPI()
	require.NoError(err, "Failed to get info API")
	
	initialNodeID, err := info.GetNodeID(ctx)
	require.NoError(err, "Failed to get initial node ID")
	
	// Stop the node
	t.Log("Stopping node...")
	stopCtx, stopCancel := context.WithTimeout(ctx, 10*time.Second)
	err = node.Stop(stopCtx)
	stopCancel()
	require.NoError(err, "Failed to stop node")
	
	// Verify node is stopped
	healthy, err := node.IsHealthy(ctx)
	require.Error(err, "Node should return error when stopped")
	require.False(healthy, "Node should not be healthy when stopped")
	
	// Restart the node
	t.Log("Restarting node...")
	restartCtx, restartCancel := context.WithTimeout(ctx, 30*time.Second)
	defer restartCancel()
	
	err = node.Start(restartCtx)
	require.NoError(err, "Failed to restart node")
	
	// Wait for healthy state after restart
	err = tmpnet.WaitForHealthy(restartCtx, node)
	require.NoError(err, "Node failed to become healthy after restart")
	
	// Verify node ID is the same after restart
	info, err = node.GetAPIClient().InfoAPI()
	require.NoError(err, "Failed to get info API after restart")
	
	restartedNodeID, err := info.GetNodeID(ctx)
	require.NoError(err, "Failed to get node ID after restart")
	require.Equal(initialNodeID.String(), restartedNodeID.String(), 
		"Node ID changed after restart")
	
	t.Log("Node restart test completed successfully")
}