// Package chainmigrate provides generic blockchain import/export functionality
// for migrating data between different blockchain implementations.
// Supports SubnetEVM→C-Chain, Zoo L2→mainnet, and any future chain migrations.
package chainmigrate

import (
	"context"
	"math/big"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
)

// ChainType identifies the type of blockchain
type ChainType string

const (
	ChainTypeSubnetEVM ChainType = "subnet-evm"
	ChainTypeCChain    ChainType = "c-chain"
	ChainTypeCoreth    ChainType = "coreth"    // Special case for old Coreth VM
	ChainTypeZooL2     ChainType = "zoo-l2"
	ChainTypeCustom    ChainType = "custom"
	ChainTypePChain    ChainType = "p-chain"
	ChainTypeXChain    ChainType = "x-chain"
	ChainTypeQChain    ChainType = "q-chain"
)

// BlockData represents a generic block with all necessary data
type BlockData struct {
	// Core block data
	Number          uint64
	Hash            common.Hash
	ParentHash      common.Hash
	Timestamp       uint64
	StateRoot       common.Hash
	ReceiptsRoot    common.Hash
	TransactionsRoot common.Hash
	
	// Block metadata
	GasLimit        uint64
	GasUsed         uint64
	Difficulty      *big.Int
	TotalDifficulty *big.Int
	Coinbase        common.Address
	Nonce           types.BlockNonce
	MixHash         common.Hash
	ExtraData       []byte
	BaseFee         *big.Int // EIP-1559
	
	// Full data
	Header          []byte          // RLP encoded header
	Body            []byte          // RLP encoded body
	Receipts        []byte          // RLP encoded receipts
	Transactions    []*Transaction  // Decoded transactions
	UncleHeaders    [][]byte        // Uncle/ommer headers
	
	// Chain-specific extensions
	Extensions      map[string]interface{} // For chain-specific data
}

// Transaction represents a generic transaction
type Transaction struct {
	Hash     common.Hash
	Nonce    uint64
	From     common.Address
	To       *common.Address // nil for contract creation
	Value    *big.Int
	Gas      uint64
	GasPrice *big.Int
	Data     []byte
	V, R, S  *big.Int // Signature values
	
	// EIP-1559 fields
	GasTipCap   *big.Int
	GasFeeCap   *big.Int
	AccessList  types.AccessList
	
	// Receipt data
	Receipt     *TransactionReceipt
}

// TransactionReceipt represents a transaction receipt
type TransactionReceipt struct {
	Status            uint64
	CumulativeGasUsed uint64
	Bloom             types.Bloom
	Logs              []*types.Log
	TransactionHash   common.Hash
	ContractAddress   *common.Address
	GasUsed           uint64
}

// StateAccount represents an account in the state trie
type StateAccount struct {
	Address     common.Address
	Nonce       uint64
	Balance     *big.Int
	CodeHash    common.Hash
	StorageRoot common.Hash
	Code        []byte                     // Contract code
	Storage     map[common.Hash]common.Hash // Storage slots
}

// ChainExporter defines the interface for exporting blockchain data
type ChainExporter interface {
	// Initialize the exporter with source database
	Init(config ExporterConfig) error
	
	// Get chain metadata
	GetChainInfo() (*ChainInfo, error)
	
	// Export blocks in a range (inclusive)
	ExportBlocks(ctx context.Context, start, end uint64) (<-chan *BlockData, <-chan error)
	
	// Export state at a specific block
	ExportState(ctx context.Context, blockNumber uint64) (<-chan *StateAccount, <-chan error)
	
	// Export specific account state
	ExportAccount(ctx context.Context, address common.Address, blockNumber uint64) (*StateAccount, error)
	
	// Export chain configuration (genesis, network ID, etc.)
	ExportConfig() (*ChainConfig, error)
	
	// Verify export integrity
	VerifyExport(blockNumber uint64) error
	
	// Close the exporter
	Close() error
}

// ChainImporter defines the interface for importing blockchain data
type ChainImporter interface {
	// Initialize the importer with destination database
	Init(config ImporterConfig) error
	
	// Import chain configuration
	ImportConfig(config *ChainConfig) error
	
	// Import blocks (must be sequential)
	ImportBlock(block *BlockData) error
	
	// Import blocks in batch
	ImportBlocks(blocks []*BlockData) error
	
	// Import state accounts
	ImportState(accounts []*StateAccount, blockNumber uint64) error
	
	// Finalize import at block height
	FinalizeImport(blockNumber uint64) error
	
	// Verify import integrity
	VerifyImport(blockNumber uint64) error
	
	// Execute block to rebuild state (for runtime replay)
	ExecuteBlock(block *BlockData) error
	
	// Close the importer
	Close() error
}

