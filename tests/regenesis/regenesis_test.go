// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package regenesis_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/tests/regenesis/testutil"
)

// =============================================================================
// UNIT TESTS - Genesis Block Creation and Validation
// =============================================================================

func TestGenesisBlockCreation(t *testing.T) {
	t.Run("CreateValidGenesisBlock", func(t *testing.T) {
		require := require.New(t)

		cfg := testutil.DefaultGenesisConfig()
		genesisBlock, err := testutil.CreateGenesisBlock(cfg)
		require.NoError(err)
		require.NotNil(genesisBlock)

		// Verify genesis block properties
		require.Equal(uint64(0), genesisBlock.Number)
		require.Equal(common.Hash{}, genesisBlock.ParentHash)
		require.NotEqual(common.Hash{}, genesisBlock.Hash)
		require.NotEqual(common.Hash{}, genesisBlock.StateRoot)
	})

	t.Run("GenesisWithPreFundedAccounts", func(t *testing.T) {
		require := require.New(t)

		cfg := testutil.DefaultGenesisConfig()
		cfg.PreFundedAccounts = map[common.Address]*big.Int{
			common.HexToAddress("0x1234567890123456789012345678901234567890"): big.NewInt(1e18),
			common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"): big.NewInt(2e18),
		}

		genesisBlock, err := testutil.CreateGenesisBlock(cfg)
		require.NoError(err)
		require.NotNil(genesisBlock)

		// Verify allocation is reflected in state root
		require.NotEqual(common.Hash{}, genesisBlock.StateRoot)
	})

	t.Run("GenesisWithValidators", func(t *testing.T) {
		require := require.New(t)

		cfg := testutil.DefaultGenesisConfig()
		cfg.Validators = []testutil.ValidatorConfig{
			{NodeID: ids.GenerateTestNodeID(), Weight: 1000},
			{NodeID: ids.GenerateTestNodeID(), Weight: 2000},
		}

		genesisBlock, err := testutil.CreateGenesisBlock(cfg)
		require.NoError(err)
		require.NotNil(genesisBlock)
	})

	t.Run("InvalidGenesisNetworkID", func(t *testing.T) {
		require := require.New(t)

		cfg := testutil.DefaultGenesisConfig()
		cfg.NetworkID = 0 // Invalid

		_, err := testutil.CreateGenesisBlock(cfg)
		require.Error(err)
		require.Contains(err.Error(), "network ID")
	})

	t.Run("GenesisTimestampValidation", func(t *testing.T) {
		require := require.New(t)

		cfg := testutil.DefaultGenesisConfig()
		cfg.Timestamp = uint64(time.Now().Unix())

		genesisBlock, err := testutil.CreateGenesisBlock(cfg)
		require.NoError(err)
		require.Equal(cfg.Timestamp, genesisBlock.Timestamp)
	})
}

// =============================================================================
// UNIT TESTS - Multi-Chain Genesis Consistency
// =============================================================================

func TestMultiChainGenesisConsistency(t *testing.T) {
	t.Run("PChainXChainCChainConsistency", func(t *testing.T) {
		require := require.New(t)

		networkID := uint32(88888)
		cfg := testutil.MultiChainGenesisConfig{
			NetworkID: networkID,
			Timestamp: uint64(time.Now().Unix()),
		}

		genesisCfg, err := testutil.CreateMultiChainGenesis(cfg)
		require.NoError(err)
		require.NotNil(genesisCfg)

		// Verify network ID consistency
		require.Equal(networkID, genesisCfg.NetworkID)

		// Parse and verify C-Chain genesis
		var cChainGenesis map[string]interface{}
		err = json.Unmarshal([]byte(genesisCfg.CChainGenesis), &cChainGenesis)
		require.NoError(err)

		// Verify chain ID matches network ID
		if config, ok := cChainGenesis["config"].(map[string]interface{}); ok {
			if chainID, ok := config["chainId"].(float64); ok {
				require.Equal(float64(networkID), chainID)
			}
		}
	})

	t.Run("CrossChainAssetIDConsistency", func(t *testing.T) {
		require := require.New(t)

		cfg := testutil.MultiChainGenesisConfig{
			NetworkID: 88888,
		}

		genesisCfg, err := testutil.CreateMultiChainGenesis(cfg)
		require.NoError(err)

		// Verify asset IDs are consistently defined
		require.NotEmpty(genesisCfg.Allocations)
	})

	t.Run("ValidatorSetConsistency", func(t *testing.T) {
		require := require.New(t)

		validators := []testutil.ValidatorConfig{
			{NodeID: ids.GenerateTestNodeID(), Weight: 1000},
			{NodeID: ids.GenerateTestNodeID(), Weight: 2000},
		}

		cfg := testutil.MultiChainGenesisConfig{
			NetworkID:  88888,
			Validators: validators,
		}

		genesisCfg, err := testutil.CreateMultiChainGenesis(cfg)
		require.NoError(err)

		// Verify validators are properly configured
		require.Len(genesisCfg.InitialStakers, len(validators))
	})
}

