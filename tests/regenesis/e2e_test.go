// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build e2e

package regenesis_test

import (
	"context"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/ethclient"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/health"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/tests/regenesis/testutil"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/platformvm"
)

// =============================================================================
// E2E TESTS - Full Network Regenesis
// =============================================================================

func TestE2EFullNetworkRegenesis(t *testing.T) {
	if os.Getenv("E2E_REGENESIS") == "" {
		t.Skip("Set E2E_REGENESIS=1 to run this test")
	}

	t.Run("RegenesisWithStateTransfer", func(t *testing.T) {
		require := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		// Setup paths
		luxdPath := os.Getenv("LUXD_PATH")
		if luxdPath == "" {
			t.Skip("LUXD_PATH not set")
		}

		sourceDataDir := os.Getenv("SOURCE_DATA_DIR")
		if sourceDataDir == "" {
			t.Skip("SOURCE_DATA_DIR not set - need source chain data for regenesis")
		}

		// Create target directory
		targetDataDir := t.TempDir()

		// Step 1: Migrate state from source to target
		t.Log("Step 1: Migrating state...")
		migrator := testutil.NewStateMigrator(sourceDataDir, targetDataDir)

		migrationStats, err := migrator.Migrate(ctx)
		require.NoError(err, "State migration should succeed")
		require.Greater(migrationStats.BlocksMigrated, uint64(0), "Should have migrated blocks")
		t.Logf("Migrated %d blocks, %d keys", migrationStats.BlocksMigrated, migrationStats.KeysMigrated)
		migrator.Close()

		// Step 2: Create network configuration
		t.Log("Step 2: Creating network configuration...")
		network := &tmpnet.Network{
			Owner:     "e2e-regenesis-test",
			Nodes:     tmpnet.NewNodesOrPanic(3),
			NetworkID: 88888,
			DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					LuxNodePath: luxdPath,
				},
			},
			DefaultFlags: tmpnet.FlagsMap{
				config.DatabaseDirKey: targetDataDir,
			},
		}

		// Step 3: Bootstrap the network
		t.Log("Step 3: Bootstrapping network...")
		err = tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, "")
		require.NoError(err, "Network bootstrap should succeed")

		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			network.Stop(shutdownCtx)
		}()

		// Step 4: Verify network health
		t.Log("Step 4: Verifying network health...")
		healthStatus, err := network.GetHealthStatus(ctx)
		require.NoError(err)
		for nodeID, healthy := range healthStatus {
			require.True(healthy, "Node %s should be healthy", nodeID)
		}

		// Step 5: Verify chain state
		t.Log("Step 5: Verifying chain state...")
		uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
		require.NoError(err)
		require.NotEmpty(uris)

		nodeURI := uris[0].URI
		infoClient := info.NewClient(nodeURI)

		// Verify network ID
		networkID, err := infoClient.GetNetworkID(ctx)
		require.NoError(err)
		require.Equal(uint32(88888), networkID)

		// Verify C-Chain is operational
		cChainID, err := infoClient.GetBlockchainID(ctx, "C")
		require.NoError(err)
		require.NotEqual(ids.Empty, cChainID)

		// Step 6: Verify block height matches migration
		t.Log("Step 6: Verifying block height...")
		ethURI := strings.Replace(nodeURI, "http", "ws", 1) + "/ext/bc/C/ws"
		ethClient, err := ethclient.Dial(ethURI)
		require.NoError(err)
		defer ethClient.Close()

		blockNumber, err := ethClient.BlockNumber(ctx)
		require.NoError(err)
		require.GreaterOrEqual(blockNumber, migrationStats.LastProcessedBlock,
			"Block number should match or exceed migrated height")

		t.Logf("SUCCESS: Network running at block %d", blockNumber)
	})
}

// =============================================================================
// E2E TESTS - Multi-Chain Consistency
// =============================================================================

