// (c) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/luxfi/geth/common"
	gethcore "github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/txpool"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/eth/ethconfig"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/geth/rpc"

	"github.com/luxfi/database"
	"github.com/luxfi/database/pebbledb"
	"github.com/luxfi/ids"
	consensusNode "github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/node/consensus/engine/chain/block"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/version"
)

var (
	_ block.ChainVM = (*VM)(nil)

	errNilBlock     = errors.New("nil block")
	errInvalidBlock = errors.New("invalid block")
)

// VM implements the C-Chain VM interface using geth
type VM struct {
	ctx          *consensusNode.Context
	db           database.Database
	genesisBytes []byte
	lastAccepted ids.ID

	// geth components
	ethConfig   ethconfig.Config
	chainConfig *params.ChainConfig
	genesisHash common.Hash

	// Minimal backend
	backend    *MinimalEthBackend
	txPool     *txpool.TxPool
	blockChain *gethcore.BlockChain

	// Database wrappers
	ethDB ethdb.Database

	// Synchronization
	mu           sync.RWMutex
	building     ids.ID
	builtBlocks  map[ids.ID]*Block
	shutdownChan chan struct{}
}

// Initialize implements the block.ChainVM interface
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *consensusNode.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*core.Fx,
	appSender core.AppSender,
) error {
	vm.ctx = chainCtx
	vm.db = db
	vm.genesisBytes = genesisBytes
	vm.shutdownChan = make(chan struct{})
	vm.builtBlocks = make(map[ids.ID]*Block)

	// MIGRATION DETECTION: Check if we have migrated data BEFORE any initialization
	// We need to check at the C-Chain database level, not the wrapped level
	hasMigratedData := false
	migratedHeight := uint64(0)
	migratedBlockHash := common.Hash{}

	// Create a database wrapper first
	vm.ethDB = WrapDatabase(db)

	// Check for LUX_GENESIS flag to trigger automatic replay
	luxGenesis := os.Getenv("LUX_GENESIS") == "1"
	if luxGenesis {
		fmt.Printf("LUX_GENESIS=1 detected, checking for blocks to replay...\n")
		vm.ctx.Log.Info("LUX_GENESIS mode enabled for automatic block replay")
	}

	// Check environment variables for imported blockchain data
	if importedHeight := os.Getenv("LUX_IMPORTED_HEIGHT"); importedHeight != "" {
		if height, err := strconv.ParseUint(importedHeight, 10, 64); err == nil && height > 0 {
			hasMigratedData = true
			migratedHeight = height

			// Get the block hash if provided
			if importedBlockID := os.Getenv("LUX_IMPORTED_BLOCK_ID"); importedBlockID != "" {
				if blockIDBytes, err := hex.DecodeString(importedBlockID); err == nil && len(blockIDBytes) == 32 {
					copy(migratedBlockHash[:], blockIDBytes)
				}
			}

			fmt.Printf("DETECTED IMPORTED DATA AT HEIGHT %d, HASH %s\n", height, migratedBlockHash.Hex())

			// Log to Avalanche logger too
			vm.ctx.Log.Info("Detected imported blockchain data from environment",
				"height", height,
				"blockHash", migratedBlockHash.Hex(),
			)
		}
	}

	// Fallback: Check for migrated blockchain data in database
	if !hasMigratedData {
		if heightBytes, err := vm.ethDB.Get([]byte("Height")); err == nil && len(heightBytes) == 8 {
			height := binary.BigEndian.Uint64(heightBytes)
			if height > 0 {
				hasMigratedData = true
				migratedHeight = height
				fmt.Printf("DETECTED MIGRATED DATA AT HEIGHT %d\n", height)

				// Log to Avalanche logger too
				vm.ctx.Log.Info("Detected migrated blockchain data",
					"height", height,
				)
			}
		}
	}

	// If we have migrated data, skip normal genesis initialization

	// DEBUG: Log database path and check contents
	fmt.Printf("DEBUG: C-Chain VM Initialize called\n")
	fmt.Printf("DEBUG: Database type: %T\n", db)
	fmt.Printf("DEBUG: Genesis bytes length: %d\n", len(genesisBytes))

	// Parse genesis or use default
	var genesis *gethcore.Genesis
	if len(genesisBytes) > 0 {
		genesis = &gethcore.Genesis{}
		if err := json.Unmarshal(genesisBytes, genesis); err != nil {
			return fmt.Errorf("failed to unmarshal genesis: %w", err)
		}

		// Set terminal total difficulty for PoS transition
		if genesis.Config != nil && genesis.Config.TerminalTotalDifficulty == nil {
			genesis.Config.TerminalTotalDifficulty = common.Big0
		}
	} else {
		// Use a default dev genesis if none provided
		genesis = &gethcore.Genesis{
			Config:     params.AllEthashProtocolChanges,
			Difficulty: big.NewInt(0),
			GasLimit:   8000000,
			Alloc: gethcore.GenesisAlloc{
				common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"): types.Account{
					Balance: new(big.Int).Mul(big.NewInt(1000000), big.NewInt(params.Ether)),
				},
			},
		}
		genesis.Config.TerminalTotalDifficulty = common.Big0
	}

	// Initialize chain config
	vm.chainConfig = genesis.Config
	if vm.chainConfig == nil {
		vm.chainConfig = params.AllEthashProtocolChanges
		genesis.Config = vm.chainConfig
	}

	// Initialize eth config
	vm.ethConfig = ethconfig.Defaults
	vm.ethConfig.Genesis = genesis
	vm.ethConfig.NetworkId = vm.chainConfig.ChainID.Uint64()
	vm.ethConfig.Miner.Etherbase = common.Address{}

	// CRITICAL: For migrated data, we must prevent normal genesis initialization
	if hasMigratedData {
		fmt.Printf("MIGRATION MODE: Skipping genesis, loading from height %d\n", migratedHeight)

		// Set a special genesis that won't overwrite our data
		genesis.Alloc = nil // Clear allocations to prevent overwriting state

		// Mark database as already initialized to prevent SetupGenesisBlock
		// Write a dummy genesis hash to satisfy the check
		if err := vm.ethDB.Put([]byte("genesis"), []byte{1}); err == nil {
			fmt.Printf("Marked database as initialized\n")
		}
	}

	// Create minimal Ethereum backend
	var err error
	if hasMigratedData {
		// CRITICAL: Skip all genesis processing for migrated data
		fmt.Printf("MIGRATION MODE ACTIVE: Loading blockchain from height %d\n", migratedHeight)

		// Create a special backend that doesn't touch genesis
		vm.backend, err = NewMigratedBackend(vm.ethDB, migratedHeight)
		if err != nil {
			return fmt.Errorf("failed to create migrated backend: %w", err)
		}
	} else {
		vm.backend, err = NewMinimalEthBackend(vm.ethDB, &vm.ethConfig, genesis)
	}
	if err != nil {
		return fmt.Errorf("failed to create eth backend: %w", err)
	}

	vm.blockChain = vm.backend.BlockChain()
	vm.txPool = vm.backend.TxPool()

	// Get genesis hash
	genesisBlock := vm.blockChain.Genesis()
	if genesisBlock == nil {
		return fmt.Errorf("genesis block not found")
	}
	vm.genesisHash = genesisBlock.Hash()

	// Check if we have existing blocks beyond genesis
	// If we detected migrated data via environment variables, use that
	if hasMigratedData && migratedBlockHash != (common.Hash{}) {
		vm.lastAccepted = ids.ID(migratedBlockHash)
		vm.ctx.Log.Info("Using imported blockchain data from environment",
			"height", migratedHeight,
			"hash", migratedBlockHash.Hex(),
			"lastAccepted", vm.lastAccepted.String(),
		)

		// Log database status after migration detection
		vm.logDatabaseStatus()
		return nil
	}

	// First check our custom consensus keys for migrated data
	if heightBytes, err := vm.ethDB.Get([]byte("Height")); err == nil && len(heightBytes) == 8 {
		height := binary.BigEndian.Uint64(heightBytes)
		if height > 0 {
			vm.ctx.Log.Info("Found Height consensus key",
				"height", height,
			)

			// Try to get the block hash at this height
			blockNumBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(blockNumBytes, height)

			// Check canonical hash using 9-byte format
			key := canonicalKey(height)

			if hashBytes, err := vm.ethDB.Get(key); err == nil && len(hashBytes) == 32 {
				var hash common.Hash
				copy(hash[:], hashBytes)

				// Force the blockchain to recognize this height
				// Note: SetHead is not the right approach, we need to ensure the blockchain loads the data

				vm.lastAccepted = ids.ID(hash)
				vm.ctx.Log.Info("Found migrated blockchain data",
					"height", height,
					"hash", hash.Hex(),
					"lastAccepted", vm.lastAccepted.String(),
				)

				// Log database status after migration detection
				vm.logDatabaseStatus()
				return nil
			}
		}
	}

	currentBlock := vm.blockChain.CurrentBlock()
	if currentBlock != nil && currentBlock.Number.Uint64() > 0 {
		// We have migrated data, set last accepted to current block
		vm.lastAccepted = ids.ID(currentBlock.Hash())

		vm.ctx.Log.Info("C-Chain VM found existing blockchain data",
			"currentHash", currentBlock.Hash().Hex(),
			"currentHeight", currentBlock.Number.Uint64(),
			"lastAccepted", vm.lastAccepted.String(),
		)
	} else {
		// Fresh start, use genesis
		vm.lastAccepted = ids.ID(vm.genesisHash)

		vm.ctx.Log.Info("C-Chain VM starting from genesis",
			"genesisHash", vm.genesisHash.Hex(),
			"lastAccepted", vm.lastAccepted.String(),
		)

		// If LUX_GENESIS=1 and we're at genesis, check for blocks to replay
		if luxGenesis && (currentBlock == nil || (currentBlock != nil && currentBlock.Number.Uint64() == 0)) {
			vm.ctx.Log.Info("LUX_GENESIS=1 detected at genesis, checking for blocks to replay...")

			// Look for blockchain data in the C-Chain database directory
			// The blockchain data should be in the same database
			if err := vm.replayBlockchainData(); err != nil {
				vm.ctx.Log.Warn("Failed to replay blockchain data",
					"error", err,
				)
			} else {
				// Update current block after replay
				currentBlock = vm.blockChain.CurrentBlock()
				if currentBlock != nil && currentBlock.Number.Uint64() > 0 {
					vm.lastAccepted = ids.ID(currentBlock.Hash())
					vm.ctx.Log.Info("Successfully replayed blockchain data",
						"currentHash", currentBlock.Hash().Hex(),
						"currentHeight", currentBlock.Number.Uint64(),
					)
				}
			}
		}
	}

	// Log database statistics
	vm.logDatabaseStatus()

	vm.ctx.Log.Info("C-Chain VM initialized")
	vm.ctx.Log.Info("Chain configuration",
		"chainID", vm.chainConfig.ChainID.String(),
		"genesisHash", vm.genesisHash.Hex(),
	)

	return nil
}

