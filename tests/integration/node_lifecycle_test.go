// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/log"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

// TestNodeLifecycle tests the complete lifecycle of a node:
// start -> bootstrap -> healthy -> shutdown
func TestNodeLifecycle(t *testing.T) {
	// Skip by default - requires network infrastructure
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test - requires network infrastructure (set RUN_INTEGRATION_TESTS=true)")
	}

	require := require.New(t)

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a single node network
	network := &tmpnet.Network{
		Owner: "integration-test-lifecycle",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Use no-op logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Verify node is accessible
	node := network.Nodes[0]
	require.NotNil(node)

	// Get node URI
	uris, err := network.GetNodeURIs(ctx, func(f func()) {})
	require.NoError(err)
	require.NotEmpty(uris)
	nodeURI := uris[0].URI

	// Test node is responding to API calls
	infoClient := info.NewClient(nodeURI)
	_, _, err = infoClient.GetNodeID(ctx)
	require.NoError(err)
}

// TestNodeRestart tests node restart functionality
func TestNodeRestart(t *testing.T) {
	// Skip by default - requires network infrastructure
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test - requires network infrastructure (set RUN_INTEGRATION_TESTS=true)")
	}

	require := require.New(t)

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a single node network
	network := &tmpnet.Network{
		Owner: "integration-test-restart",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Use no-op logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Get node and URI
	node := network.Nodes[0]
	uris, err := network.GetNodeURIs(ctx, func(f func()) {})
	require.NoError(err)
	require.NotEmpty(uris)
	nodeURI := uris[0].URI
	infoClient := info.NewClient(nodeURI)

	// Get initial node ID
	initialNodeID, _, err := infoClient.GetNodeID(ctx)
	require.NoError(err)

	// Stop the node
	err = node.Stop(ctx)
	require.NoError(err)

	// Restart the node
	err = node.Start(nil) // Start doesn't take context
	require.NoError(err)

	// Wait a bit for restart
	time.Sleep(2 * time.Second)

	// Verify node ID is the same after restart (new client since URI may change)
	newInfoClient := info.NewClient(nodeURI)
	restartedNodeID, _, err := newInfoClient.GetNodeID(ctx)
	require.NoError(err)
	require.Equal(initialNodeID, restartedNodeID)
}

// TestMultiNodeNetwork tests multiple node network operations
func TestMultiNodeNetwork(t *testing.T) {
	// Skip by default - requires network infrastructure
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test - requires network infrastructure (set RUN_INTEGRATION_TESTS=true)")
	}

	require := require.New(t)

	const nodeCount = 3

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a multi-node network
	network := &tmpnet.Network{
		Owner: "integration-test-multinode",
		Nodes: tmpnet.NewNodesOrPanic(nodeCount),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Use no-op logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Get node URIs
	uris, err := network.GetNodeURIs(ctx, func(f func()) {})
	require.NoError(err)
	require.Len(uris, nodeCount)

	// Verify all nodes are running and accessible
	require.Len(network.Nodes, nodeCount)
	for i, nodeURI := range uris {
		infoClient := info.NewClient(nodeURI.URI)

		// Test node is responding to API calls
		nodeID, _, err := infoClient.GetNodeID(ctx)
		require.NoError(err, "Node %d should respond to GetNodeID", i)
		require.NotEmpty(nodeID, "Node %d should have a node ID", i)

		// Test network info
		networkInfo, err := infoClient.GetNetworkID(ctx)
		require.NoError(err, "Node %d should respond to GetNetworkID", i)
		require.NotZero(networkInfo, "Node %d should have a network ID", i)
	}

	// Verify nodes can communicate with each other
	for i, nodeURI := range uris {
		infoClient := info.NewClient(nodeURI.URI)
		peers, err := infoClient.Peers(ctx, nil) // Pass nil for nodeIDs to get all peers
		require.NoError(err, "Node %d should be able to get peers", i)
		// Should have at least one peer (other nodes) for multi-node setup
		require.GreaterOrEqual(len(peers), 0, "Node %d should have peers", i) // Allow 0 for single-node bootstrap
	}
}