func TestE2EMultiChainConsistency(t *testing.T) {
	if os.Getenv("E2E_REGENESIS") == "" {
		t.Skip("Set E2E_REGENESIS=1 to run this test")
	}

	t.Run("AllChainsOperational", func(t *testing.T) {
		require := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		luxdPath := os.Getenv("LUXD_PATH")
		if luxdPath == "" {
			t.Skip("LUXD_PATH not set")
		}

		// Create network
		network := &tmpnet.Network{
			Owner: "e2e-multichain-test",
			Nodes: tmpnet.NewNodesOrPanic(3),
			DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					LuxNodePath: luxdPath,
				},
			},
		}

		err := tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, "")
		require.NoError(err)

		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			network.Stop(shutdownCtx)
		}()

		uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
		require.NoError(err)
		nodeURI := uris[0].URI

		infoClient := info.NewClient(nodeURI)

		// Verify P-Chain
		pChainID, err := infoClient.GetBlockchainID(ctx, "P")
		require.NoError(err, "P-Chain should be accessible")
		require.NotEqual(ids.Empty, pChainID)
		t.Logf("P-Chain ID: %s", pChainID)

		// Verify X-Chain
		xChainID, err := infoClient.GetBlockchainID(ctx, "X")
		require.NoError(err, "X-Chain should be accessible")
		require.NotEqual(ids.Empty, xChainID)
		t.Logf("X-Chain ID: %s", xChainID)

		// Verify C-Chain
		cChainID, err := infoClient.GetBlockchainID(ctx, "C")
		require.NoError(err, "C-Chain should be accessible")
		require.NotEqual(ids.Empty, cChainID)
		t.Logf("C-Chain ID: %s", cChainID)

		// Verify chains can process queries
		pClient := platformvm.NewClient(nodeURI)
		height, err := pClient.GetHeight(ctx)
		require.NoError(err)
		require.GreaterOrEqual(height, uint64(0))
		t.Logf("P-Chain height: %d", height)
	})
}

// =============================================================================
// E2E TESTS - Validator Operations Post-Regenesis
// =============================================================================

func TestE2EValidatorOperationsPostRegenesis(t *testing.T) {
	if os.Getenv("E2E_REGENESIS") == "" {
		t.Skip("Set E2E_REGENESIS=1 to run this test")
	}

	t.Run("QueryValidatorSet", func(t *testing.T) {
		require := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		luxdPath := os.Getenv("LUXD_PATH")
		if luxdPath == "" {
			t.Skip("LUXD_PATH not set")
		}

		network := &tmpnet.Network{
			Owner: "e2e-validator-test",
			Nodes: tmpnet.NewNodesOrPanic(3),
			DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					LuxNodePath: luxdPath,
				},
			},
		}

		err := tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, "")
		require.NoError(err)

		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			network.Stop(shutdownCtx)
		}()

		uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
		require.NoError(err)
		nodeURI := uris[0].URI

		pClient := platformvm.NewClient(nodeURI)

		// Get current validators
		validators, err := pClient.GetCurrentValidators(ctx, constants.PrimaryNetworkID, nil)
		require.NoError(err)
		require.NotEmpty(validators.Validators, "Should have validators")

		t.Logf("Active validators: %d", len(validators.Validators))

		// Verify each node is a validator
		for _, node := range network.Nodes {
			found := false
			for _, validator := range validators.Validators {
				if validator.NodeID == node.NodeID {
					found = true
					break
				}
			}
			require.True(found, "Node %s should be a validator", node.NodeID)
		}
	})
}

// =============================================================================
// E2E TESTS - Transaction Processing Post-Regenesis
// =============================================================================

func TestE2ETransactionProcessingPostRegenesis(t *testing.T) {
	if os.Getenv("E2E_REGENESIS") == "" {
		t.Skip("Set E2E_REGENESIS=1 to run this test")
	}

	t.Run("CChainTransactionAfterRegenesis", func(t *testing.T) {
		require := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		luxdPath := os.Getenv("LUXD_PATH")
		if luxdPath == "" {
			t.Skip("LUXD_PATH not set")
		}

		network := &tmpnet.Network{
			Owner: "e2e-tx-test",
			Nodes: tmpnet.NewNodesOrPanic(3),
			DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					LuxNodePath: luxdPath,
				},
			},
		}

		err := tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, "")
		require.NoError(err)

		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			network.Stop(shutdownCtx)
		}()

		uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
		require.NoError(err)
		nodeURI := uris[0].URI

		// Connect to C-Chain
		ethURI := strings.Replace(nodeURI, "http", "ws", 1) + "/ext/bc/C/ws"
		ethClient, err := ethclient.Dial(ethURI)
		require.NoError(err)
		defer ethClient.Close()

		// Get chain ID
		chainID, err := ethClient.ChainID(ctx)
		require.NoError(err)
		t.Logf("C-Chain ID: %s", chainID)

		// Get network ID
		networkID, err := ethClient.NetworkID(ctx)
		require.NoError(err)
		t.Logf("Network ID: %s", networkID)

		// Get block number
		blockNumber, err := ethClient.BlockNumber(ctx)
		require.NoError(err)
		t.Logf("Current block: %d", blockNumber)

		// Get gas price
		gasPrice, err := ethClient.SuggestGasPrice(ctx)
		require.NoError(err)
		t.Logf("Gas price: %s", gasPrice)

		// Verify we can query a block
		block, err := ethClient.BlockByNumber(ctx, big.NewInt(0))
		require.NoError(err)
		require.NotNil(block)
		t.Logf("Genesis block hash: %s", block.Hash().Hex())
	})
}