// logDatabaseStatus logs information about the current database state
func (vm *VM) logDatabaseStatus() {
	// Get current block info
	currentBlock := vm.blockChain.CurrentBlock()
	if currentBlock != nil {
		vm.ctx.Log.Info("Current blockchain state",
			"height", currentBlock.Number.Uint64(),
			"hash", currentBlock.Hash().Hex(),
			"timestamp", currentBlock.Time,
		)
	}

	// Get head block info
	headBlock := vm.blockChain.CurrentHeader()
	if headBlock != nil {
		vm.ctx.Log.Info("Head block state",
			"height", headBlock.Number.Uint64(),
			"hash", headBlock.Hash().Hex(),
		)
	}

	// Log database type
	vm.ctx.Log.Info("Database info",
		"type", fmt.Sprintf("%T", vm.ethDB),
	)
}

// SetState implements the block.ChainVM interface
func (vm *VM) SetState(ctx context.Context, state consensusNode.State) error {
	return nil
}

// Shutdown implements the block.ChainVM interface
func (vm *VM) Shutdown(ctx context.Context) error {
	return nil
}

// Version implements the block.ChainVM interface
func (vm *VM) Version(ctx context.Context) (string, error) {
	return "1.0.0", nil
}

// CreateHandlers implements the block.ChainVM interface
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	handlers := make(map[string]http.Handler)

	// Create RPC server and register APIs
	rpcServer := rpc.NewServer()

	// Manually register our minimal APIs to avoid any auto-start issues
	ethAPI := NewEthAPI(vm.backend)
	netAPI := &NetAPI{networkID: vm.ethConfig.NetworkId}
	web3API := &Web3API{}

	// Register each API namespace
	if err := rpcServer.RegisterName("eth", ethAPI); err != nil {
		return nil, fmt.Errorf("failed to register eth API: %w", err)
	}
	if err := rpcServer.RegisterName("net", netAPI); err != nil {
		return nil, fmt.Errorf("failed to register net API: %w", err)
	}
	if err := rpcServer.RegisterName("web3", web3API); err != nil {
		return nil, fmt.Errorf("failed to register web3 API: %w", err)
	}

	vm.ctx.Log.Info("Registered API namespaces")

	// Create HTTP handler
	httpHandler := rpcServer

	// Register the handler at both /rpc and / for compatibility
	handlers["/rpc"] = httpHandler
	handlers["/"] = httpHandler

	vm.ctx.Log.Info("Created RPC handlers")

	return handlers, nil
}