// ChainMigrator orchestrates the migration between chains
type ChainMigrator interface {
	// Migrate performs the full migration
	Migrate(ctx context.Context, source ChainExporter, dest ChainImporter, options MigrationOptions) error
	
	// MigrateRange migrates a specific block range
	MigrateRange(ctx context.Context, source ChainExporter, dest ChainImporter, start, end uint64) error
	
	// MigrateState migrates state at a specific height
	MigrateState(ctx context.Context, source ChainExporter, dest ChainImporter, blockNumber uint64) error
	
	// VerifyMigration verifies the migration was successful
	VerifyMigration(source ChainExporter, dest ChainImporter, blockNumber uint64) error
}

// ChainInfo contains metadata about a blockchain
type ChainInfo struct {
	ChainType       ChainType
	NetworkID       uint64
	ChainID         *big.Int
	GenesisHash     common.Hash
	CurrentHeight   uint64
	TotalDifficulty *big.Int
	StateRoot       common.Hash
	
	// Chain-specific info
	VMVersion       string
	DatabaseType    string // "pebble", "badger", "leveldb"
	IsPruned        bool
	ArchiveMode     bool
	
	// Special flags
	HasWarpMessages bool // For subnet communications
	HasProposerVM   bool // For proposer VM chains
}

// ChainConfig contains chain configuration
type ChainConfig struct {
	// Network configuration
	NetworkID       uint64
	ChainID         *big.Int
	
	// Genesis configuration
	GenesisBlock    *BlockData
	GenesisAlloc    map[common.Address]*StateAccount
	
	// Chain parameters
	HomesteadBlock  *big.Int
	EIP150Block     *big.Int
	EIP155Block     *big.Int
	EIP158Block     *big.Int
	ByzantiumBlock  *big.Int
	ConstantinopleBlock *big.Int
	PetersburgBlock *big.Int
	IstanbulBlock   *big.Int
	BerlinBlock     *big.Int
	LondonBlock     *big.Int
	
	// Special configurations
	IsCoreth        bool // Special handling for Coreth VM differences
	HasNetID        bool
	NetID           string

	// Custom precompiles
	Precompiles     map[common.Address]string
}

// ExporterConfig configures a chain exporter
type ExporterConfig struct {
	ChainType       ChainType
	DatabasePath    string
	DatabaseType    string // "pebble", "badger", "leveldb"

	// For net chains
	NetNamespace    []byte
	NetID           string

	// Export options
	ExportState     bool
	ExportReceipts  bool
	VerifyIntegrity bool
	MaxConcurrency  int

	// Special handling
	CorethCompat    bool // Enable Coreth VM compatibility mode
}

// ImporterConfig configures a chain importer
type ImporterConfig struct {
	ChainType       ChainType
	DatabasePath    string
	DatabaseType    string
	
	// Import options
	ExecuteBlocks   bool // Execute transactions vs just import data
	VerifyState     bool
	BatchSize       int
	MaxConcurrency  int
	
	// State management
	PreserveState   bool // Preserve existing state
	MergeState      bool // Merge with existing state
	
	// Special handling
	CorerthCompat   bool
	GenesisTime     time.Time
}

// MigrationOptions configures the migration process
type MigrationOptions struct {
	// Block range
	StartBlock      uint64
	EndBlock        uint64
	
	// Processing options
	BatchSize       int
	MaxConcurrency  int
	VerifyEachBlock bool
	
	// State options
	MigrateState    bool
	StateHeight     uint64 // Block height for state migration
	
	// Error handling
	ContinueOnError bool
	RetryFailures   int
	
	// Progress reporting
	ProgressCallback func(current, total uint64)
	
	// Special migrations
	RegenesisMode   bool // For mainnet regenesis
	PreserveNonces  bool // Preserve account nonces
	RemapAddresses  map[common.Address]common.Address // Address remapping
}

// MigrationResult contains the result of a migration
type MigrationResult struct {
	Success         bool
	BlocksMigrated  uint64
	StatesMigrated  uint64
	StartTime       time.Time
	EndTime         time.Time
	Errors          []error
	
	// Verification results
	SourceStateRoot common.Hash
	DestStateRoot   common.Hash
	StateMatches    bool
}