// (c) 2024 Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/dgraph-io/badger/v4"
	"github.com/holiman/uint256"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/consensus"
	"github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/state"
	"github.com/luxfi/geth/core/state/snapshot"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/core/vm"
	"github.com/luxfi/geth/crypto"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/geth/triedb"
)

// EVMHeader is the EVM header format with additional optional fields
type EVMHeader struct {
	ParentHash      common.Hash      `json:"parentHash"       gencodec:"required"`
	UncleHash       common.Hash      `json:"sha3Uncles"       gencodec:"required"`
	Coinbase        common.Address   `json:"miner"`
	Root            common.Hash      `json:"stateRoot"        gencodec:"required"`
	TxHash          common.Hash      `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash     common.Hash      `json:"receiptsRoot"     gencodec:"required"`
	Bloom           types.Bloom      `json:"logsBloom"        gencodec:"required"`
	Difficulty      *big.Int         `json:"difficulty"       gencodec:"required"`
	Number          *big.Int         `json:"number"           gencodec:"required"`
	GasLimit        uint64           `json:"gasLimit"         gencodec:"required"`
	GasUsed         uint64           `json:"gasUsed"          gencodec:"required"`
	Time            uint64           `json:"timestamp"        gencodec:"required"`
	Extra           []byte           `json:"extraData"        gencodec:"required"`
	MixDigest       common.Hash      `json:"mixHash"`
	Nonce           types.BlockNonce `json:"nonce"`
	BaseFee         *big.Int         `json:"baseFeePerGas"    rlp:"optional"`
	WithdrawalsHash common.Hash      `json:"withdrawalsRoot"  rlp:"optional"`
	BlockGasCost    *big.Int         `rlp:"optional"`
	ExtDataHash     common.Hash      `rlp:"optional"`
}

// DatabaseBackend represents the storage backend type
type DatabaseBackend string

const (
	// PebbleBackend uses PebbleDB
	PebbleBackend DatabaseBackend = "pebbledb"
	// BadgerBackend uses BadgerDB
	BadgerBackend DatabaseBackend = "badgerdb"
	// AutoDetectBackend will attempt to detect the backend type
	AutoDetectBackend DatabaseBackend = "auto"
)

// DatabaseType represents the type of database to replay from
type DatabaseType string

const (
	// StandardDB is a regular geth/coreth database without namespacing
	StandardDB DatabaseType = "standard"
	// NamespacedDB is a EVM database with 32-byte namespace prefix
	NamespacedDB DatabaseType = "namespaced"
	// AutoDetect will attempt to detect the database type
	AutoDetect DatabaseType = "auto"
)

// UnifiedReplayConfig holds configuration for database replay
type UnifiedReplayConfig struct {
	SourcePath               string          // Path to source database
	DatabaseBackend          DatabaseBackend // Storage backend type (pebbledb/badgerdb/auto)
	DatabaseType             DatabaseType    // Type of source database
	Namespace                []byte          // Namespace for namespaced databases (optional)
	TestMode                 bool            // If true, only replay first 100 blocks
	TestLimit                uint64          // Number of blocks to replay in test mode
	CopyAllState             bool            // If true, copy all state data (can be large)
	MaxStateNodes            uint64          // Maximum state nodes to copy (0 = unlimited)
	ExtractGenesisFromSource bool            // If true, extract genesis from block 0
	ParallelWorkers          int             // Number of parallel workers for processing (default: 8)
	BatchSize                int             // Batch size for database writes (default: 100000)
	TargetWallet             common.Address  // Wallet to track balance changes
	TargetHeight             uint64          // Target block height to process to
	ChainConfig              *params.ChainConfig // Chain configuration for EVM
	ReplayTransactions       bool            // If true, replay transactions through EVM (default: true)
	VerifyStateRoots         bool            // If true, verify state roots after each block
	LogInterval              uint64          // Log progress every N blocks (default: 1000)
}

// EVMReplayProgress tracks the progress of replaying blocks (renamed to avoid conflict)
type EVMReplayProgress struct {
	StartTime          time.Time
	CurrentBlock       uint64
	TotalBlocks        uint64
	ProcessedTxs       uint64
	FailedTxs          uint64
	StateVerifications uint64
	TargetWalletBalance *uint256.Int
	LastLogTime        time.Time
	mu                 sync.RWMutex
}

// SourceDatabase is an interface for different database backends
type SourceDatabase interface {
	Get(key []byte) ([]byte, io.Closer, error)
	Close() error
	NewIterator(prefix []byte, upperBound []byte) (DatabaseIterator, error)
}

// DatabaseIterator abstracts iteration over database keys
type DatabaseIterator interface {
	First() bool
	Valid() bool
	Next() bool
	Key() []byte
	Value() []byte
	Close()
}

// pebbleDBWrapper wraps PebbleDB to implement SourceDatabase
type pebbleDBWrapper struct {
	db *pebble.DB
}

func (p *pebbleDBWrapper) Get(key []byte) ([]byte, io.Closer, error) {
	return p.db.Get(key)
}

func (p *pebbleDBWrapper) Close() error {
	return p.db.Close()
}

func (p *pebbleDBWrapper) NewIterator(prefix []byte, upperBound []byte) (DatabaseIterator, error) {
	iter, err := p.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, err
	}
	return &pebbleIterWrapper{iter: iter}, nil
}

type pebbleIterWrapper struct {
	iter *pebble.Iterator
}

func (p *pebbleIterWrapper) First() bool       { return p.iter.First() }
func (p *pebbleIterWrapper) Valid() bool        { return p.iter.Valid() }
func (p *pebbleIterWrapper) Next() bool         { return p.iter.Next() }
func (p *pebbleIterWrapper) Key() []byte        { return p.iter.Key() }
func (p *pebbleIterWrapper) Value() []byte      { return p.iter.Value() }
func (p *pebbleIterWrapper) Close()             { p.iter.Close() }

// badgerDBWrapper wraps BadgerDB to implement SourceDatabase
type badgerDBWrapper struct {
	db *badger.DB
}

func (b *badgerDBWrapper) Get(key []byte) ([]byte, io.Closer, error) {
	var value []byte
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		value, err = item.ValueCopy(nil)
		return err
	})
	return value, nil, err
}

func (b *badgerDBWrapper) Close() error {
	return b.db.Close()
}

func (b *badgerDBWrapper) NewIterator(prefix []byte, upperBound []byte) (DatabaseIterator, error) {
	txn := b.db.NewTransaction(false)
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	iter := txn.NewIterator(opts)
	return &badgerIterWrapper{iter: iter, txn: txn}, nil
}

type badgerIterWrapper struct {
	iter *badger.Iterator
	txn  *badger.Txn
}

func (b *badgerIterWrapper) First() bool {
	b.iter.Rewind()
	return b.iter.Valid()
}

func (b *badgerIterWrapper) Valid() bool { return b.iter.Valid() }
func (b *badgerIterWrapper) Next() bool {
	b.iter.Next()
	return b.iter.Valid()
}
func (b *badgerIterWrapper) Key() []byte { return b.iter.Item().Key() }
func (b *badgerIterWrapper) Value() []byte {
	val, _ := b.iter.Item().ValueCopy(nil)
	return val
}
func (b *badgerIterWrapper) Close() {
	b.iter.Close()
	b.txn.Discard()
}

// UnifiedReplayer handles replaying blocks from various database formats
type UnifiedReplayer struct {
	config       *UnifiedReplayConfig
	targetDB     ethdb.Database
	blockchain   *core.BlockChain
	sourceDB     SourceDatabase
	namespace    []byte
	isNamespaced bool
	progress     *EVMReplayProgress
	stateDB      *state.StateDB
	stateCache   state.Database
	chainConfig  *params.ChainConfig
	engine       consensus.Engine
	vmConfig     vm.Config
	trieDB       *triedb.Database
	snapshots    *snapshot.Tree
	useExternalTrieDB bool  // If true, don't close trieDB on cleanup
}

// NewUnifiedReplayer creates a new unified database replayer (backward compatibility)
func NewUnifiedReplayer(config *UnifiedReplayConfig, targetDB ethdb.Database, blockchain *core.BlockChain) (*UnifiedReplayer, error) {
	return NewUnifiedReplayerWithTrieDB(config, targetDB, blockchain, nil, nil)
}

// NewUnifiedReplayerWithTrieDB creates a new unified database replayer with optional external trie database
func NewUnifiedReplayerWithTrieDB(config *UnifiedReplayConfig, targetDB ethdb.Database, blockchain *core.BlockChain, trieDB *triedb.Database, stateCache state.Database) (*UnifiedReplayer, error) {
	log.Printf("NewUnifiedReplayer: Starting initialization for replay to height %d", config.TargetHeight)

	if config.SourcePath == "" {
		return nil, fmt.Errorf("source database path is required")
	}

	// Set defaults
	if config.TestMode && config.TestLimit == 0 {
		config.TestLimit = 100
	}
	if config.MaxStateNodes == 0 && !config.CopyAllState {
		config.MaxStateNodes = 1000000 // Default to 1M nodes if not copying all
	}
	if config.ParallelWorkers == 0 {
		config.ParallelWorkers = 8
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100000
	}
	if config.LogInterval == 0 {
		config.LogInterval = 1000
	}
	if config.ReplayTransactions {
		// Default to replaying transactions for proper state building
		config.ReplayTransactions = true
	}

	// Set chain config if not provided
	if config.ChainConfig == nil {
		config.ChainConfig = &params.ChainConfig{
			ChainID:             big.NewInt(96369), // LUX mainnet
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			BerlinBlock:         big.NewInt(0),
			LondonBlock:         big.NewInt(0),
		}
	}

	// Detect or open database backend
	backend := config.DatabaseBackend
	if backend == AutoDetectBackend || backend == "" {
		detectedBackend, err := detectDatabaseBackend(config.SourcePath)
		if err != nil {
			return nil, fmt.Errorf("failed to detect database backend: %v", err)
		}
		backend = detectedBackend
		log.Printf("Auto-detected database backend: %s", backend)
	}

	// Open source database with appropriate backend
	var sourceDB SourceDatabase
	switch backend {
	case BadgerBackend:
		log.Printf("Opening BadgerDB at %s", config.SourcePath)
		opts := badger.DefaultOptions(config.SourcePath)
		opts.ReadOnly = true
		opts.Logger = nil // Disable BadgerDB logging
		db, err := badger.Open(opts)
		if err != nil {
			return nil, fmt.Errorf("cannot open BadgerDB at %s: %v", config.SourcePath, err)
		}
		sourceDB = &badgerDBWrapper{db: db}
		log.Printf("Successfully opened BadgerDB")

	case PebbleBackend:
		log.Printf("Opening PebbleDB at %s", config.SourcePath)
		db, err := pebble.Open(config.SourcePath, &pebble.Options{ReadOnly: true})
		if err != nil {
			return nil, fmt.Errorf("cannot open PebbleDB at %s: %v", config.SourcePath, err)
		}
		sourceDB = &pebbleDBWrapper{db: db}
		log.Printf("Successfully opened PebbleDB")

	default:
		return nil, fmt.Errorf("unsupported database backend: %s", backend)
	}

	// Initialize trie database and state cache
	// CRITICAL FIX: Use blockchain's existing trie database if provided
	var useExternalTrieDB bool
	if trieDB == nil {
		// Create our own trie database if not provided
		trieDB = triedb.NewDatabase(targetDB, nil)
		useExternalTrieDB = false
		log.Printf("Creating new trie database for replay")
	} else {
		// Use the provided trie database from the blockchain
		useExternalTrieDB = true
		log.Printf("Using blockchain's existing trie database for replay")
	}

	// Create snapshots if not provided with state cache
	var snapshots *snapshot.Tree
	if stateCache == nil {
		snapshots, _ = snapshot.New(snapshot.Config{
			CacheSize:  256,
			Recovery:   true,
			NoBuild:    false,
			AsyncBuild: true,
		}, targetDB, trieDB, common.Hash{})
		stateCache = state.NewDatabase(trieDB, snapshots)
		log.Printf("Created new state cache for replay")
	} else {
		log.Printf("Using blockchain's existing state cache for replay")
	}

	replayer := &UnifiedReplayer{
		config:      config,
		targetDB:    targetDB,
		blockchain:  blockchain,
		sourceDB:    sourceDB,
		chainConfig: config.ChainConfig,
		vmConfig:    vm.Config{},
		trieDB:      trieDB,
		snapshots:   snapshots,
		stateCache:  stateCache,
		useExternalTrieDB: useExternalTrieDB,
	}

	// Initialize progress tracker
	replayer.progress = &EVMReplayProgress{
		StartTime:           time.Now(),
		TotalBlocks:         config.TargetHeight,
		TargetWalletBalance: uint256.NewInt(0),
		LastLogTime:         time.Now(),
	}

	// Auto-detect database type if needed
	if config.DatabaseType == AutoDetect {
		if err := replayer.detectDatabaseType(); err != nil {
			sourceDB.Close()
			return nil, err
		}
	} else {
		replayer.isNamespaced = (config.DatabaseType == NamespacedDB)
		if replayer.isNamespaced && len(config.Namespace) == 0 {
			// Use default LUX net namespace
			replayer.namespace = []byte{
				0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
				0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
				0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
				0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
			}
		} else {
			replayer.namespace = config.Namespace
		}
	}

	return replayer, nil
}

// detectDatabaseBackend attempts to detect the database backend type
func detectDatabaseBackend(dbPath string) (DatabaseBackend, error) {
	// Check for BadgerDB manifest file
	manifestPath := filepath.Join(dbPath, "MANIFEST")
	if _, err := os.Stat(manifestPath); err == nil {
		// Read first few bytes to check for BadgerDB magic
		file, err := os.Open(manifestPath)
		if err != nil {
			return "", fmt.Errorf("failed to open MANIFEST: %v", err)
		}
		defer file.Close()

		magic := make([]byte, 4)
		if _, err := file.Read(magic); err != nil {
			return "", fmt.Errorf("failed to read MANIFEST: %v", err)
		}

		// BadgerDB manifest starts with "Bdgr" magic bytes
		if string(magic) == "Bdgr" {
			return BadgerBackend, nil
		}
	}

	// Check for BadgerDB .vlog files (another indicator)
	files, err := os.ReadDir(dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %v", err)
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".vlog" {
			return BadgerBackend, nil
		}
		// PebbleDB uses .sst files but so does BadgerDB, so check for PebbleDB-specific files
		if file.Name() == "CURRENT" {
			// Read CURRENT file to check for PebbleDB
			currentPath := filepath.Join(dbPath, "CURRENT")
			data, err := os.ReadFile(currentPath)
			if err == nil && len(data) > 0 {
				// PebbleDB CURRENT file contains MANIFEST filename
				if !contains(string(data), "Bdgr") {
					return PebbleBackend, nil
				}
			}
		}
	}

	// Default to BadgerDB if we find .sst files but no clear PebbleDB indicators
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".sst" {
			return BadgerBackend, nil
		}
	}

	return "", fmt.Errorf("unable to detect database backend")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}

// detectDatabaseType attempts to detect if the database is namespaced or standard
func (r *UnifiedReplayer) detectDatabaseType() error {
	log.Printf("Auto-detecting database type...")

	// Check for namespaced database with common namespace
	testNamespace := []byte{
		0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
		0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
		0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
		0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
	}

	// Look for headers with namespace prefix
	headerPrefix := append(testNamespace, 'h')
	iter, err := r.sourceDB.NewIterator(headerPrefix, append(headerPrefix, 0xFF))
	if err != nil {
		return fmt.Errorf("failed to create iterator: %v", err)
	}
	defer iter.Close()

	if iter.First() && iter.Valid() {
		r.isNamespaced = true
		r.namespace = testNamespace
		log.Printf("Detected namespaced EVM database")
		return nil
	}

	// Check for standard geth database
	if _, closer, err := r.sourceDB.Get([]byte("LastBlock")); err == nil {
		if closer != nil {
			closer.Close()
		}
		r.isNamespaced = false
		log.Printf("Detected standard geth database")
		return nil
	}

	return fmt.Errorf("unable to detect database type")
}

// ReplayWithEVM replays blocks by executing transactions through the EVM
func (r *UnifiedReplayer) ReplayWithEVM() error {
	log.Printf("Starting EVM-based replay up to block %d", r.config.TargetHeight)
	log.Printf("Target wallet to track: %s", r.config.TargetWallet.Hex())

	// Initialize genesis state
	genesis, err := r.ExtractGenesis()
	if err != nil {
		return fmt.Errorf("failed to extract genesis: %v", err)
	}

	// Initialize state database from genesis
	statedb, err := state.New(genesis.Root(), r.stateCache)
	if err != nil {
		return fmt.Errorf("failed to create state database: %v", err)
	}
	r.stateDB = statedb

	// Process blocks sequentially
	for blockNum := uint64(1); blockNum <= r.config.TargetHeight; blockNum++ {
		if err := r.replayBlock(blockNum); err != nil {
			log.Printf("ERROR: Failed to replay block %d: %v", blockNum, err)
			if r.config.TestMode {
				continue // Skip errors in test mode
			}
			return err
		}

		// Log progress
		if blockNum%r.config.LogInterval == 0 || blockNum == r.config.TargetHeight {
			r.logProgress(blockNum)
		}
	}

	// Final wallet balance check
	if r.config.TargetWallet != (common.Address{}) {
		balance := r.stateDB.GetBalance(r.config.TargetWallet)
		balanceBig := balance.ToBig()
		log.Printf("FINAL: Wallet %s balance after replay: %s LUX",
			r.config.TargetWallet.Hex(),
			new(big.Float).Quo(new(big.Float).SetInt(balanceBig), big.NewFloat(1e18)).String())
	}

	return nil
}

// replayBlock replays a single block by executing its transactions
func (r *UnifiedReplayer) replayBlock(blockNum uint64) error {
	// Get block hash from canonical chain
	canonicalKey := r.buildCanonicalKey(blockNum)
	hashData, closer, err := r.sourceDB.Get(canonicalKey)
	if err != nil {
		return fmt.Errorf("block %d not found in canonical chain", blockNum)
	}
	defer closer.Close()

	blockHash := common.BytesToHash(hashData)

	// Get header
	header, err := r.getHeader(blockNum, blockHash)
	if err != nil {
		return fmt.Errorf("failed to get header for block %d: %v", blockNum, err)
	}

	// Get body (transactions)
	body, err := r.getBody(blockNum, blockHash)
	if err != nil {
		return fmt.Errorf("failed to get body for block %d: %v", blockNum, err)
	}

	// Create block
	block := types.NewBlockWithHeader(header).WithBody(*body)

	// Execute transactions
	receipts, err := r.executeBlock(block)
	if err != nil {
		return fmt.Errorf("failed to execute block %d: %v", blockNum, err)
	}

	// Verify state root if configured
	if r.config.VerifyStateRoots {
		stateRoot := r.stateDB.IntermediateRoot(true)
		if stateRoot != header.Root {
			log.Printf("WARNING: State root mismatch at block %d: computed %s, expected %s",
				blockNum, stateRoot.Hex(), header.Root.Hex())
		} else {
			atomic.AddUint64(&r.progress.StateVerifications, 1)
		}
	}

	// Track wallet balance
	if r.config.TargetWallet != (common.Address{}) {
		balance := r.stateDB.GetBalance(r.config.TargetWallet)
		r.progress.mu.Lock()
		r.progress.TargetWalletBalance = balance
		r.progress.mu.Unlock()
	}

	// Store block and receipts in target database
	rawdb.WriteBlock(r.targetDB, block)
	rawdb.WriteReceipts(r.targetDB, block.Hash(), block.NumberU64(), receipts)
	rawdb.WriteCanonicalHash(r.targetDB, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(r.targetDB, block.Hash())

	// Commit state changes
	root, err := r.stateDB.Commit(block.NumberU64(), true, true)
	if err != nil {
		return fmt.Errorf("failed to commit state for block %d: %v", blockNum, err)
	}

	// CRITICAL FIX: When using external trie database, force a flush to disk
	// This ensures state changes are persisted immediately
	if r.useExternalTrieDB && r.trieDB != nil {
		// The blockchain's trie database needs the commits to be written
		// Force the trie database to persist the state changes
		if err := r.trieDB.Commit(root, false); err != nil {
			log.Printf("WARNING: Failed to commit trie database at block %d: %v", blockNum, err)
		}
	}

	// Prepare for next block
	r.stateDB, err = state.New(root, r.stateCache)
	if err != nil {
		return fmt.Errorf("failed to create new state for block %d: %v", blockNum, err)
	}

	atomic.StoreUint64(&r.progress.CurrentBlock, blockNum)
	return nil
}

// executeBlock executes all transactions in a block
func (r *UnifiedReplayer) executeBlock(block *types.Block) (types.Receipts, error) {
	header := block.Header()

	// Create EVM block context
	blockContext := core.NewEVMBlockContext(header, &fakeChainContext{config: r.chainConfig}, &header.Coinbase)

	// Process transactions
	var (
		receipts types.Receipts
		usedGas  = new(uint64)
		gp       = new(core.GasPool).AddGas(header.GasLimit)
	)

	// Create EVM instance for this block
	evm := vm.NewEVM(blockContext, r.stateDB, r.chainConfig, r.vmConfig)

	for i, tx := range block.Transactions() {
		r.stateDB.SetTxContext(tx.Hash(), i)

		// Create transaction message
		msg, err := core.TransactionToMessage(tx, types.MakeSigner(r.chainConfig, header.Number, header.Time), header.BaseFee)
		if err != nil {
			atomic.AddUint64(&r.progress.FailedTxs, 1)
			continue
		}

		// Set transaction context in EVM
		evm.SetTxContext(core.NewEVMTxContext(msg))

		// Apply transaction
		result, err := core.ApplyMessage(evm, msg, gp)
		if err != nil {
			atomic.AddUint64(&r.progress.FailedTxs, 1)
			log.Printf("Failed to apply tx %s in block %d: %v", tx.Hash().Hex(), header.Number.Uint64(), err)
			continue
		}

		*usedGas += result.UsedGas

		// Create receipt based on execution result
		var root []byte
		var status uint64
		if r.chainConfig.IsByzantium(header.Number) {
			status = types.ReceiptStatusSuccessful
			if result.Failed() {
				status = types.ReceiptStatusFailed
			}
		} else {
			root = r.stateDB.IntermediateRoot(r.chainConfig.IsEIP158(header.Number)).Bytes()
		}

		// Build receipt
		receipt := &types.Receipt{
			Type:              tx.Type(),
			PostState:         root,
			Status:            status,
			CumulativeGasUsed: *usedGas,
			Bloom:             types.Bloom{}, // Will be calculated later
			Logs:              r.stateDB.GetLogs(tx.Hash(), block.NumberU64(), block.Hash(), uint64(i)),
			TxHash:            tx.Hash(),
			GasUsed:           result.UsedGas,
			BlockHash:         block.Hash(),
			BlockNumber:       block.Number(),
			TransactionIndex:  uint(i),
		}

		// Set contract address for contract creation
		if msg.To == nil {
			receipt.ContractAddress = crypto.CreateAddress(msg.From, tx.Nonce())
		}

		receipts = append(receipts, receipt)
		atomic.AddUint64(&r.progress.ProcessedTxs, 1)
	}

	// Calculate and set bloom for each receipt
	if len(receipts) > 0 {
		for _, receipt := range receipts {
			receipt.Bloom = types.CreateBloom(receipt)
		}
	}

	// Finalize block state
	r.stateDB.Finalise(true)

	return receipts, nil
}

// Helper functions for building keys and fetching data

func (r *UnifiedReplayer) buildCanonicalKey(blockNum uint64) []byte {
	key := append([]byte(nil), r.namespace...)
	key = append(key, 'h')
	key = append(key, encodeReplayBlockNumber(blockNum)...)
	key = append(key, 'n')
	return key
}

func (r *UnifiedReplayer) getHeader(blockNum uint64, hash common.Hash) (*types.Header, error) {
	headerKey := append([]byte(nil), r.namespace...)
	headerKey = append(headerKey, 'h')
	headerKey = append(headerKey, encodeReplayBlockNumber(blockNum)...)
	headerKey = append(headerKey, hash.Bytes()...)

	headerData, closer, err := r.sourceDB.Get(headerKey)
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var header types.Header
	if err := rlp.DecodeBytes(headerData, &header); err != nil {
		// Try EVM format
		var subnetHeader EVMHeader
		if err2 := rlp.DecodeBytes(headerData, &subnetHeader); err2 != nil {
			return nil, fmt.Errorf("failed to decode header: %v", err)
		}
		// Convert to standard header
		header = types.Header{
			ParentHash:  subnetHeader.ParentHash,
			UncleHash:   subnetHeader.UncleHash,
			Coinbase:    subnetHeader.Coinbase,
			Root:        subnetHeader.Root,
			TxHash:      subnetHeader.TxHash,
			ReceiptHash: subnetHeader.ReceiptHash,
			Bloom:       subnetHeader.Bloom,
			Difficulty:  subnetHeader.Difficulty,
			Number:      subnetHeader.Number,
			GasLimit:    subnetHeader.GasLimit,
			GasUsed:     subnetHeader.GasUsed,
			Time:        subnetHeader.Time,
			Extra:       subnetHeader.Extra,
			MixDigest:   subnetHeader.MixDigest,
			Nonce:       subnetHeader.Nonce,
			BaseFee:     subnetHeader.BaseFee,
		}
	}

	return &header, nil
}

func (r *UnifiedReplayer) getBody(blockNum uint64, hash common.Hash) (*types.Body, error) {
	bodyKey := append([]byte(nil), r.namespace...)
	bodyKey = append(bodyKey, 'b')
	bodyKey = append(bodyKey, encodeReplayBlockNumber(blockNum)...)
	bodyKey = append(bodyKey, hash.Bytes()...)

	bodyData, closer, err := r.sourceDB.Get(bodyKey)
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var body types.Body
	if err := rlp.DecodeBytes(bodyData, &body); err != nil {
		return nil, fmt.Errorf("failed to decode body: %v", err)
	}

	return &body, nil
}

// ExtractGenesis extracts the genesis block from the source database
func (r *UnifiedReplayer) ExtractGenesis() (*types.Block, error) {
	// Get block 0 (genesis)
	canonicalKey := r.buildCanonicalKey(0)
	hashData, closer, err := r.sourceDB.Get(canonicalKey)
	if err != nil {
		return nil, fmt.Errorf("genesis block hash not found: %v", err)
	}
	defer closer.Close()

	genesisHash := common.BytesToHash(hashData)
	log.Printf("Found genesis hash: %s", genesisHash.Hex())

	// Get genesis header
	header, err := r.getHeader(0, genesisHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get genesis header: %v", err)
	}

	// Get genesis body (usually empty)
	body, err := r.getBody(0, genesisHash)
	if err != nil {
		// Genesis might not have a body
		body = &types.Body{}
	}

	// Create genesis block
	genesis := types.NewBlockWithHeader(header).WithBody(*body)

	// Write genesis to target database
	rawdb.WriteBlock(r.targetDB, genesis)
	rawdb.WriteCanonicalHash(r.targetDB, genesis.Hash(), 0)
	rawdb.WriteHeadBlockHash(r.targetDB, genesis.Hash())

	log.Printf("Genesis block imported: number=%d hash=%s", genesis.NumberU64(), genesis.Hash().Hex())
	return genesis, nil
}

func (r *UnifiedReplayer) logProgress(blockNum uint64) {
	r.progress.mu.RLock()
	defer r.progress.mu.RUnlock()

	elapsed := time.Since(r.progress.StartTime)
	blocksPerSecond := float64(blockNum) / elapsed.Seconds()
	remainingBlocks := r.config.TargetHeight - blockNum
	eta := time.Duration(float64(remainingBlocks) / blocksPerSecond * float64(time.Second))

	walletInfo := ""
	if r.config.TargetWallet != (common.Address{}) {
		balanceBig := r.progress.TargetWalletBalance.ToBig()
		balanceLux := new(big.Float).Quo(
			new(big.Float).SetInt(balanceBig),
			big.NewFloat(1e18),
		)
		walletInfo = fmt.Sprintf(" | Wallet Balance: %s LUX", balanceLux.String())
	}

	log.Printf("Progress: Block %d/%d (%.2f%%) | %.2f blocks/s | ETA: %s | Txs: %d (failed: %d)%s",
		blockNum,
		r.config.TargetHeight,
		float64(blockNum)/float64(r.config.TargetHeight)*100,
		blocksPerSecond,
		eta,
		r.progress.ProcessedTxs,
		r.progress.FailedTxs,
		walletInfo,
	)
}

// Helper function to encode block number (renamed to avoid conflict)
func encodeReplayBlockNumber(num uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, num)
	return enc
}

// fakeChainContext implements core.ChainContext for EVM execution
type fakeChainContext struct {
	config *params.ChainConfig
}

func (f *fakeChainContext) Engine() consensus.Engine {
	return nil
}

func (f *fakeChainContext) GetHeader(hash common.Hash, number uint64) *types.Header {
	return nil
}

func (f *fakeChainContext) Config() *params.ChainConfig {
	return f.config
}

func (f *fakeChainContext) CurrentHeader() *types.Header {
	return nil
}

func (f *fakeChainContext) GetHeaderByHash(hash common.Hash) *types.Header {
	return nil
}

func (f *fakeChainContext) GetHeaderByNumber(number uint64) *types.Header {
	return nil
}

// Close closes the replayer and cleans up resources
func (r *UnifiedReplayer) Close() error {
	if r.sourceDB != nil {
		return r.sourceDB.Close()
	}
	return nil
}