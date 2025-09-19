// (c) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/consensus"
	"github.com/luxfi/geth/consensus/clique"
	gethcore "github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/state"
	"github.com/luxfi/geth/core/txpool"
	"github.com/luxfi/geth/core/txpool/legacypool"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/core/vm"
	"github.com/luxfi/geth/eth/ethconfig"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/geth/rpc"
	"github.com/luxfi/geth/triedb"
)

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// MinimalEthBackend provides a minimal Ethereum backend without p2p networking
type MinimalEthBackend struct {
	chainConfig *params.ChainConfig
	blockchain  *gethcore.BlockChain
	txPool      *txpool.TxPool
	chainDb     ethdb.Database
	engine      consensus.Engine
	networkID   uint64
}

// NewMigratedBackend creates a special backend for fully migrated data
// This completely bypasses genesis initialization
func NewMigratedBackend(db ethdb.Database, migratedHeight uint64) (*MinimalEthBackend, error) {
	fmt.Printf("Creating migrated backend for Coreth data at height %d\n", migratedHeight)
	fmt.Printf("Database type: %T\n", db)

	// The migrated data is already in proper geth format in the ethdb
	// We can use it directly
	rawDB := db

	// Test if we can read a key directly
	testKey := []byte("LastBlock")
	if val, err := rawDB.Get(testKey); err == nil {
		fmt.Printf("Successfully read LastBlock: %x (len=%d)\n", val, len(val))
	} else {
		fmt.Printf("Failed to read LastBlock: %v\n", err)
	}

	// Also try a canonical hash key
	testKey2 := make([]byte, 9)
	testKey2[0] = 'H'
	binary.BigEndian.PutUint64(testKey2[1:], 0)
	if val, err := rawDB.Get(testKey2); err == nil {
		fmt.Printf("Successfully read block 0 canonical: %x (len=%d)\n", val, len(val))
	} else {
		fmt.Printf("Failed to read block 0 canonical with key %x: %v\n", testKey2, err)
	}

	// Create chain config for LUX mainnet with all forks enabled
	chainConfig := &params.ChainConfig{
		ChainID:                 big.NewInt(96369),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		MuirGlacierBlock:        big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		ArrowGlacierBlock:       big.NewInt(0),
		GrayGlacierBlock:        big.NewInt(0),
		MergeNetsplitBlock:      big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              nil,
		VerkleTime:              nil,
		TerminalTotalDifficulty: common.Big0,
		BlobScheduleConfig: &params.BlobScheduleConfig{
			Cancun: &params.BlobConfig{
				Target:         3,
				Max:            6,
				UpdateFraction: 3338477,
			},
		},
	}

	// Create a dummy consensus engine
	engine := &dummyEngine{}

	fmt.Printf("Reading migrated database for blocks...\n")

	// The database is already migrated with proper Coreth format
	// We just need to verify it has the expected blocks

	// Check for LastBlock key
	lastBlockBytes, err := rawDB.Get([]byte("LastBlock"))
	if err == nil && len(lastBlockBytes) == 32 {
		var lastBlockHash common.Hash
		copy(lastBlockHash[:], lastBlockBytes)
		fmt.Printf("Found LastBlock in database: %x\n", lastBlockHash)

		// Set this as the head
		rawdb.WriteHeadBlockHash(rawDB, lastBlockHash)
		rawdb.WriteHeadHeaderHash(rawDB, lastBlockHash)
		rawdb.WriteHeadFastBlockHash(rawDB, lastBlockHash)
	}

	// Count available blocks by checking canonical hashes
	// Use direct key access since ReadCanonicalHash might not work with BadgerDB wrapper
	blockCount := 0
	for i := uint64(0); i <= 10; i++ {
		// Build the canonical hash key: 'H' + block number
		key := make([]byte, 9)
		key[0] = 'H'
		binary.BigEndian.PutUint64(key[1:], i)

		// Try to read the hash value
		hashBytes, err := rawDB.Get(key)
		if err == nil && len(hashBytes) == 32 {
			var hash common.Hash
			copy(hash[:], hashBytes)
			blockCount++
			fmt.Printf("  Found block %d: %x\n", i, hash[:8])
		} else if err != nil {
			// Debug: show the error
			if i == 0 {
				fmt.Printf("  Failed to read block %d with key %x: %v\n", i, key, err)
			}
		}
	}

	// Also check the target height
	if migratedHeight > 10 {
		key := make([]byte, 9)
		key[0] = 'H'
		binary.BigEndian.PutUint64(key[1:], migratedHeight)

		hashBytes, err := rawDB.Get(key)
		if err == nil && len(hashBytes) == 32 {
			var hash common.Hash
			copy(hash[:], hashBytes)
			blockCount++
			fmt.Printf("  Found block %d: %x\n", migratedHeight, hash[:8])
		}
	}

	fmt.Printf("Found %d canonical blocks in migrated database\n", blockCount)

	if blockCount == 0 {
		// The database might use different key format, try direct iteration
		// but limit it to avoid crashes
		fmt.Printf("No canonical blocks found, database may need re-migration\n")
		return nil, fmt.Errorf("no canonical blocks found in migrated database")
	}

	// Use the migrated height as the target
	actualHeight := migratedHeight

	// Get the hash at the migrated height using direct key access
	var headHash common.Hash
	key := make([]byte, 9)
	key[0] = 'H'
	binary.BigEndian.PutUint64(key[1:], migratedHeight)

	hashBytes, err := rawDB.Get(key)
	if err == nil && len(hashBytes) == 32 {
		copy(headHash[:], hashBytes)
	} else {
		// Try to find the highest available block
		for i := migratedHeight; i > 0 && i > migratedHeight-1000; i-- {
			key := make([]byte, 9)
			key[0] = 'H'
			binary.BigEndian.PutUint64(key[1:], i)

			hashBytes, err := rawDB.Get(key)
			if err == nil && len(hashBytes) == 32 {
				copy(headHash[:], hashBytes)
				actualHeight = i
				fmt.Printf("Block %d not found, using block %d instead\n", migratedHeight, i)
				break
			}
		}
	}

	if headHash == (common.Hash{}) {
		return nil, fmt.Errorf("no head block found in migrated database at height %d", migratedHeight)
	}

	fmt.Printf("Setting head to block %d with hash: %x\n", actualHeight, headHash)

	// The canonical mappings should already be in the database from migration
	// Just ensure the head pointers are set correctly
	rawdb.WriteHeadBlockHash(rawDB, headHash)
	rawdb.WriteHeadHeaderHash(rawDB, headHash)
	rawdb.WriteHeadFastBlockHash(rawDB, headHash)
	rawdb.WriteLastPivotNumber(rawDB, actualHeight)

	// Create blockchain options that skip validation
	options := &gethcore.BlockChainConfig{
		TrieCleanLimit: 256,
		NoPrefetch:     false,
		StateScheme:    rawdb.HashScheme,
	}

	// CRITICAL: Create blockchain WITHOUT genesis
	// This prevents any genesis initialization
	fmt.Printf("Creating blockchain without genesis...\n")
	blockchain, err := gethcore.NewBlockChain(db, nil, engine, options)
	if err != nil {
		fmt.Printf("Failed to create blockchain: %v\n", err)
		return nil, fmt.Errorf("failed to create blockchain from migrated data: %w", err)
	}

	// Verify the blockchain loaded at the right height
	currentBlock := blockchain.CurrentBlock()
	fmt.Printf("Blockchain initialized at height: %d\n", currentBlock.Number.Uint64())

	// Create transaction pool
	legacyPool := legacypool.New(ethconfig.Defaults.TxPool, blockchain)
	txPool, err := txpool.New(ethconfig.Defaults.TxPool.PriceLimit, blockchain, []txpool.SubPool{legacyPool})
	if err != nil {
		return nil, err
	}

	return &MinimalEthBackend{
		chainConfig: chainConfig,
		blockchain:  blockchain,
		txPool:      txPool,
		chainDb:     db,
		engine:      engine,
		networkID:   96369,
	}, nil
}