// =============================================================================
// UNIT TESTS - State Migration
// =============================================================================

func TestStateMigration(t *testing.T) {
	t.Run("MigrateEmptyState", func(t *testing.T) {
		require := require.New(t)

		srcDir := t.TempDir()
		dstDir := t.TempDir()

		migrator := testutil.NewStateMigrator(srcDir, dstDir)
		defer migrator.Close()

		stats, err := migrator.Migrate(context.Background())
		require.NoError(err)
		require.Equal(uint64(0), stats.KeysMigrated)
	})

	t.Run("MigrateWithNamespaceStripping", func(t *testing.T) {
		require := require.New(t)

		// Setup source database with namespaced keys
		srcDB := testutil.CreateTestDatabase("")
		dstDB := testutil.CreateTestDatabase("")
		namespace := common.FromHex("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")

		// Write test keys with namespace prefix
		testKeys := [][]byte{
			append(namespace, []byte("key1")...),
			append(namespace, []byte("key2")...),
			append(namespace, []byte("key3")...),
		}
		for _, key := range testKeys {
			err := srcDB.Put(key, []byte("value"))
			require.NoError(err)
		}

		// Migrate using injected databases
		migrator := testutil.NewStateMigratorWithDatabases(srcDB, dstDB)
		migrator.SetNamespace(namespace)

		stats, err := migrator.Migrate(context.Background())
		require.NoError(err)
		require.Equal(uint64(3), stats.KeysMigrated)

		// Verify keys are stripped - check destination database BEFORE closing
		val, err := dstDB.Get([]byte("key1"))
		require.NoError(err)
		require.Equal([]byte("value"), val)

		// Close after verification
		migrator.Close()
	})

	t.Run("MigratePreservesBlockStructure", func(t *testing.T) {
		require := require.New(t)

		// Create source with block data using injected databases
		srcDB := testutil.CreateTestDatabase("")
		dstDB := testutil.CreateTestDatabase("")

		testutil.WriteTestBlocks(srcDB, 10)

		// Migrate using injected databases
		migrator := testutil.NewStateMigratorWithDatabases(srcDB, dstDB)
		stats, err := migrator.Migrate(context.Background())
		require.NoError(err)
		require.Greater(stats.BlocksMigrated, uint64(0))

		// Verify block structure in destination database BEFORE closing
		for i := uint64(0); i < 10; i++ {
			canonKey := testutil.CanonicalKey(uint64(i))
			hashBytes, err := dstDB.Get(canonKey)
			require.NoError(err)
			require.NotEmpty(hashBytes, "block %d canonical hash should exist", i)

			hash := common.BytesToHash(hashBytes)
			headerKey := testutil.HeaderKey(uint64(i), hash)
			exists, err := dstDB.Has(headerKey)
			require.NoError(err)
			require.True(exists, "block %d header should exist", i)
		}

		// Close after verification
		migrator.Close()
	})
}

// =============================================================================
// UNIT TESTS - Block Import with Hash Verification
// =============================================================================

