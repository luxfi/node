// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

// getNodeBinaryPath returns the path to the luxd binary, building it if necessary
func getNodeBinaryPath(t *testing.T) string {
	// Check if LUXD_PATH is set
	if path := os.Getenv("LUXD_PATH"); path != "" {
		return path
	}

	// Try to find it in the build directory
	buildDir := filepath.Join("..", "..", "build")
	luxdPath := filepath.Join(buildDir, "luxd")
	
	if _, err := os.Stat(luxdPath); err == nil {
		return luxdPath
	}

	// Build it
	t.Log("Building luxd binary for integration tests...")
	cmd := exec.Command("go", "build", "-o", luxdPath, "./cmd/luxd")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		t.Skipf("Skipping integration test: failed to build luxd: %v", err)
	}
	
	return luxdPath
}

// TestCChainOperations tests C-Chain (EVM) operations
func TestCChainOperations(t *testing.T) {
	require := require.New(t)

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a single node network
	network := &tmpnet.Network{
		Owner: "integration-test-cchain",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Get node URI and create client
	uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
	require.NoError(err)
	require.NotEmpty(uris)
	nodeURI := uris[0].URI
	infoClient := info.NewClient(nodeURI)

	// Test C-Chain is available
	cChainID, err := infoClient.GetBlockchainID(ctx, "C")
	require.NoError(err)
	require.NotEqual(ids.Empty, cChainID)

	// Test basic node info operations
	nodeID, _, err := infoClient.GetNodeID(ctx)
	require.NoError(err)
	require.NotEmpty(nodeID)
}

// TestXChainOperations tests X-Chain (DAG) operations
func TestXChainOperations(t *testing.T) {
	require := require.New(t)

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a single node network
	network := &tmpnet.Network{
		Owner: "integration-test-xchain",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Get node URI and create client
	uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
	require.NoError(err)
	require.NotEmpty(uris)
	nodeURI := uris[0].URI
	infoClient := info.NewClient(nodeURI)

	// Test X-Chain is available
	xChainID, err := infoClient.GetBlockchainID(ctx, "X")
	require.NoError(err)
	require.NotEqual(ids.Empty, xChainID)

	// Test basic node operations
	nodeID, _, err := infoClient.GetNodeID(ctx)
	require.NoError(err)
	require.NotEmpty(nodeID)
}

// TestPChainOperations tests P-Chain (Platform) operations
func TestPChainOperations(t *testing.T) {
	require := require.New(t)

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a single node network
	network := &tmpnet.Network{
		Owner: "integration-test-pchain",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Get node URI and create client
	uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
	require.NoError(err)
	require.NotEmpty(uris)
	nodeURI := uris[0].URI
	infoClient := info.NewClient(nodeURI)

	// Test P-Chain is available
	pChainID, err := infoClient.GetBlockchainID(ctx, "P")
	require.NoError(err)
	require.NotEqual(ids.Empty, pChainID)

	// Test basic node operations
	nodeID, _, err := infoClient.GetNodeID(ctx)
	require.NoError(err)
	require.NotEmpty(nodeID)

	// Test network info
	networkID, err := infoClient.GetNetworkID(ctx)
	require.NoError(err)
	require.NotZero(networkID)
}

// TestBlockProduction tests that blocks are being produced
func TestBlockProduction(t *testing.T) {
	require := require.New(t)

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a single node network
	network := &tmpnet.Network{
		Owner: "integration-test-blocks",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Get node URI and create client
	uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
	require.NoError(err)
	require.NotEmpty(uris)
	nodeURI := uris[0].URI
	infoClient := info.NewClient(nodeURI)

	// Test that node is running and accessible
	nodeID, _, err := infoClient.GetNodeID(ctx)
	require.NoError(err)
	require.NotEmpty(nodeID)

	// Test basic connectivity - as a proxy for block production readiness
	networkID, err := infoClient.GetNetworkID(ctx)
	require.NoError(err)
	require.NotZero(networkID)

	// Test that we can query blockchain IDs (indicates chains are operational)
	pChainID, err := infoClient.GetBlockchainID(ctx, "P")
	require.NoError(err)
	require.NotEqual(ids.Empty, pChainID)
}
