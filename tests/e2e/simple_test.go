// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/log"

	"github.com/luxfi/node/tests/fixture/tmpnet"
)

func TestSimpleNetworkStart(t *testing.T) {
	// Set the path to the binary
	luxdPath := "/Users/z/work/lux/node/build/luxd"
	
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Bootstrap the network
	err := tmpnet.BootstrapNewNetwork(ctx, logger, network, "")
	require.NoError(t, err)

	// Clean up
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
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