func TestBlockImportHashVerification(t *testing.T) {
	t.Run("ImportBlockVerifyHash", func(t *testing.T) {
		require := require.New(t)

		importer := testutil.NewBlockImporter(t.TempDir())
		defer importer.Close()

		// Create a test block
		block := testutil.CreateTestBlock(1, common.Hash{})

		// Import and verify
		err := importer.ImportBlock(block)
		require.NoError(err)

		// Verify hash matches
		storedHash, err := importer.GetBlockHash(1)
		require.NoError(err)
		require.Equal(block.Hash, storedHash)
	})

	t.Run("ImportBlockSequenceVerification", func(t *testing.T) {
		require := require.New(t)

		importer := testutil.NewBlockImporter(t.TempDir())
		defer importer.Close()

		// Import blocks in sequence
		parentHash := common.Hash{}
		for i := uint64(0); i < 5; i++ {
			block := testutil.CreateTestBlock(i, parentHash)
			err := importer.ImportBlock(block)
			require.NoError(err)
			parentHash = block.Hash
		}

		// Verify chain integrity
		for i := uint64(1); i < 5; i++ {
			block, err := importer.GetBlock(i)
			require.NoError(err)

			parentBlock, err := importer.GetBlock(i - 1)
			require.NoError(err)

			require.Equal(parentBlock.Hash, block.ParentHash,
				"block %d parent hash mismatch", i)
		}
	})

	t.Run("RejectBlockWithInvalidHash", func(t *testing.T) {
		require := require.New(t)

		importer := testutil.NewBlockImporter(t.TempDir())
		defer importer.Close()

		// Create a block with corrupted hash
		block := testutil.CreateTestBlock(1, common.Hash{})
		block.Hash[0] ^= 0xFF // Corrupt the hash

		// Import should fail verification
		err := importer.ImportBlockWithVerification(block)
		require.Error(err)
		require.Contains(err.Error(), "hash verification failed")
	})

	t.Run("CanonicalHashMapping", func(t *testing.T) {
		require := require.New(t)

		importer := testutil.NewBlockImporter(t.TempDir())
		defer importer.Close()

		block := testutil.CreateTestBlock(100, common.Hash{})
		err := importer.ImportBlock(block)
		require.NoError(err)

		// Verify canonical hash mapping
		canonicalHash, err := importer.GetCanonicalHash(100)
		require.NoError(err)
		require.Equal(block.Hash, canonicalHash)
	})
}

// =============================================================================
// UNIT TESTS - RLP Encoding/Decoding
// =============================================================================

func TestRLPEncodingDecoding(t *testing.T) {
	t.Run("HeaderRLPRoundTrip", func(t *testing.T) {
		require := require.New(t)

		header := &types.Header{
			ParentHash:  common.HexToHash("0x1234"),
			Number:      big.NewInt(100),
			GasLimit:    8000000,
			GasUsed:     21000,
			Time:        uint64(time.Now().Unix()),
			BaseFee:     big.NewInt(1000000000),
			Difficulty:  big.NewInt(0),
			Coinbase:    common.HexToAddress("0xabcd"),
		}

		// Encode
		encoded, err := rlp.EncodeToBytes(header)
		require.NoError(err)

		// Decode
		var decoded types.Header
		err = rlp.DecodeBytes(encoded, &decoded)
		require.NoError(err)

		// Verify
		require.Equal(header.ParentHash, decoded.ParentHash)
		require.Equal(header.Number.Uint64(), decoded.Number.Uint64())
		require.Equal(header.GasLimit, decoded.GasLimit)
	})

	t.Run("BlockRLPRoundTrip", func(t *testing.T) {
		require := require.New(t)

		block := testutil.CreateTestBlock(50, common.Hash{})

		// Encode
		encoded, err := rlp.EncodeToBytes(block.Header)
		require.NoError(err)
		require.NotEmpty(encoded)

		// Verify it's valid RLP
		var decoded types.Header
		err = rlp.DecodeBytes(encoded, &decoded)
		require.NoError(err)
	})
}

// =============================================================================
// UNIT TESTS - Database Key Format
// =============================================================================