// NewMinimalEthBackendForMigration creates a backend that loads from migrated data
func NewMinimalEthBackendForMigration(db ethdb.Database, config *ethconfig.Config, genesis *gethcore.Genesis, migratedHeight uint64) (*MinimalEthBackend, error) {
	var chainConfig *params.ChainConfig
	if genesis != nil && genesis.Config != nil {
		chainConfig = genesis.Config
	} else {
		// Use a default config for migrated data with all forks enabled
		chainConfig = &params.ChainConfig{
			ChainID:                 big.NewInt(96369),
			HomesteadBlock:          big.NewInt(0),
			EIP150Block:             big.NewInt(0),
			EIP155Block:             big.NewInt(0),
			EIP158Block:             big.NewInt(0),
			ByzantiumBlock:          big.NewInt(0),
			ConstantinopleBlock:     big.NewInt(0),
			PetersburgBlock:         big.NewInt(0),
			IstanbulBlock:           big.NewInt(0),
			MuirGlacierBlock:        big.NewInt(0),
			BerlinBlock:             big.NewInt(0),
			LondonBlock:             big.NewInt(0),
			ArrowGlacierBlock:       big.NewInt(0),
			GrayGlacierBlock:        big.NewInt(0),
			MergeNetsplitBlock:      big.NewInt(0),
			ShanghaiTime:            newUint64(0),
			CancunTime:              newUint64(0),
			PragueTime:              nil,
			VerkleTime:              nil,
			TerminalTotalDifficulty: common.Big0,
			BlobScheduleConfig: &params.BlobScheduleConfig{
				Cancun: &params.BlobConfig{
					Target:         3,
					Max:            6,
					UpdateFraction: 3338477,
				},
			},
		}
	}

	// Create consensus engine
	var engine consensus.Engine
	if chainConfig.Clique != nil {
		engine = clique.New(chainConfig.Clique, db)
	} else {
		// Use a dummy engine for PoS
		engine = &dummyEngine{}
	}

	// Set the head pointers to the migrated height
	fmt.Printf("Setting blockchain to migrated height %d\n", migratedHeight)

	// Get the hash at the migrated height using 9-byte format
	key := canonicalKey(migratedHeight)

	var headHash common.Hash
	if val, err := db.Get(key); err == nil && len(val) == 32 {
		copy(headHash[:], val)
		fmt.Printf("Found head hash at height %d: %x\n", migratedHeight, headHash)

		// Write head pointers
		rawdb.WriteHeadBlockHash(db, headHash)
		rawdb.WriteHeadHeaderHash(db, headHash)
		rawdb.WriteHeadFastBlockHash(db, headHash)
		rawdb.WriteLastPivotNumber(db, migratedHeight)
	}

	// Initialize blockchain - skip genesis since we have migrated data
	options := &gethcore.BlockChainConfig{
		TrieCleanLimit: config.TrieCleanCache,
		NoPrefetch:     config.NoPrefetch,
		StateScheme:    rawdb.HashScheme,
	}

	// IMPORTANT: Pass nil genesis to prevent overwriting migrated data
	fmt.Printf("Creating blockchain from migrated data...\n")
	blockchain, err := gethcore.NewBlockChain(db, nil, engine, options)
	if err != nil {
		// If it fails, it might be because it expects genesis
		// Try creating a minimal genesis that won't overwrite data
		fmt.Printf("First attempt failed: %v, trying with minimal genesis\n", err)

		minimalGenesis := &gethcore.Genesis{
			Config:     chainConfig,
			Difficulty: big.NewInt(0),
			GasLimit:   8000000,
			Alloc:      nil, // No allocations to prevent state overwrite
		}

		blockchain, err = gethcore.NewBlockChain(db, minimalGenesis, engine, options)
		if err != nil {
			return nil, fmt.Errorf("failed to create blockchain from migrated data: %w", err)
		}
	}

	fmt.Printf("Blockchain created, current height: %d\n", blockchain.CurrentBlock().Number.Uint64())

	// Create transaction pool
	legacyPool := legacypool.New(config.TxPool, blockchain)
	txPool, err := txpool.New(config.TxPool.PriceLimit, blockchain, []txpool.SubPool{legacyPool})
	if err != nil {
		return nil, err
	}

	return &MinimalEthBackend{
		chainConfig: chainConfig,
		blockchain:  blockchain,
		txPool:      txPool,
		chainDb:     db,
		engine:      engine,
		networkID:   config.NetworkId,
	}, nil
}

