// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/log"

	"github.com/luxfi/node/tests/fixture/tmpnet"
)

func TestSimpleNetworkStart(t *testing.T) {
	// Check if LUXD_PATH is set
	luxdPath := os.Getenv("LUXD_PATH")
	if luxdPath == "" {
		t.Skip("LUXD_PATH environment variable not set - skipping simple network test")
	}

	// Verify the binary exists
	if _, err := os.Stat(luxdPath); err != nil {
		t.Skipf("LUXD_PATH binary not found at %s - skipping simple network test", luxdPath)
	}
	
	// Create logger
	logger := log.New()
	
	// Create a minimal network configuration
	network := &tmpnet.Network{
		Owner: "test-simple",
		DefaultFlags: tmpnet.FlagsMap{
			"min-stake-duration": "1s",
		},
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
		Nodes: tmpnet.NewNodesOrPanic(1),
	}

	// Start the network
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Bootstrap the network
	err := tmpnet.BootstrapNewNetwork(ctx, logger, network, "")
	require.NoError(t, err)

	// Clean up
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = network.Stop(stopCtx)
	}()

	// Verify the network is healthy
	for _, node := range network.Nodes {
		healthy, err := node.IsHealthy(ctx)
		require.NoError(t, err)
		require.True(t, healthy, "Node %s should be healthy", node.NodeID)
	}

	t.Log("Network started successfully and all nodes are healthy")
}