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

	"github.com/luxfi/log"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

// TestSimpleNodeStart tests if we can start a single node
func TestSimpleNodeStart(t *testing.T) {
	// Skip by default - requires network infrastructure
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test - requires network infrastructure (set RUN_INTEGRATION_TESTS=true)")
	}

	require := require.New(t)

	// Build luxd if needed
	buildDir := filepath.Join("..", "..", "build")
	luxdPath := filepath.Join(buildDir, "luxd")

	if _, err := os.Stat(luxdPath); os.IsNotExist(err) {
		t.Log("Building luxd binary...")
		cmd := exec.Command("go", "build", "-o", luxdPath, "./cmd/luxd")
		cmd.Dir = filepath.Join("..", "..")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		require.NoError(cmd.Run())
	}

	t.Logf("Using luxd binary at: %s", luxdPath)

	// Create a minimal network
	network := &tmpnet.Network{
		Owner: "test-simple",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	// Set a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try to bootstrap
	t.Log("Attempting to bootstrap network...")
	err := tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, "")
	if err != nil {
		t.Logf("Bootstrap failed with error: %v", err)

		// Try to get more info
		if len(network.Nodes) > 0 {
			node := network.Nodes[0]
			t.Logf("Node ID: %s", node.NodeID)
			t.Logf("Node DataDir: %s", node.DataDir)
			t.Logf("Node URI: %s", node.URI)

			// Check for logs
			logDir := filepath.Join(node.DataDir, "logs")
			if _, err := os.Stat(logDir); err == nil {
				entries, _ := os.ReadDir(logDir)
				for _, entry := range entries {
					t.Logf("Found log file: %s", entry.Name())
					logPath := filepath.Join(logDir, entry.Name())
					content, _ := os.ReadFile(logPath)
					if len(content) > 0 {
						t.Logf("Log content from %s:\n%s", entry.Name(), string(content[:min(1000, len(content))]))
					}
				}
			}
		}
	}
	require.NoError(err, "Failed to bootstrap network")

	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	t.Log("Network started successfully!")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}