// NewMinimalEthBackend creates a new minimal Ethereum backend
func NewMinimalEthBackend(db ethdb.Database, config *ethconfig.Config, genesis *gethcore.Genesis) (*MinimalEthBackend, error) {
	// Special marker for "use existing genesis in database"
	_ = false // useExistingGenesis - may use later

	// If no genesis is provided, check if we should use existing or create default
	if genesis == nil {
		// Check if database already has a genesis
		if existingHash := rawdb.ReadCanonicalHash(db, 0); existingHash != (common.Hash{}) {
			fmt.Printf("Using existing genesis from database: %s\n", existingHash.Hex())
			// Use existing genesis - no action needed
		} else {
			// No existing genesis, create default
			genesis = &gethcore.Genesis{
				Config:     params.AllEthashProtocolChanges,
				Difficulty: big.NewInt(1),
				GasLimit:   8000000,
				Alloc: gethcore.GenesisAlloc{
					// Default test account with some balance
					common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"): types.Account{
						Balance: new(big.Int).Mul(big.NewInt(1000000), big.NewInt(params.Ether)),
					},
				},
			}
		}
	}

	var chainConfig *params.ChainConfig
	if genesis != nil {
		chainConfig = genesis.Config
	}
	if chainConfig == nil {
		// Use default mainnet config for replay scenarios with all forks enabled
		chainConfig = &params.ChainConfig{
			ChainID:                 big.NewInt(96369),
			HomesteadBlock:          big.NewInt(0),
			EIP150Block:             big.NewInt(0),
			EIP155Block:             big.NewInt(0),
			EIP158Block:             big.NewInt(0),
			ByzantiumBlock:          big.NewInt(0),
			ConstantinopleBlock:     big.NewInt(0),
			PetersburgBlock:         big.NewInt(0),
			IstanbulBlock:           big.NewInt(0),
			MuirGlacierBlock:        big.NewInt(0),
			BerlinBlock:             big.NewInt(0),
			LondonBlock:             big.NewInt(0),
			ArrowGlacierBlock:       big.NewInt(0),
			GrayGlacierBlock:        big.NewInt(0),
			MergeNetsplitBlock:      big.NewInt(0),
			ShanghaiTime:            newUint64(0),
			CancunTime:              newUint64(0),
			PragueTime:              nil,
			VerkleTime:              nil,
			TerminalTotalDifficulty: common.Big0,
			BlobScheduleConfig: &params.BlobScheduleConfig{
				Cancun: &params.BlobConfig{
					Target:         3,
					Max:            6,
					UpdateFraction: 3338477,
				},
			},
		}
	}

	// Create consensus engine
	var engine consensus.Engine
	if chainConfig.Clique != nil {
		engine = clique.New(chainConfig.Clique, db)
	} else {
		// Use a dummy engine for PoS
		engine = &dummyEngine{}
	}

	// Initialize blockchain
	options := &gethcore.BlockChainConfig{
		TrieCleanLimit: config.TrieCleanCache,
		NoPrefetch:     config.NoPrefetch,
		StateScheme:    rawdb.HashScheme,
	}

	// For network 96369, check for migrated data first
	// DISABLED: The migrated data has issues with the treasury account missing
	// We need to use regular genesis for now
	if false && config != nil && config.NetworkId == 96369 {
		fmt.Printf("Checking for migrated data with 41-byte key format...\n")
		// Migration check disabled due to iterator issues
		fmt.Printf("Migration check skipped - using regular genesis\n")
	}

	// Log genesis info for debugging
	if genesis != nil {
		genesisBlock := genesis.ToBlock()
		fmt.Printf("Genesis block hash: %s\n", genesisBlock.Hash().Hex())
		fmt.Printf("Genesis chain ID: %v\n", genesis.Config.ChainID)
	}

	// Check if we need to initialize genesis first
	stored := rawdb.ReadCanonicalHash(db, 0)
	fmt.Printf("Debug: Reading canonical hash key: %x value: %x err: %v\n",
		canonicalKey(0), stored, nil)

	if stored == (common.Hash{}) {
		// Double check with direct key access for migrated data
		// Use 9-byte canonical key format (no suffix)
		key := canonicalKey(0)
		if val, err := db.Get(key); err == nil && len(val) == 32 {
			copy(stored[:], val)
			fmt.Printf("Found canonical hash with direct key access: %x\n", stored)
		}

		if stored == (common.Hash{}) {
			fmt.Printf("No genesis found in database, will initialize\n")

			// SPECIAL CASE: Check if we're replaying from an existing genesis
			// In this case, the genesis is already written but SetupGenesisBlockWithOverride
			// will fail because it sees a different genesis
			expectedReplayGenesis := common.HexToHash("0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e")
			if header := rawdb.ReadHeader(db, expectedReplayGenesis, 0); header != nil {
				fmt.Printf("Found replay genesis in database, using it directly\n")
				stored = expectedReplayGenesis
				// Don't run SetupGenesisBlockWithOverride
			} else {
				// Create trie database for genesis initialization
				tdb := triedb.NewDatabase(db, triedb.HashDefaults)

				// Initialize genesis block normally
				_, genesisHash, _, err := gethcore.SetupGenesisBlockWithOverride(db, tdb, genesis, nil)
				if err != nil {
					return nil, fmt.Errorf("failed to setup genesis: %w", err)
				}

				if genesisHash != (common.Hash{}) {
					fmt.Printf("Genesis initialized with hash: %s\n", genesisHash.Hex())
				}

				// Check again
				stored = rawdb.ReadCanonicalHash(db, 0)
				fmt.Printf("After setup, canonical hash at 0: %s\n", stored.Hex())
			}
		}
	} else {
		fmt.Printf("Found existing genesis in database: %s\n", stored.Hex())
	}

	// Check for highest block in migrated data
	currentHash := rawdb.ReadHeadBlockHash(db)
	if currentHash == (common.Hash{}) {
		// Try to read from our custom keys
		if val, err := db.Get([]byte("LastBlock")); err == nil && len(val) == 32 {
			copy(currentHash[:], val)
			fmt.Printf("Found head block from LastBlock key: %x\n", currentHash)

			// Write it to the standard location
			rawdb.WriteHeadBlockHash(db, currentHash)
			rawdb.WriteHeadHeaderHash(db, currentHash)
			rawdb.WriteHeadFastBlockHash(db, currentHash)
		}
	}

	if currentHash != (common.Hash{}) {
		if header := rawdb.ReadHeader(db, currentHash, 0); header != nil {
			fmt.Printf("Found header at hash %x with number %d\n", currentHash, header.Number.Uint64())
		} else {
			// Try to read the header by iterating through possible block numbers
			if heightBytes, err := db.Get([]byte("Height")); err == nil && len(heightBytes) == 8 {
				height := binary.BigEndian.Uint64(heightBytes)
				if header := rawdb.ReadHeader(db, currentHash, height); header != nil {
					fmt.Printf("Found header at height %d\n", height)
				}
			}
		}
	}

	// Now create blockchain - it will use the already initialized genesis
	// When genesis is nil and database has genesis, NewBlockChain will use it
	// However, NewBlockChain calls SetupGenesisBlockWithOverride which causes issues
	// when we have a custom genesis already in the database
	// So we need to create the blockchain manually when we have existing genesis

	var blockchain *gethcore.BlockChain
	var err error

	// Check if we already have a genesis in the database
	existingGenesisHash := rawdb.ReadCanonicalHash(db, 0)
	if existingGenesisHash != (common.Hash{}) && genesis == nil {
		// We have genesis in database and no new genesis provided
		// Create blockchain without calling SetupGenesisBlockWithOverride
		fmt.Printf("Creating blockchain with existing genesis: %s\n", existingGenesisHash.Hex())

		// Read the chain config from database
		storedConfig := rawdb.ReadChainConfig(db, existingGenesisHash)
		if storedConfig == nil {
			// No stored config, use our default
			storedConfig = chainConfig
			// Write it to database
			rawdb.WriteChainConfig(db, existingGenesisHash, storedConfig)
		}

		// Create the blockchain directly without genesis setup
		blockchain, err = createBlockchainWithoutGenesis(db, storedConfig, engine, options)
		if err != nil {
			return nil, fmt.Errorf("failed to create blockchain without genesis: %w", err)
		}
	} else {
		// Normal path - let NewBlockChain handle genesis
		blockchain, err = gethcore.NewBlockChain(db, genesis, engine, options)
		if err != nil {
			return nil, fmt.Errorf("failed to create blockchain: %w", err)
		}
	}

	// Create transaction pool
	legacyPool := legacypool.New(config.TxPool, blockchain)
	txPool, err := txpool.New(config.TxPool.PriceLimit, blockchain, []txpool.SubPool{legacyPool})
	if err != nil {
		return nil, err
	}

	return &MinimalEthBackend{
		chainConfig: chainConfig,
		blockchain:  blockchain,
		txPool:      txPool,
		chainDb:     db,
		engine:      engine,
		networkID:   config.NetworkId,
	}, nil
}