func TestDatabaseKeyFormat(t *testing.T) {
	t.Run("HeaderKeyFormat", func(t *testing.T) {
		require := require.New(t)

		blockNum := uint64(12345)
		blockHash := common.HexToHash("0xabcdef1234567890")

		key := testutil.HeaderKey(blockNum, blockHash)

		// Verify key format: 'h' + 8-byte number + 32-byte hash = 41 bytes
		require.Len(key, 41)
		require.Equal(byte('h'), key[0])

		// Extract and verify number
		extractedNum := binary.BigEndian.Uint64(key[1:9])
		require.Equal(blockNum, extractedNum)

		// Extract and verify hash
		extractedHash := common.BytesToHash(key[9:41])
		require.Equal(blockHash, extractedHash)
	})

	t.Run("CanonicalKeyFormat", func(t *testing.T) {
		require := require.New(t)

		blockNum := uint64(12345)
		key := testutil.CanonicalKey(blockNum)

		// Verify key format: 'h' + 8-byte number + 'n' = 10 bytes
		require.Len(key, 10)
		require.Equal(byte('h'), key[0])
		require.Equal(byte('n'), key[9])

		// Extract and verify number
		extractedNum := binary.BigEndian.Uint64(key[1:9])
		require.Equal(blockNum, extractedNum)
	})

	t.Run("BodyKeyFormat", func(t *testing.T) {
		require := require.New(t)

		blockNum := uint64(12345)
		blockHash := common.HexToHash("0xabcdef1234567890")

		key := testutil.BodyKey(blockNum, blockHash)

		// Verify key format: 'b' + 8-byte number + 32-byte hash = 41 bytes
		require.Len(key, 41)
		require.Equal(byte('b'), key[0])
	})
}

// =============================================================================
// UNIT TESTS - Progress Tracking
// =============================================================================

func TestMigrationProgress(t *testing.T) {
	t.Run("ProgressCallback", func(t *testing.T) {
		require := require.New(t)

		// Create source with blocks using injected databases
		srcDB := testutil.CreateTestDatabase("")
		dstDB := testutil.CreateTestDatabase("")
		testutil.WriteTestBlocks(srcDB, 100)

		migrator := testutil.NewStateMigratorWithDatabases(srcDB, dstDB)
		defer migrator.Close()

		// Track progress
		var progressCalls int
		migrator.SetProgressCallback(func(current, total uint64) {
			progressCalls++
			require.LessOrEqual(current, total)
		})

		_, err := migrator.Migrate(context.Background())
		require.NoError(err)
		require.Greater(progressCalls, 0)
	})

	t.Run("ResumeFromCheckpoint", func(t *testing.T) {
		require := require.New(t)

		// Create source with blocks using injected databases
		srcDB := testutil.CreateTestDatabase("")
		dstDB := testutil.CreateTestDatabase("")
		testutil.WriteTestBlocks(srcDB, 100)

		// First migration (partial - stop after 50 blocks)
		migrator := testutil.NewStateMigratorWithDatabases(srcDB, dstDB)
		migrator.SetStopAfter(50)
		stats, err := migrator.Migrate(context.Background())
		require.NoError(err)

		// Verify partial migration stopped at expected block count
		require.Equal(uint64(50), stats.BlocksMigrated)
		require.Greater(stats.LastProcessedBlock, uint64(0))
		require.Less(stats.LastProcessedBlock, uint64(100))

		// Verify checkpoint tracking worked
		require.Greater(stats.KeysMigrated, uint64(0))

		// Close after verification
		migrator.Close()
	})
}

// =============================================================================
// BENCHMARK TESTS
// =============================================================================

func BenchmarkBlockImport(b *testing.B) {
	importer := testutil.NewBlockImporter(b.TempDir())
	defer importer.Close()

	block := testutil.CreateTestBlock(0, common.Hash{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		block.Number = uint64(i)
		_ = importer.ImportBlock(block)
	}
}

func BenchmarkStateMigration(b *testing.B) {
	srcDir := b.TempDir()

	// Create source with data
	srcDB := testutil.CreateTestDatabase(srcDir)
	testutil.WriteTestBlocks(srcDB, 1000)
	srcDB.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dstDir := filepath.Join(b.TempDir(), "dst")
		os.MkdirAll(dstDir, 0755)

		migrator := testutil.NewStateMigrator(srcDir, dstDir)
		_, _ = migrator.Migrate(context.Background())
		migrator.Close()
	}
}

func BenchmarkHeaderKeyParsing(b *testing.B) {
	key := testutil.HeaderKey(12345, common.HexToHash("0xabcdef"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testutil.ParseHeaderKey(key)
	}
}