// NewHTTPHandler implements the block.ChainVM interface
func (vm *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	return nil, nil
}

// WaitForEvent implements the block.ChainVM interface
func (vm *VM) WaitForEvent(ctx context.Context) (core.Message, error) {
	<-ctx.Done()
	return core.PendingTxs, ctx.Err()
}

// HealthCheck implements the block.ChainVM interface
func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	return map[string]string{"status": "healthy"}, nil
}

// Connected implements the block.ChainVM interface
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	return nil
}

// Disconnected implements the block.ChainVM interface
func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return nil
}

// GetBlock implements the block.ChainVM interface
func (vm *VM) GetBlock(ctx context.Context, blkID ids.ID) (chain.Block, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	// Check if it's a built block
	if blk, ok := vm.builtBlocks[blkID]; ok {
		return blk, nil
	}

	// Get block from blockchain
	hash := common.Hash(blkID)
	ethBlock := vm.blockChain.GetBlockByHash(hash)
	if ethBlock == nil {
		return nil, database.ErrNotFound
	}

	return vm.newBlock(ethBlock)
}

// ParseBlock implements the block.ChainVM interface
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (chain.Block, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(blockBytes, ethBlock); err != nil {
		return nil, fmt.Errorf("failed to decode block: %w", err)
	}

	return vm.newBlock(ethBlock)
}

