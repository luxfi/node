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
	fmt.Printf("Creating migrated backend for subnet-EVM data at height %d\n", migratedHeight)
	
	// Enable subnet namespace handling in database wrapper
	UseSubnetNamespace = true
	
	// For now, we'll use the wrapped database
	// Later we can optimize to open raw database directly
	rawDB := db
	
	// Create chain config for LUX mainnet
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
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: common.Big0,
	}
	
	// Create a dummy consensus engine
	engine := &dummyEngine{}
	
	fmt.Printf("Scanning subnet-EVM database for blocks...\n")
	
	// Build block index first
	blocksByNumber := make(map[uint64]common.Hash)
	headersByHash := make(map[common.Hash][]byte)
	
	// We need to access the raw database, not the wrapped one
	// The wrapped database won't see the namespace-prefixed keys
	// So we'll scan for canonical mappings first
	canonicalCount := 0
	
	// Look for 'H' canonical mappings (blockNum -> hash)
	// When using wrapped database, keys are without namespace
	for blockNum := uint64(0); blockNum <= 1082781; blockNum++ {
		// Build the key: just 'H' + blockNum (namespace handled by wrapper)
		key := make([]byte, 9)
		key[0] = 'H'
		binary.BigEndian.PutUint64(key[1:9], blockNum)
		
		// Try to get the hash for this block number
		if val, err := rawDB.Get(key); err == nil && len(val) == 32 {
			var hash common.Hash
			copy(hash[:], val)
			blocksByNumber[blockNum] = hash
			canonicalCount++
			
			if canonicalCount <= 10 || canonicalCount%100000 == 0 {
				fmt.Printf("  Found canonical block %d -> hash %x\n", blockNum, hash[:8])
			}
			
			// Now get the header for this hash
			// With wrapped database, just use the hash directly
			headerKey := make([]byte, 32)
			copy(headerKey, hash[:])
			
			if headerVal, err := rawDB.Get(headerKey); err == nil {
				headersByHash[hash] = headerVal
			}
		}
		
		// Stop early if we haven't found blocks in a while
		if blockNum > 100 && canonicalCount == 0 {
			break
		}
	}
	
	fmt.Printf("Found %d canonical blocks in subnet-EVM database\n", canonicalCount)
	
	// Close the raw database if we opened it
	if rawDB != db {
		rawDB.Close()
	}
	
	fmt.Printf("Found %d blocks in subnet-EVM database\n", len(blocksByNumber))
	
	// Find the requested block or highest available
	var headHash common.Hash
	var actualHeight uint64
	
	if hash, exists := blocksByNumber[migratedHeight]; exists {
		headHash = hash
		actualHeight = migratedHeight
	} else {
		// Find highest block
		for num, hash := range blocksByNumber {
			if num > actualHeight {
				actualHeight = num
				headHash = hash
			}
		}
		fmt.Printf("Block %d not found, using highest block %d\n", migratedHeight, actualHeight)
	}
	
	if headHash == (common.Hash{}) {
		return nil, fmt.Errorf("no blocks found in subnet-EVM database")
	}
	
	fmt.Printf("Setting head to block %d with hash: %x\n", actualHeight, headHash)
	
	// Write canonical mappings in standard format for all blocks
	fmt.Printf("Writing canonical mappings for %d blocks...\n", len(blocksByNumber))
	for blockNum, hash := range blocksByNumber {
		rawdb.WriteCanonicalHash(db, hash, blockNum)
		// Also write the header in standard format
		if _, exists := headersByHash[hash]; exists {
			rawdb.WriteHeader(db, &types.Header{Number: big.NewInt(int64(blockNum))})
		}
	}
	
	// Set head pointers
	rawdb.WriteHeadBlockHash(db, headHash)
	rawdb.WriteHeadHeaderHash(db, headHash)
	rawdb.WriteHeadFastBlockHash(db, headHash)
	rawdb.WriteLastPivotNumber(db, actualHeight)
	
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
		// Use a default config for migrated data
		chainConfig = &params.ChainConfig{
			ChainID: big.NewInt(96369),
			TerminalTotalDifficulty: common.Big0,
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
	// If no genesis is provided, use a default dev genesis
	if genesis == nil {
		genesis = &gethcore.Genesis{
			Config: params.AllEthashProtocolChanges,
			Difficulty: big.NewInt(1),
			GasLimit: 8000000,
			Alloc: gethcore.GenesisAlloc{
				// Default test account with some balance
				common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"): types.Account{
					Balance: new(big.Int).Mul(big.NewInt(1000000), big.NewInt(params.Ether)),
				},
			},
		}
	}
	
	chainConfig := genesis.Config
	if chainConfig == nil {
		chainConfig = params.AllEthashProtocolChanges
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
			
			// Create trie database for genesis initialization
			tdb := triedb.NewDatabase(db, triedb.HashDefaults)
			
			// Initialize genesis block
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
	blockchain, err := gethcore.NewBlockChain(db, nil, engine, options)
	if err != nil {
		// If it fails, try with genesis parameter
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