// BlockChain returns the blockchain
func (b *MinimalEthBackend) BlockChain() *gethcore.BlockChain {
	return b.blockchain
}

// TxPool returns the transaction pool
func (b *MinimalEthBackend) TxPool() *txpool.TxPool {
	return b.txPool
}

// ChainConfig returns the chain configuration
func (b *MinimalEthBackend) ChainConfig() *params.ChainConfig {
	return b.chainConfig
}

// APIs returns the collection of RPC services the ethereum package offers
func (b *MinimalEthBackend) APIs() []rpc.API {
	// Return basic APIs needed for Ethereum RPC
	return []rpc.API{
		{
			Namespace: "eth",
			Service:   NewEthAPI(b),
			Public:    true,
		},
		{
			Namespace: "net",
			Service:   &NetAPI{networkID: b.networkID},
			Public:    true,
		},
		{
			Namespace: "web3",
			Service:   &Web3API{},
			Public:    true,
		},
	}
}

// createBlockchainWithoutGenesis creates a blockchain using existing genesis in database
// This avoids calling SetupGenesisBlockWithOverride which would fail with genesis mismatch
func createBlockchainWithoutGenesis(db ethdb.Database, chainConfig *params.ChainConfig, engine consensus.Engine, options *gethcore.BlockChainConfig) (*gethcore.BlockChain, error) {
	// The key insight is that NewBlockChain with nil genesis will use what's in the database
	// But it compares against the default mainnet genesis (d4e56740...)
	// We need to make it think our genesis IS the mainnet genesis

	// Get the genesis hash from database
	genesisHash := rawdb.ReadCanonicalHash(db, 0)
	if genesisHash == (common.Hash{}) {
		return nil, fmt.Errorf("no genesis found in database")
	}

	fmt.Printf("Attempting to create blockchain with genesis hash: %s\n", genesisHash.Hex())

	// The issue is that when genesis is nil, NewBlockChain defaults to mainnet genesis
	// and compares it with what's in the database
	// We need to pass nil and hope it accepts what's in the database

	// First ensure the chain config is written
	if rawdb.ReadChainConfig(db, genesisHash) == nil {
		fmt.Printf("Writing chain config for genesis %s\n", genesisHash.Hex())
		rawdb.WriteChainConfig(db, genesisHash, chainConfig)
	}

	// Try to create blockchain with nil genesis
	// This should use what's in the database
	blockchain, err := gethcore.NewBlockChain(db, nil, engine, options)
	if err != nil {
		// If it fails with genesis mismatch, we have a problem
		// The only way around this is to modify the geth code itself
		// or to use the exact genesis that matches our extracted one
		return nil, fmt.Errorf("failed to create blockchain: %w", err)
	}

	return blockchain, nil
}

// dummyEngine is a consensus engine that does nothing (for PoS mode)
type dummyEngine struct{}

func (d *dummyEngine) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (d *dummyEngine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	return nil
}

func (d *dummyEngine) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))
	for range headers {
		results <- nil
	}
	return abort, results
}

func (d *dummyEngine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	return nil
}

func (d *dummyEngine) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	return nil
}

func (d *dummyEngine) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state vm.StateDB, body *types.Body) {
	// No-op for PoS
}

func (d *dummyEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, body *types.Body, receipts []*types.Receipt) (*types.Block, error) {
	// Finalize the state
	d.Finalize(chain, header, state, body)

	// Assemble and return the block
	return types.NewBlock(header, body, receipts, nil), nil
}

func (d *dummyEngine) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	results <- block
	return nil
}

func (d *dummyEngine) SealHash(header *types.Header) common.Hash {
	return header.Hash()
}

func (d *dummyEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (d *dummyEngine) Close() error {
	return nil
}