// BuildBlock implements the block.ChainVM interface
func (vm *VM) BuildBlock(ctx context.Context) (chain.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Get current block as parent
	parent := vm.blockChain.CurrentBlock()
	if parent == nil {
		return nil, fmt.Errorf("no parent block available")
	}

	// Create a new block header
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   parent.GasLimit,
		Time:       uint64(time.Now().Unix()),
		Coinbase:   vm.ethConfig.Miner.Etherbase,
		Difficulty: big.NewInt(1), // PoS difficulty
		MixDigest:  common.Hash{},
		Nonce:      types.EncodeNonce(0),
		Extra:      []byte{},
		BaseFee:    parent.BaseFee,
	}

	// Get pending transactions from the pool
	pending := vm.txPool.Pending(txpool.PendingFilter{})
	var txs []*types.Transaction
	for _, batch := range pending {
		for _, lazyTx := range batch {
			// Resolve the lazy transaction
			tx := lazyTx.Resolve()
			if tx != nil {
				txs = append(txs, tx)
			}
		}
	}

	// Create a new block with transactions
	block := types.NewBlock(header, &types.Body{
		Transactions: txs,
		Uncles:       []*types.Header{},
		Withdrawals:  []*types.Withdrawal{},
	}, []*types.Receipt{}, nil)

	// Create a new block wrapper
	blk, err := vm.newBlock(block)
	if err != nil {
		return nil, err
	}

	// Store built block
	vm.builtBlocks[blk.ID()] = blk
	vm.building = blk.ID()

	return blk, nil
}

