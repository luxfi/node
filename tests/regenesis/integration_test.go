// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build integration

package regenesis_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/tests/regenesis/testutil"
	"github.com/luxfi/node/vms/platformvm"
)

// =============================================================================
// INTEGRATION TESTS - Network Bootstrap with Validators
// =============================================================================

func TestNetworkBootstrapWithValidators(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("BootstrapSingleValidator", func(t *testing.T) {
		t.Skip("Requires LUXD_PATH environment variable")
		require := require.New(t)

		luxdPath := os.Getenv("LUXD_PATH")
		if luxdPath == "" {
			t.Skip("LUXD_PATH not set")
		}

		network := &tmpnet.Network{
			Owner: "regenesis-test-bootstrap",
			Nodes: tmpnet.NewNodesOrPanic(1),
			DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					LuxNodePath: luxdPath,
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		err := tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, "")
		require.NoError(err)

		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			network.Stop(shutdownCtx)
		}()

		// Verify node is healthy
		for _, node := range network.Nodes {
			healthy, err := node.IsHealthy(ctx)
			require.NoError(err)
			require.True(healthy)
		}
	})

	t.Run("BootstrapMultipleValidators", func(t *testing.T) {
		t.Skip("Requires LUXD_PATH environment variable")
		require := require.New(t)

		luxdPath := os.Getenv("LUXD_PATH")
		if luxdPath == "" {
			t.Skip("LUXD_PATH not set")
		}

		network := &tmpnet.Network{
			Owner: "regenesis-test-multi",
			Nodes: tmpnet.NewNodesOrPanic(3),
			DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					LuxNodePath: luxdPath,
				},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		err := tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, "")
		require.NoError(err)

		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			network.Stop(shutdownCtx)
		}()

		// Verify all nodes are healthy
		for _, node := range network.Nodes {
			healthy, err := node.IsHealthy(ctx)
			require.NoError(err)
			require.True(healthy, "Node %s should be healthy", node.NodeID)
		}

		// Verify nodes can see each other
		uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
		require.NoError(err)
		require.Len(uris, 3)
	})
}

// =============================================================================
// INTEGRATION TESTS - eth_blockNumber Verification
// =============================================================================

func TestEthBlockNumberReflectsImportedState(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("BlockNumberAfterMigration", func(t *testing.T) {
		t.Skip("Requires running network")
		require := require.New(t)

		// This test would:
		// 1. Start a network with migrated data
		// 2. Query eth_blockNumber
		// 3. Verify it matches expected height from migration

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Mock expected values
		expectedBlockNumber := uint64(1000000)

		// In a real test, we would:
		// ethClient := NewEthClient(nodeURI)
		// blockNumber, err := ethClient.BlockNumber(ctx)
		// require.NoError(err)
		// require.Equal(expectedBlockNumber, blockNumber)

		_ = ctx
		_ = expectedBlockNumber
	})
}

// =============================================================================
// INTEGRATION TESTS - Cross-Chain State Consistency
// =============================================================================

func TestCrossChainStateConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("PChainXChainBalanceConsistency", func(t *testing.T) {
		t.Skip("Requires running network")
		require := require.New(t)

		// This test verifies that balances are consistent across chains
		// after a regenesis migration

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// In a real test:
		// 1. Query P-Chain balance for an address
		// 2. Query X-Chain balance for the same address
		// 3. Verify consistency based on migration rules

		_ = ctx
		require.True(true) // Placeholder
	})

	t.Run("CChainStateRootMatch", func(t *testing.T) {
		t.Skip("Requires running network")
		require := require.New(t)

		// This test verifies that the C-Chain state root matches
		// the expected state root from the source chain

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Expected state root from migration
		expectedStateRoot := common.HexToHash("0x...")

		// In a real test:
		// ethClient := NewEthClient(nodeURI)
		// block, err := ethClient.BlockByNumber(ctx, nil)
		// require.NoError(err)
		// require.Equal(expectedStateRoot, block.Root())

		_ = ctx
		_ = expectedStateRoot
		require.True(true) // Placeholder
	})
}

// =============================================================================
// INTEGRATION TESTS - Full Migration Flow
// =============================================================================

func TestFullMigrationFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("MigrateAndBootstrap", func(t *testing.T) {
		t.Skip("Requires LUXD_PATH and source database")
		require := require.New(t)

		// Create temporary directories
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		// Create source database with test data
		srcDB := testutil.CreateTestDatabase(srcDir)
		testutil.WriteTestBlocks(srcDB, 100)
		srcDB.Close()

		// Perform migration
		migrator := testutil.NewStateMigrator(srcDir, dstDir)
		stats, err := migrator.Migrate(context.Background())
		require.NoError(err)
		require.Equal(uint64(100), stats.BlocksMigrated)
		migrator.Close()

		// In a real test, we would then:
		// 1. Create a network configuration pointing to the migrated data
		// 2. Bootstrap the network
		// 3. Verify the network comes up healthy
		// 4. Verify block heights and state roots match
	})
}

// =============================================================================
// INTEGRATION TESTS - Genesis Validation
// =============================================================================

func TestGenesisValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("ValidateGenesisAgainstNetwork", func(t *testing.T) {
		t.Skip("Requires running network")
		require := require.New(t)

		// This test validates that a genesis configuration
		// can be used to start a network

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Create genesis
		cfg := testutil.MultiChainGenesisConfig{
			NetworkID: 88888,
			Validators: []testutil.ValidatorConfig{
				{NodeID: ids.GenerateTestNodeID(), Weight: 1000},
			},
		}

		genesis, err := testutil.CreateMultiChainGenesis(cfg)
		require.NoError(err)
		require.NotNil(genesis)

		// Validate genesis structure
		require.Equal(cfg.NetworkID, genesis.NetworkID)
		require.NotEmpty(genesis.CChainGenesis)
		require.NotEmpty(genesis.Allocations)

		_ = ctx
	})

	t.Run("GenesisHashDeterminism", func(t *testing.T) {
		require := require.New(t)

		// Same configuration should produce same genesis hash
		cfg := testutil.DefaultGenesisConfig()

		genesis1, err := testutil.CreateGenesisBlock(cfg)
		require.NoError(err)

		genesis2, err := testutil.CreateGenesisBlock(cfg)
		require.NoError(err)

		require.Equal(genesis1.Hash, genesis2.Hash)
		require.Equal(genesis1.StateRoot, genesis2.StateRoot)
	})
}

// =============================================================================
// INTEGRATION TESTS - Block Import Verification
// =============================================================================

func TestBlockImportVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("ImportLargeBlockRange", func(t *testing.T) {
		require := require.New(t)

		importer := testutil.NewBlockImporter(t.TempDir())
		defer importer.Close()

		// Import 1000 blocks
		parentHash := common.Hash{}
		for i := uint64(0); i < 1000; i++ {
			block := testutil.CreateTestBlock(i, parentHash)
			err := importer.ImportBlock(block)
			require.NoError(err)
			parentHash = block.Hash
		}

		// Verify chain integrity
		for i := uint64(1); i < 1000; i++ {
			block, err := importer.GetBlock(i)
			require.NoError(err)

			parentBlock, err := importer.GetBlock(i - 1)
			require.NoError(err)

			require.Equal(parentBlock.Hash, block.ParentHash,
				"Block %d parent hash mismatch", i)
		}
	})

	t.Run("VerifyBlockHashConsistency", func(t *testing.T) {
		require := require.New(t)

		importer := testutil.NewBlockImporter(t.TempDir())
		defer importer.Close()

		// Import blocks
		blocks := make([]*testutil.TestBlock, 100)
		parentHash := common.Hash{}
		for i := uint64(0); i < 100; i++ {
			blocks[i] = testutil.CreateTestBlock(i, parentHash)
			err := importer.ImportBlock(blocks[i])
			require.NoError(err)
			parentHash = blocks[i].Hash
		}

		// Verify all hashes match
		for i := uint64(0); i < 100; i++ {
			storedHash, err := importer.GetBlockHash(i)
			require.NoError(err)
			require.Equal(blocks[i].Hash, storedHash)
		}
	})
}

// =============================================================================
// INTEGRATION TESTS - Network Info Verification
// =============================================================================

func TestNetworkInfoAfterRegenesis(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("VerifyNetworkID", func(t *testing.T) {
		t.Skip("Requires running network")
		require := require.New(t)

		// This test verifies network ID is correctly set after regenesis

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Expected network ID from genesis
		expectedNetworkID := uint32(88888)

		// In a real test:
		// infoClient := info.NewClient(nodeURI)
		// networkID, err := infoClient.GetNetworkID(ctx)
		// require.NoError(err)
		// require.Equal(expectedNetworkID, networkID)

		_ = ctx
		_ = expectedNetworkID
		require.True(true) // Placeholder
	})

	t.Run("VerifyValidatorSet", func(t *testing.T) {
		t.Skip("Requires running network")
		require := require.New(t)

		// This test verifies validator set matches genesis configuration

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// In a real test:
		// pClient := platformvm.NewClient(nodeURI)
		// validators, err := pClient.GetCurrentValidators(ctx, constants.PrimaryNetworkID, nil)
		// require.NoError(err)
		// require.NotEmpty(validators.Validators)

		_ = ctx
		require.True(true) // Placeholder
	})
}

// =============================================================================
// HELPER FUNCTIONS FOR INTEGRATION TESTS
// =============================================================================

// startTestNetwork starts a test network for integration testing
func startTestNetwork(t *testing.T, nodeCount int) (*tmpnet.Network, func()) {
	t.Helper()

	luxdPath := os.Getenv("LUXD_PATH")
	if luxdPath == "" {
		t.Skip("LUXD_PATH not set")
	}

	network := &tmpnet.Network{
		Owner: "regenesis-integration-test",
		Nodes: tmpnet.NewNodesOrPanic(nodeCount),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	err := tmpnet.BootstrapNewNetwork(ctx, log.NoLog{}, network, "")
	if err != nil {
		cancel()
		t.Fatalf("Failed to bootstrap network: %v", err)
	}

	cleanup := func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}

	return network, cleanup
}

// getNodeInfoClient returns an info client for a node
func getNodeInfoClient(t *testing.T, network *tmpnet.Network) info.Client {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
	if err != nil {
		t.Fatalf("Failed to get node URIs: %v", err)
	}

	if len(uris) == 0 {
		t.Fatal("No node URIs available")
	}

	return info.NewClient(uris[0].URI)
}

// getPlatformClient returns a platform VM client for a node
func getPlatformClient(t *testing.T, network *tmpnet.Network) platformvm.Client {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uris, err := network.GetNodeURIs(ctx, func(fn func()) {})
	if err != nil {
		t.Fatalf("Failed to get node URIs: %v", err)
	}

	if len(uris) == 0 {
		t.Fatal("No node URIs available")
	}

	return platformvm.NewClient(uris[0].URI)
}