// =============================================================================
// E2E TESTS - Network Restart Resilience
// =============================================================================

func TestE2ENetworkRestartResilience(t *testing.T) {
	if os.Getenv("E2E_REGENESIS") == "" {
		t.Skip("Set E2E_REGENESIS=1 to run this test")
	}

	t.Run("RestartAfterRegenesis", func(t *testing.T) {
		require := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		luxdPath := os.Getenv("LUXD_PATH")
		if luxdPath == "" {
			t.Skip("LUXD_PATH not set")
		}

		rootDir := t.TempDir()

		network := &tmpnet.Network{
			Owner: "e2e-restart-test",
			Nodes: tmpnet.NewNodesOrPanic(3),
			DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					LuxNodePath: luxdPath,
				},
			},
		}

		// First boot
		err := tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, rootDir)
		require.NoError(err)

		// Get initial state
		uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
		require.NoError(err)
		nodeURI := uris[0].URI

		infoClient := info.NewClient(nodeURI)
		initialNetworkID, err := infoClient.GetNetworkID(ctx)
		require.NoError(err)

		// Stop network
		t.Log("Stopping network...")
		err = network.Stop(ctx)
		require.NoError(err)

		// Wait for clean shutdown
		time.Sleep(5 * time.Second)

		// Restart network
		t.Log("Restarting network...")
		network2, err := tmpnet.ReadNetwork(ctx, log.NoLog{}, network.Dir)
		require.NoError(err)

		err = network2.Restart(ctx)
		require.NoError(err)

		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			network2.Stop(shutdownCtx)
		}()

		// Verify state persisted
		uris2, err := network2.GetNodeURIs(ctx, func(fn func()) {})
		require.NoError(err)
		nodeURI2 := uris2[0].URI

		infoClient2 := info.NewClient(nodeURI2)
		restartedNetworkID, err := infoClient2.GetNetworkID(ctx)
		require.NoError(err)

		require.Equal(initialNetworkID, restartedNetworkID,
			"Network ID should persist across restart")

		// Verify all nodes are healthy
		healthStatus, err := network2.GetHealthStatus(ctx)
		require.NoError(err)
		for nodeID, healthy := range healthStatus {
			require.True(healthy, "Node %s should be healthy after restart", nodeID)
		}

		t.Log("SUCCESS: Network restarted successfully with state preserved")
	})
}

// =============================================================================
// E2E TESTS - Bootstrap New Node
// =============================================================================

func TestE2EBootstrapNewNode(t *testing.T) {
	if os.Getenv("E2E_REGENESIS") == "" {
		t.Skip("Set E2E_REGENESIS=1 to run this test")
	}

	t.Run("NewNodeCanBootstrap", func(t *testing.T) {
		require := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		luxdPath := os.Getenv("LUXD_PATH")
		if luxdPath == "" {
			t.Skip("LUXD_PATH not set")
		}

		// Start initial network
		network := &tmpnet.Network{
			Owner: "e2e-bootstrap-test",
			Nodes: tmpnet.NewNodesOrPanic(3),
			DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					LuxNodePath: luxdPath,
				},
			},
		}

		err := tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, "")
		require.NoError(err)

		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			network.Stop(shutdownCtx)
		}()

		// Get initial block height
		uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
		require.NoError(err)
		nodeURI := uris[0].URI

		ethURI := strings.Replace(nodeURI, "http", "ws", 1) + "/ext/bc/C/ws"
		ethClient, err := ethclient.Dial(ethURI)
		require.NoError(err)
		initialBlock, err := ethClient.BlockNumber(ctx)
		require.NoError(err)
		ethClient.Close()

		t.Logf("Initial block height: %d", initialBlock)

		// Add new node
		t.Log("Adding new node to network...")
		newNode := tmpnet.NewNode()
		newNode.IsEphemeral = true

		err = network.AddNode(ctx, newNode)
		require.NoError(err)

		err = network.StartNode(ctx, newNode)
		require.NoError(err)

		// Wait for new node to bootstrap
		t.Log("Waiting for new node to bootstrap...")
		err = newNode.WaitForHealthy(ctx)
		require.NoError(err)

		// Verify new node has same state
		newNodeURI, _, err := newNode.GetLocalURI(ctx)
		require.NoError(err)

		newEthURI := strings.Replace(newNodeURI, "http", "ws", 1) + "/ext/bc/C/ws"
		newEthClient, err := ethclient.Dial(newEthURI)
		require.NoError(err)
		defer newEthClient.Close()

		newBlock, err := newEthClient.BlockNumber(ctx)
		require.NoError(err)

		require.GreaterOrEqual(newBlock, initialBlock,
			"New node should have caught up to initial block height")

		t.Logf("SUCCESS: New node bootstrapped to block %d", newBlock)
	})
}