// AppGossip implements the block.ChainVM interface
func (vm *VM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return nil
}

// AppRequest implements the block.ChainVM interface
func (vm *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return nil
}

// AppRequestFailed implements the block.ChainVM interface
func (vm *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *core.AppError) error {
	return nil
}

// AppResponse implements the block.ChainVM interface
func (vm *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return nil
}

// CrossChainAppRequest implements the block.ChainVM interface
func (vm *VM) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	return nil
}

// CrossChainAppRequestFailed implements the block.ChainVM interface
func (vm *VM) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32, appErr *core.AppError) error {
	return nil
}

// CrossChainAppResponse implements the block.ChainVM interface
func (vm *VM) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	return nil
}

// SetPreference implements the block.ChainVM interface
func (vm *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	return nil
}

// LastAccepted implements the block.ChainVM interface
func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	return vm.lastAccepted, nil
}

// GetBlockIDAtHeight implements the block.ChainVM interface
func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	block := vm.blockChain.GetBlockByNumber(height)
	if block == nil {
		return ids.Empty, database.ErrNotFound
	}
	return ids.ID(block.Hash()), nil
}

// replayBlockchainData reads imported blockchain data and replays it into the C-Chain
func (vm *VM) replayBlockchainData() error {
	vm.ctx.Log.Info("Starting blockchain data replay...")

	// Look for import path from environment variable
	importPath := os.Getenv("LUX_GENESIS_IMPORT_PATH")
	if importPath == "" {
		// Use relative state repo path for original blockchain data
		importPath = "../genesis/state/processed/lux-mainnet-96369/C/db"
		if _, err := os.Stat(importPath); os.IsNotExist(err) {
			// Try the symlinked path
			importPath = "../state/chaindata/lux-mainnet-96369/db/pebbledb"
			if _, err := os.Stat(importPath); os.IsNotExist(err) {
				return fmt.Errorf("blockchain import path not found, set LUX_GENESIS_IMPORT_PATH")
			}
		}
	}

	vm.ctx.Log.Info("Opening import database", "path", importPath)

	// Open the import database using PebbleDB
	importDB, err := pebbledb.New(importPath, 256, 256, "importDB", true)
	if err != nil {
		return fmt.Errorf("failed to open import database: %w", err)
	}
	defer importDB.Close()

	// Check for the highest block number in the import database
	highestBlock := uint64(0)

	// First, check for our custom Height key
	if heightBytes, err := importDB.Get([]byte("Height")); err == nil && len(heightBytes) == 8 {
		highestBlock = binary.BigEndian.Uint64(heightBytes)
		vm.ctx.Log.Info("Found Height key", "height", highestBlock)
	}

	if highestBlock == 0 {
		// Scan for canonical blocks to find the highest
		iter := importDB.NewIteratorWithStartAndPrefix([]byte("h"), nil)
		defer iter.Release()

		for iter.Next() {
			key := iter.Key()
			// Look for canonical block keys: h + num (8 bytes) + n
			if len(key) == 10 && key[0] == 'h' && key[9] == 'n' {
				blockNum := binary.BigEndian.Uint64(key[1:9])
				if blockNum > highestBlock {
					highestBlock = blockNum
				}
			}
		}

		if highestBlock == 0 {
			return fmt.Errorf("no blocks found to replay")
		}

		vm.ctx.Log.Info("Found highest block by scanning", "height", highestBlock)
	}

	// First, log consensus parameters being used
	vm.ctx.Log.Info("Consensus parameters for C-Chain",
		"networkID", vm.ctx.NetworkID,
		"subnetID", vm.ctx.SubnetID,
		"chainID", vm.ctx.ChainID,
	)
	
	// Note: The consensus parameters are set at the node level, not the VM level
	// For single-node operation, they should be k=1, alpha=1, beta=1
	
	// First, let's debug what keys exist in the database
	vm.ctx.Log.Info("Debugging import database keys...")
	
	// Iterate through all keys to see the format
	debugIter := importDB.NewIterator()
	defer debugIter.Release()
	
	keyTypes := make(map[string]int)
	sampleKeys := []string{}
	canonicalFound := 0
	headerFound := 0
	bodyFound := 0
	count := 0
	
	for debugIter.Next() && count < 1000 {
		key := debugIter.Key()
		if len(key) > 0 {
			// Count key types by first byte
			if len(key) >= 1 {
				firstByte := key[0]
				keyTypes[fmt.Sprintf("%02x", firstByte)]++
			}
			
			// Sample some keys for debugging
			if count < 20 {
				sampleKeys = append(sampleKeys, fmt.Sprintf("key[%d]: %x", len(key), key))
			}
			
			// Look for canonical keys (h + 8 bytes + n)
			if len(key) == 10 && key[0] == 0x68 && key[9] == 0x6e {
				blockNum := binary.BigEndian.Uint64(key[1:9])
				if canonicalFound < 5 {
					vm.ctx.Log.Info("Found canonical key", "blockNum", blockNum, "key", fmt.Sprintf("%x", key))
				}
				canonicalFound++
			}
			
			// Also look for block body keys to understand structure
			if len(key) >= 41 && key[0] == 0x62 {
				blockNum := binary.BigEndian.Uint64(key[1:9])
				if blockNum <= 5 {
					vm.ctx.Log.Info("Found body key", "blockNum", blockNum, "keyLen", len(key), "key", fmt.Sprintf("%x", key[:20]))
				}
			}
			
			// Look for header keys (H + 32 bytes)
			if len(key) == 33 && key[0] == 0x48 {
				headerFound++
			}
			
			// Look for body keys (b + 32 bytes)
			if len(key) == 33 && key[0] == 0x62 {
				bodyFound++
			}
		}
		count++
		}
	
	vm.ctx.Log.Info("Database key analysis",
		"totalScanned", count,
		"keyTypes", keyTypes,
		"canonicalKeys", canonicalFound,
		"headerKeys", headerFound,
		"bodyKeys", bodyFound,
	)
	for i, sample := range sampleKeys {
		vm.ctx.Log.Info(fmt.Sprintf("Sample key %d", i), "data", sample)
	}
	
	// Now replay blocks from 1 to highestBlock
	vm.ctx.Log.Info("Replaying blocks", "from", 1, "to", highestBlock)

	// Track progress
	startTime := time.Now()
	lastLogTime := startTime
	blocksProcessed := uint64(0)
	blocksInserted := uint64(0)

	batchSize := uint64(1000)
	for start := uint64(1); start <= highestBlock; start += batchSize {
		end := start + batchSize - 1
		if end > highestBlock {
			end = highestBlock
		}

		batchStart := time.Now()
		batchProcessed := 0
		batchInserted := 0

		// Process blocks in batch
		for blockNum := start; blockNum <= end; blockNum++ {
			// Get canonical hash using the correct format
			canonicalKey := canonicalKey(blockNum)
			
			// Debug first few keys
			if blockNum <= 5 {
				vm.ctx.Log.Info("Looking for canonical key", 
					"blockNum", blockNum, 
					"key", fmt.Sprintf("%x", canonicalKey))
			}
			
			hashBytes, err := importDB.Get(canonicalKey)
			if err != nil {
				// For debugging, check if we have a body key instead
				if blockNum <= 5 {
					// Try to find any key for this block number
					bodyPrefix := make([]byte, 9)
					bodyPrefix[0] = 0x62 // 'b'
					binary.BigEndian.PutUint64(bodyPrefix[1:], blockNum)
					
					// Create an iterator with this prefix
					iter := importDB.NewIteratorWithStartAndPrefix(bodyPrefix, nil)
					if iter.Next() {
						vm.ctx.Log.Info("Found body key for block", 
							"blockNum", blockNum,
							"key", fmt.Sprintf("%x", iter.Key()))
					}
					iter.Release()
				}
				continue // Skip missing blocks
			}

			var blockHash common.Hash
			copy(blockHash[:], hashBytes)

			// Get block header
			headerKey := append([]byte("H"), hashBytes...)
			headerData, err := importDB.Get(headerKey)
			if err != nil {
				vm.ctx.Log.Warn("Missing header", "number", blockNum)
				continue
			}

			// Get block body
			bodyKey := append([]byte("b"), hashBytes...)
			bodyData, err := importDB.Get(bodyKey)
			if err != nil {
				vm.ctx.Log.Warn("Missing body", "number", blockNum)
				continue
			}

			// Decode header
			header := new(types.Header)
			if err := rlp.DecodeBytes(headerData, header); err != nil {
				vm.ctx.Log.Error("Failed to decode header",
					"number", blockNum,
					"error", err)
				continue
			}

			// Decode body
			body := new(types.Body)
			if err := rlp.DecodeBytes(bodyData, body); err != nil {
				vm.ctx.Log.Error("Failed to decode body",
					"number", blockNum,
					"error", err)
				continue
			}

			// Reconstruct block
			block := types.NewBlock(header, body, nil, nil)

			// Insert block into blockchain
			if _, err := vm.blockChain.InsertChain([]*types.Block{block}); err != nil {
				vm.ctx.Log.Error("Failed to insert block",
					"number", blockNum,
					"hash", block.Hash().Hex(),
					"error", err)
				continue
			}

			batchProcessed++
			batchInserted++
			blocksProcessed++
			blocksInserted++

			// Update lastAccepted periodically
			if blockNum%1000 == 0 {
				vm.lastAccepted = ids.ID(block.Hash())
			}

			// Log progress every 5 seconds
			if time.Since(lastLogTime) > 5*time.Second {
				elapsed := time.Since(startTime)
				blocksPerSec := float64(blocksProcessed) / elapsed.Seconds()
				remaining := highestBlock - blockNum
				eta := time.Duration(float64(remaining) / blocksPerSec * float64(time.Second))
				
				// Check current blockchain height
				currentHeight := vm.blockChain.CurrentBlock().Number.Uint64()
				
				vm.ctx.Log.Info("Blockchain replay progress",
					"processed", blockNum,
					"inserted", blocksInserted,
					"currentHeight", currentHeight,
					"target", highestBlock,
					"percent", fmt.Sprintf("%.2f%%", float64(blockNum)*100/float64(highestBlock)),
					"blocks/sec", fmt.Sprintf("%.0f", blocksPerSec),
					"eta", eta.Round(time.Second).String(),
				)
				lastLogTime = time.Now()
			}
		}

		// Log batch completion
		batchDuration := time.Since(batchStart)
		vm.ctx.Log.Info("Batch completed",
			"batch", fmt.Sprintf("%d-%d", start, end),
			"processed", batchProcessed,
			"inserted", batchInserted,
			"duration", batchDuration.Round(time.Millisecond),
			"rate", fmt.Sprintf("%.0f blocks/sec", float64(batchProcessed)/batchDuration.Seconds()),
		)
	}

	// Update to the final block
	finalKey := canonicalKey(highestBlock)
	if hashBytes, err := importDB.Get(finalKey); err == nil {
		var finalHash common.Hash
		copy(finalHash[:], hashBytes)
		vm.lastAccepted = ids.ID(finalHash)

		vm.ctx.Log.Info("Blockchain replay completed",
			"finalHeight", highestBlock,
			"finalHash", finalHash.Hex())
	}

	return nil
}
