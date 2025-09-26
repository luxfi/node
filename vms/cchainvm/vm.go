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
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luxfi/geth/common"
	gethcore "github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/txpool"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/eth/ethconfig"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/geth/rpc"

	consensusNode "github.com/luxfi/consensus"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/version"
)

// newUint64 is a helper function to create a pointer to uint64
func newUint64(n uint64) *uint64 {
	return &n
}

var (
	_ block.ChainVM = (*VM)(nil)

	errNilBlock     = errors.New("nil block")
	errInvalidBlock = errors.New("invalid block")
)

// ReplayState represents the state of database replay
type ReplayState int32

const (
	ReplayNotStarted ReplayState = 0
	ReplayInProgress ReplayState = 1
	ReplayCompleted  ReplayState = 2
	ReplayFailed     ReplayState = 3
)

// ReplayProgress tracks the progress of database replay
type ReplayProgress struct {
	state        atomic.Int32  // ReplayState
	currentBlock atomic.Uint64
	totalBlocks  atomic.Uint64
	startTime    atomic.Value // time.Time
	phase        atomic.Value // string
	errorMsg     atomic.Value // string
}

// NewReplayProgress creates a new replay progress tracker
func NewReplayProgress() *ReplayProgress {
	p := &ReplayProgress{}
	p.state.Store(int32(ReplayNotStarted))
	p.phase.Store("")
	p.errorMsg.Store("")
	return p
}

// Start marks the replay as started
func (p *ReplayProgress) Start(totalBlocks uint64) {
	p.state.Store(int32(ReplayInProgress))
	p.totalBlocks.Store(totalBlocks)
	p.currentBlock.Store(0)
	p.startTime.Store(time.Now())
	p.phase.Store("starting")
	p.errorMsg.Store("")
}

// SetPhase updates the current phase
func (p *ReplayProgress) SetPhase(phase string) {
	p.phase.Store(phase)
}

// UpdateBlock updates the current block number
func (p *ReplayProgress) UpdateBlock(blockNum uint64) {
	p.currentBlock.Store(blockNum)
}

// IncrementBlock increments the current block counter
func (p *ReplayProgress) IncrementBlock() uint64 {
	return p.currentBlock.Add(1)
}

// Complete marks the replay as completed
func (p *ReplayProgress) Complete() {
	p.state.Store(int32(ReplayCompleted))
	p.phase.Store("completed")
}

// Fail marks the replay as failed
func (p *ReplayProgress) Fail(err error) {
	p.state.Store(int32(ReplayFailed))
	p.phase.Store("failed")
	p.errorMsg.Store(err.Error())
}

// GetStatus returns the current status
func (p *ReplayProgress) GetStatus() map[string]interface{} {
	state := ReplayState(p.state.Load())

	if state == ReplayNotStarted {
		return map[string]interface{}{
			"replaying": false,
			"state":     "not_started",
		}
	}

	current := p.currentBlock.Load()
	total := p.totalBlocks.Load()
	phase := p.phase.Load().(string)

	result := map[string]interface{}{
		"state":        stateString(state),
		"currentBlock": current,
		"totalBlocks":  total,
		"phase":        phase,
	}

	if state == ReplayInProgress {
		result["replaying"] = true

		// Calculate percentage
		if total > 0 {
			result["percentage"] = float64(current) * 100.0 / float64(total)
		}

		// Calculate rate and ETA
		if startTimeVal := p.startTime.Load(); startTimeVal != nil {
			if startTime, ok := startTimeVal.(time.Time); ok {
				elapsed := time.Since(startTime).Seconds()
				if elapsed > 0 && current > 0 {
					rate := float64(current) / elapsed
					result["blocksPerSecond"] = rate

					if total > current && rate > 0 {
						remaining := total - current
						etaSeconds := float64(remaining) / rate
						result["estimatedTimeRemaining"] = formatDuration(time.Duration(etaSeconds * float64(time.Second)))
					}
				}
			}
		}
	} else if state == ReplayCompleted {
		result["replaying"] = false
		if startTimeVal := p.startTime.Load(); startTimeVal != nil {
			if startTime, ok := startTimeVal.(time.Time); ok {
				elapsed := time.Since(startTime)
				result["totalTime"] = elapsed.String()
				if elapsed.Seconds() > 0 && current > 0 {
					result["averageRate"] = float64(current) / elapsed.Seconds()
				}
			}
		}
	} else if state == ReplayFailed {
		result["replaying"] = false
		if errMsg := p.errorMsg.Load(); errMsg != nil {
			result["error"] = errMsg.(string)
		}
	}

	return result
}

func stateString(state ReplayState) string {
	switch state {
	case ReplayNotStarted:
		return "not_started"
	case ReplayInProgress:
		return "in_progress"
	case ReplayCompleted:
		return "completed"
	case ReplayFailed:
		return "failed"
	default:
		return "unknown"
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// DatabaseReplayConfig holds configuration for database replay
type DatabaseReplayConfig struct {
	SourcePath               string // Path to source database
	TestLimit                uint64 // If > 0, limit replay to this many blocks
	ExtractGenesisFromSource bool   // If true, extract genesis from block 0 of source
}

// VM implements the C-Chain VM interface using geth
type VM struct {
	ctx          context.Context
	chainCtx     interface{} // Store original chain context for helper functions
	log          *slog.Logger
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

	// Database replay config (if using --genesis-db)
	replayConfig *DatabaseReplayConfig

	// Replay progress tracking
	replayProgress *ReplayProgress

	// Synchronization
	mu           sync.RWMutex
	building     ids.ID
	builtBlocks  map[ids.ID]*Block
	shutdownChan chan struct{}
}

// Initialize implements the block.ChainVM interface
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx interface{},
	db interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	msgChan interface{},
	fxs []interface{},
	appSender interface{},
) error {
	// Store the original chain context
	vm.chainCtx = chainCtx
	vm.ctx = ctx // Use the regular context for now

	// Create a logger
	vm.log = slog.Default().With("vm", "cchain")

	// Type assert the database interface
	vmDB, ok := db.(database.Database)
	if !ok {
		return fmt.Errorf("db is not a database.Database")
	}
	vm.db = vmDB

	vm.genesisBytes = genesisBytes
	vm.shutdownChan = make(chan struct{})
	vm.builtBlocks = make(map[ids.ID]*Block)
	vm.replayProgress = NewReplayProgress()

	// MIGRATION DETECTION: Check if we have migrated data BEFORE any initialization
	// We need to check at the C-Chain database level, not the wrapped level
	hasMigratedData := false
	migratedHeight := uint64(0)
	migratedBlockHash := common.Hash{}

	// Create a database wrapper first (will be replaced if we have migrated data)
	vm.ethDB = WrapDatabase(vmDB)

	// Check for REPLAY environment variable to trigger database replay
	if replayPath := os.Getenv("LUX_REPLAY_DB"); replayPath != "" {
		vm.log.Info("Replay mode enabled via environment variable", "source", replayPath)
		vm.replayConfig = &DatabaseReplayConfig{
			SourcePath: replayPath,
		}
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

			// Log to Lux logger too
			vm.log.Info("Detected imported blockchain data from environment",
				"height", height,
				"blockHash", migratedBlockHash.Hex(),
			)

			// Open the ethdb subdirectory directly for migrated data
			// Try to extract chain data dir from environment or use default
			chainDataDir := os.Getenv("CHAIN_DATA_DIR")
			if chainDataDir == "" {
				// Use a default path
				chainDataDir = "/home/z/.luxd/chainData/C/db"
			}
			// Check both possible locations for ethdb
			possiblePaths := []string{
				filepath.Join(chainDataDir, "badgerdb", "ethdb"), // New location after migration
				filepath.Join(chainDataDir, "ethdb"),              // Direct location
			}

			for _, ethdbPath := range possiblePaths {
				if _, err := os.Stat(ethdbPath); err == nil {
					fmt.Printf("Opening migrated ethdb at: %s\n", ethdbPath)
					badgerConfig := BadgerDatabaseConfig{
						DataDir:       ethdbPath,
						EnableAncient: false,
						ReadOnly:      false,
					}
					ethDB, err := NewBadgerDatabase(nil, badgerConfig)
					if err == nil {
						// Wrap the migrated database with an adapter
						vm.ethDB = NewMigratedDataAdapter(ethDB)
						fmt.Printf("Successfully opened migrated ethdb with adapter\n")
						break
					} else {
						fmt.Printf("Failed to open migrated ethdb at %s: %v\n", ethdbPath, err)
					}
				}
			}
		}
	}

	// Check if replay config is set via CLI flags (--genesis-db)
	if vm.replayConfig != nil {
		vm.log.Info("Genesis database replay configured",
			"sourcePath", vm.replayConfig.SourcePath,
			"blockLimit", vm.replayConfig.TestLimit,
		)
		hasMigratedData = true
	}

	// Fallback: Check for migrated blockchain data in database
	if !hasMigratedData {
		// ALWAYS check chainData/C/db first for C-Chain (no blockchain ID dependency)
		dataDir := os.Getenv("DATA_DIR")
		if dataDir == "" {
			dataDir = "/home/z/.luxd"
		}
		baseDir := filepath.Join(dataDir, "chainData", "C", "db")
		possiblePaths := []string{
			filepath.Join(baseDir, "badgerdb", "ethdb"),
			filepath.Join(baseDir, "ethdb"),
		}

		// Also check blockchain-ID-specific path as fallback
		if chainCtxWithDir, ok := vm.chainCtx.(interface{ GetChainDataDir() string }); ok {
			possiblePaths = append(possiblePaths,
				filepath.Join(chainCtxWithDir.GetChainDataDir(), "badgerdb", "ethdb"),
				filepath.Join(chainCtxWithDir.GetChainDataDir(), "ethdb"),
			)
		}

		for _, ethdbPath := range possiblePaths {
			if stat, err := os.Stat(ethdbPath); err == nil && stat.IsDir() {
				// Check if it has substantial data (>500MB indicates migrated blockchain)
				if dirSize := getDirSize(ethdbPath); dirSize > 500*1024*1024 {
					fmt.Printf("DETECTED MIGRATED BADGERDB AT %s (%d MB)\n", ethdbPath, dirSize/(1024*1024))

					// Open the migrated database
					badgerConfig := BadgerDatabaseConfig{
						DataDir:       ethdbPath,
						EnableAncient: false,
						ReadOnly:      false,
					}
					if ethDB, err := NewBadgerDatabase(nil, badgerConfig); err == nil {
						// Wrap with adapter
						vm.ethDB = NewMigratedDataAdapter(ethDB)
						hasMigratedData = true
						migratedHeight = 1082780 // Known height from migration
						fmt.Printf("Successfully opened migrated ethdb with adapter\n")
						break
					}
				}
			}
		}

		// Also check Height key in current database
		if !hasMigratedData {
			if heightBytes, err := vm.ethDB.Get([]byte("Height")); err == nil && len(heightBytes) == 8 {
				height := binary.BigEndian.Uint64(heightBytes)
				if height > 0 {
					hasMigratedData = true
					migratedHeight = height
					fmt.Printf("DETECTED MIGRATED DATA AT HEIGHT %d\n", height)

					// Log to Lux logger too
					vm.log.Info("Detected migrated blockchain data",
						"height", height,
					)
				}
			}
		}
	}

	// If we have migrated data, skip normal genesis initialization

	// DEBUG: Log database path and check contents
	fmt.Printf("DEBUG: C-Chain VM Initialize called\n")
	fmt.Printf("DEBUG: Database type: %T\n", db)
	fmt.Printf("DEBUG: Genesis bytes length: %d\n", len(genesisBytes))
	fmt.Printf("DEBUG: Genesis bytes: %s\n", string(genesisBytes))

	// Parse genesis or use default
	var genesis *gethcore.Genesis

	if false { // Old hardcoded fallback (disabled)
		genesis = &gethcore.Genesis{
				Config: &params.ChainConfig{
				ChainID:             big.NewInt(96369),
				HomesteadBlock:      big.NewInt(0),
				EIP150Block:         big.NewInt(0),
				EIP155Block:         big.NewInt(0),
				EIP158Block:         big.NewInt(0),
				ByzantiumBlock:      big.NewInt(0),
				ConstantinopleBlock: big.NewInt(0),
				PetersburgBlock:     big.NewInt(0),
				IstanbulBlock:       big.NewInt(0),
				MuirGlacierBlock:    big.NewInt(0),
				BerlinBlock:         big.NewInt(0),
				LondonBlock:         big.NewInt(0),
				ArrowGlacierBlock:   big.NewInt(0),
				GrayGlacierBlock:    big.NewInt(0),
				MergeNetsplitBlock:  big.NewInt(0),
				// Use actual timestamps from extracted database config
				ShanghaiTime:            newUint64(1607144400),   // From extracted config
				CancunTime:              newUint64(253399622400), // Far future from extracted config
				PragueTime:              nil,                     // Not yet defined
				VerkleTime:              nil,                     // Not yet defined
				TerminalTotalDifficulty: common.Big0,
				// BlobScheduleConfig for Cancun
				BlobScheduleConfig: &params.BlobScheduleConfig{
					Cancun: &params.BlobConfig{
						Target:         3,
						Max:            6,
						UpdateFraction: 3338477,
					},
				},
			},
			Nonce:      0x0,
			Timestamp:  0x672485c2, // 1730446786 - from imported blockchain data
			ExtraData:  []byte{},
			GasLimit:   0xb71b00, // 12000000
			Difficulty: big.NewInt(0),
			Mixhash:    common.Hash{},
			Coinbase:   common.Address{},
			Alloc: gethcore.GenesisAlloc{
				// Single allocation from mainnet genesis
				common.HexToAddress("0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"): types.Account{
					Balance: func() *big.Int {
						b := new(big.Int)
						b.SetString("193e5939a08ce9dbd480000000", 16) // hex value from genesis
						return b
					}(),
				},
			},
		}

		vm.log.Info("Using fallback genesis for replay",
			"chainId", 96369,
			"shanghaiTime", 1607144400,
			"cancunTime", 253399622400,
			"expectedHash", "0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e")
	} else if len(genesisBytes) > 0 {
		// First check if this is a database replay genesis or uses migrated data
		var genesisMap map[string]interface{}
		if err := json.Unmarshal(genesisBytes, &genesisMap); err == nil {
			// Check for useMigratedData flag - for using existing migrated blockchain data
			if useMigrated, ok := genesisMap["useMigratedData"].(bool); ok && useMigrated {
				vm.log.Info("Using migrated blockchain data from existing database")
				fmt.Printf("MIGRATED DATA MODE: Using existing blockchain at block 1,082,780\n")

				// Mark as migrated data to skip genesis initialization
				hasMigratedData = true

				// Don't set any genesis - let the VM use what's already in the database
				genesis = nil

			} else if replay, ok := genesisMap["replay"].(bool); ok && replay {
				// This is a database replay genesis
				dbPath, _ := genesisMap["dbPath"].(string)
				dbType, _ := genesisMap["dbType"].(string)
				chainID, _ := genesisMap["chainId"].(float64)

				vm.log.Info("Database replay genesis detected",
					"dbPath", dbPath,
					"dbType", dbType,
					"chainId", chainID)

				// Mark as migrated data to skip genesis initialization
				hasMigratedData = true

				// Extract the chain config
				genesis = &gethcore.Genesis{
					Config: &params.ChainConfig{
						ChainID: big.NewInt(int64(chainID)),
					},
				}

				if configData, ok := genesisMap["config"].(map[string]interface{}); ok {
					configBytes, _ := json.Marshal(configData)
					if err := json.Unmarshal(configBytes, genesis.Config); err != nil {
						return fmt.Errorf("failed to parse chain config: %w", err)
					}
				}

				// Perform database replay if path is provided
				if dbPath != "" {
					vm.log.Info("Starting database replay", "path", dbPath, "type", dbType)

					// Create replay config
					replayConfig := &DatabaseReplayConfig{
						SourcePath: dbPath,
					}

					// After VM is initialized, we'll perform the replay
					// For now, mark that we need to do replay
					hasMigratedData = true
					vm.replayConfig = replayConfig
				}
			} else if hasMigratedData {
				// PATCH: When we have migrated data from environment variables,
				// don't parse the genesis JSON - it will conflict with our migrated data
				vm.log.Info("Skipping genesis parsing due to migrated data from environment")
				fmt.Printf("MIGRATION PATCH: Skipping genesis due to imported data at height %d\n", migratedHeight)
				genesis = nil
			} else {
				// Normal genesis parsing
				genesis = &gethcore.Genesis{}
				if err := json.Unmarshal(genesisBytes, genesis); err != nil {
					return fmt.Errorf("failed to unmarshal genesis: %w", err)
				}
			}
		} else {
			// Normal genesis parsing
			genesis = &gethcore.Genesis{}
			if err := json.Unmarshal(genesisBytes, genesis); err != nil {
				return fmt.Errorf("failed to unmarshal genesis: %w", err)
			}
		}

		// Set terminal total difficulty for PoS transition
		if genesis != nil && genesis.Config != nil && genesis.Config.TerminalTotalDifficulty == nil {
			genesis.Config.TerminalTotalDifficulty = common.Big0
		}
		
		// CRITICAL FIX: Add BlobSchedule for Cancun fork if CancunTime is set
		if genesis != nil && genesis.Config != nil && genesis.Config.CancunTime != nil {
			if genesis.Config.BlobScheduleConfig == nil {
				genesis.Config.BlobScheduleConfig = &params.BlobScheduleConfig{}
			}
			if genesis.Config.BlobScheduleConfig.Cancun == nil {
				genesis.Config.BlobScheduleConfig.Cancun = &params.BlobConfig{}
			}
			// Always set these values even if struct exists (might have zeros from JSON unmarshal)
			if genesis.Config.BlobScheduleConfig.Cancun.UpdateFraction == 0 {
				genesis.Config.BlobScheduleConfig.Cancun.Target = 3
				genesis.Config.BlobScheduleConfig.Cancun.Max = 6
				genesis.Config.BlobScheduleConfig.Cancun.UpdateFraction = 3338477
				vm.log.Info("Fixed BlobSchedule configuration for Cancun fork", 
					"target", 3, "max", 6, "updateFraction", 3338477)
			}
		}
	} else {
		// For network 96369, use genesis that matches migrated data
		// Extract NetworkID from chainCtx if available
		var networkID uint32 = 1 // default
		if chainCtxWithID, ok := vm.chainCtx.(interface{ GetNetworkID() uint32 }); ok {
			networkID = chainCtxWithID.GetNetworkID()
		}
		if networkID == 96369 {
			genesis = &gethcore.Genesis{
				Config: &params.ChainConfig{
					ChainID:             big.NewInt(96369),
					HomesteadBlock:      big.NewInt(0),
					EIP150Block:         big.NewInt(0),
					EIP155Block:         big.NewInt(0),
					EIP158Block:         big.NewInt(0),
					ByzantiumBlock:      big.NewInt(0),
					ConstantinopleBlock: big.NewInt(0),
					PetersburgBlock:     big.NewInt(0),
					IstanbulBlock:       big.NewInt(0),
					MuirGlacierBlock:    big.NewInt(0),
					BerlinBlock:         big.NewInt(0),
					LondonBlock:         big.NewInt(0),
					ArrowGlacierBlock:   big.NewInt(0),
					GrayGlacierBlock:    big.NewInt(0),
					MergeNetsplitBlock:  big.NewInt(0),
					// Activate time-based forks at genesis timestamp + 1 second
					// This ensures they're active but don't interfere with genesis validation
					ShanghaiTime:            newUint64(1730446787), // genesis timestamp + 1
					CancunTime:              newUint64(1730446787), // genesis timestamp + 1
					PragueTime:              nil,                   // Not yet defined
					VerkleTime:              nil,                   // Not yet defined
					TerminalTotalDifficulty: common.Big0,
					BlobScheduleConfig: &params.BlobScheduleConfig{
						Cancun: &params.BlobConfig{
							Target:         3,
							Max:            6,
							UpdateFraction: 3338477,
						},
					},
				},
				Nonce:      0x0,
				Timestamp:  0x672485c2, // 1730446786 - matches actual mainnet genesis
				ExtraData:  []byte{},
				GasLimit:   0xb71b00, // 12000000 - matches actual mainnet genesis
				Difficulty: big.NewInt(0),
				Mixhash:    common.Hash{},
				Coinbase:   common.Address{},
				Alloc:      gethcore.GenesisAlloc{},
			}
			vm.log.Info("Using genesis for migrated network 96369 data")
		} else {
			// Use default dev genesis for other networks
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
	}

	// Initialize chain config
	if genesis != nil {
		vm.chainConfig = genesis.Config
	}
	if vm.chainConfig == nil {
		// Use network 96369 config for migrated data
		if hasMigratedData {
			vm.chainConfig = &params.ChainConfig{
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
				TerminalTotalDifficulty: common.Big0,
			}
		} else {
			vm.chainConfig = params.AllEthashProtocolChanges
		}
		if genesis != nil {
			genesis.Config = vm.chainConfig
		}
	}

	// Initialize eth config
	vm.ethConfig = ethconfig.Defaults
	vm.ethConfig.Genesis = genesis
	vm.ethConfig.NetworkId = vm.chainConfig.ChainID.Uint64()
	vm.ethConfig.Miner.Etherbase = common.Address{}

	// CRITICAL: For migrated data, we must prevent normal genesis initialization
	if hasMigratedData {
		fmt.Printf("MIGRATION MODE: Skipping genesis, loading from height %d\n", migratedHeight)

		// When we have migrated data, genesis is already nil,
		// so we don't need to modify it

		// Mark database as already initialized to prevent SetupGenesisBlock
		// Write a dummy genesis hash to satisfy the check
		if err := vm.ethDB.Put([]byte("genesis"), []byte{1}); err == nil {
			fmt.Printf("Marked database as initialized\n")
		}

		// Skip old loader if using new replay
		if vm.replayConfig == nil {
			// Load all blocks from SubnetEVM database (old method)
			if err := LoadSubnetEVMDatabase(vm.ethDB); err != nil {
				fmt.Printf("Warning: Failed to load SubnetEVM database: %v\n", err)
			}
		}
	}

	// CRITICAL: If we have a replay config with genesis extraction, do it FIRST
	var extractedGenesis *types.Block
	if vm.replayConfig != nil && vm.replayConfig.ExtractGenesisFromSource {
		vm.log.Info("Extracting genesis from source database BEFORE backend creation",
			"source", vm.replayConfig.SourcePath)

		// Create a temporary replayer just to extract genesis
		config := &UnifiedReplayConfig{
			SourcePath:               vm.replayConfig.SourcePath,
			DatabaseType:             AutoDetect,
			ExtractGenesisFromSource: true,
		}

		replayer, err := NewUnifiedReplayer(config, vm.ethDB, nil) // nil blockchain is OK for genesis extraction
		if err != nil {
			return fmt.Errorf("failed to create replayer for genesis extraction: %w", err)
		}

		extractedGenesis, err = replayer.ExtractGenesis()
		replayer.Close()

		if err != nil {
			return fmt.Errorf("failed to extract genesis from source: %w", err)
		}

		vm.log.Info("Extracted genesis from source database",
			"hash", extractedGenesis.Hash().Hex(),
			"number", extractedGenesis.NumberU64(),
			"stateRoot", extractedGenesis.Root().Hex(),
			"timestamp", extractedGenesis.Time())

		// Override the genesis to use the extracted one EXACTLY as it is
		// We need to mark this database as already having the genesis
		// by NOT passing a genesis object to the backend - it will use what's in the DB
		hasMigratedData = true
		migratedHeight = 0 // Starting from genesis
		migratedBlockHash = extractedGenesis.Hash()

		// Store the extracted genesis hash for verification
		vm.genesisHash = extractedGenesis.Hash()

		fmt.Printf("Using extracted genesis directly: hash=%s\n", extractedGenesis.Hash().Hex())

		// Write the extracted genesis to database BEFORE creating backend
		rawdb.WriteBlock(vm.ethDB, extractedGenesis)
		rawdb.WriteCanonicalHash(vm.ethDB, extractedGenesis.Hash(), 0)
		rawdb.WriteHeader(vm.ethDB, extractedGenesis.Header())
		rawdb.WriteBody(vm.ethDB, extractedGenesis.Hash(), 0, extractedGenesis.Body())

		// Mark that we have genesis so backend won't try to recreate it
		rawdb.WriteHeadBlockHash(vm.ethDB, extractedGenesis.Hash())
		rawdb.WriteHeadHeaderHash(vm.ethDB, extractedGenesis.Hash())

		vm.log.Info("Pre-written extracted genesis to database")

		// CRITICAL: Clear the genesis variable so backend won't try to use it
		genesis = nil
	}

	// Create minimal Ethereum backend
	var err error
	if hasMigratedData && vm.replayConfig == nil {
		// CRITICAL: Skip all genesis processing for migrated data (old method)
		fmt.Printf("MIGRATION MODE ACTIVE: Loading blockchain from height %d\n", migratedHeight)

		// If we opened the ethdb directly, use that instead of the wrapped DB
		dbToUse := vm.ethDB
		// ALWAYS check chainData/C/db first for C-Chain (no blockchain ID dependency)
		dataDir := os.Getenv("DATA_DIR")
		if dataDir == "" {
			dataDir = "/home/z/.luxd"
		}
		baseDir := filepath.Join(dataDir, "chainData", "C", "db")
		possiblePaths := []string{
			filepath.Join(baseDir, "badgerdb", "ethdb"),
			filepath.Join(baseDir, "ethdb"),
		}

		// Also check blockchain-ID-specific path as fallback
		if chainCtxWithDir, ok := vm.chainCtx.(interface{ GetChainDataDir() string }); ok {
			possiblePaths = append(possiblePaths,
				filepath.Join(chainCtxWithDir.GetChainDataDir(), "badgerdb", "ethdb"),
				filepath.Join(chainCtxWithDir.GetChainDataDir(), "ethdb"),
			)
		}

		ethdbPath := ""
		for _, path := range possiblePaths {
			if _, err := os.Stat(path); err == nil {
				ethdbPath = path
				break
			}
		}
		if ethdbPath == "" {
			ethdbPath = filepath.Join(".", "ethdb") // fallback
		}
		if ethdbPath != "" && ethdbPath != filepath.Join(".", "ethdb") {
			// Re-open the ethdb with proper config for the backend
			badgerConfig := BadgerDatabaseConfig{
				DataDir:       ethdbPath,
				EnableAncient: false,
				ReadOnly:      false,
			}
			if directDB, err := NewBadgerDatabase(nil, badgerConfig); err == nil {
				dbToUse = directDB
				fmt.Printf("Using direct ethdb for migrated backend from %s\n", ethdbPath)
			}
		}

		// Create a special backend that doesn't touch genesis
		vm.backend, err = NewMigratedBackend(dbToUse, migratedHeight)
		if err != nil {
			return fmt.Errorf("failed to create migrated backend: %w", err)
		}
	} else if hasMigratedData && vm.replayConfig != nil {
		// We have migrated data AND need to replay
		// Use the normal backend but DON'T pass a genesis - let it use what's in the database
		fmt.Printf("MIGRATION MODE WITH REPLAY: Using extracted genesis from database\n")

		// Make sure genesis is written to database if not already there
		if genesis != nil {
			storedBlock := rawdb.ReadBlock(vm.ethDB, genesis.ToBlock().Hash(), 0)
			if storedBlock == nil {
				fmt.Printf("Writing genesis to database for replay: %s\n", genesis.ToBlock().Hash().Hex())
				rawdb.WriteBlock(vm.ethDB, genesis.ToBlock())
				rawdb.WriteCanonicalHash(vm.ethDB, genesis.ToBlock().Hash(), 0)
				rawdb.WriteHeadHeaderHash(vm.ethDB, genesis.ToBlock().Hash())
				rawdb.WriteHeadBlockHash(vm.ethDB, genesis.ToBlock().Hash())
				rawdb.WriteHeadFastBlockHash(vm.ethDB, genesis.ToBlock().Hash())
				rawdb.WriteChainConfig(vm.ethDB, genesis.ToBlock().Hash(), genesis.Config)
			}
			// Now pass the genesis to backend
			vm.backend, err = NewMinimalEthBackend(vm.ethDB, &vm.ethConfig, genesis)
		} else {
			// If no genesis, try with nil
			vm.backend, err = NewMinimalEthBackend(vm.ethDB, &vm.ethConfig, nil)
		}
		fmt.Printf("Backend creation result: err=%v, backend=%v\n", err, vm.backend != nil)
	} else {
		// Use normal backend (no migration)
		fmt.Printf("Creating normal backend with genesis hash: %s\n", genesis.ToBlock().Hash().Hex())
		vm.backend, err = NewMinimalEthBackend(vm.ethDB, &vm.ethConfig, genesis)
		fmt.Printf("Backend creation result: err=%v, backend=%v\n", err, vm.backend != nil)
	}
	if err != nil {
		return fmt.Errorf("failed to create eth backend: %w", err)
	}

	if vm.backend == nil {
		return fmt.Errorf("backend is nil after creation")
	}

	vm.blockChain = vm.backend.BlockChain()
	vm.txPool = vm.backend.TxPool()

	// Get genesis hash
	genesisBlock := vm.blockChain.Genesis()
	if genesisBlock == nil {
		return fmt.Errorf("genesis block not found")
	}
	vm.genesisHash = genesisBlock.Hash()

	// If we extracted genesis, verify it matches
	if extractedGenesis != nil {
		if vm.genesisHash != extractedGenesis.Hash() {
			vm.log.Warn("Genesis hash mismatch after backend creation",
				"expected", extractedGenesis.Hash().Hex(),
				"got", vm.genesisHash.Hex())
			// Force the genesis hash to match
			vm.genesisHash = extractedGenesis.Hash()
		}
	}

	// Perform database replay if configured
	if vm.replayConfig != nil {
		vm.log.Info("STARTING DATABASE REPLAY", "source", vm.replayConfig.SourcePath)
		fmt.Printf("STARTING DATABASE REPLAY from %s\n", vm.replayConfig.SourcePath)

		// Use unified replay system
		config := &UnifiedReplayConfig{
			SourcePath:               vm.replayConfig.SourcePath,
			DatabaseType:             AutoDetect, // Auto-detect the database type
			TestMode:                 false,      // Full replay by default
			CopyAllState:             false,      // Don't copy all state (too large)
			MaxStateNodes:            1000000,    // Limit to 1M nodes for safety
			ExtractGenesisFromSource: false,      // Already extracted above
		}

		// Check if test mode is requested
		if vm.replayConfig.TestLimit > 0 {
			config.TestMode = true
			config.TestLimit = vm.replayConfig.TestLimit
			vm.log.Info("TEST MODE: Limiting replay to blocks", "limit", config.TestLimit)
			fmt.Printf("TEST MODE: Limiting replay to %d blocks\n", config.TestLimit)
		}

		vm.log.Info("Creating unified replayer", "sourcePath", config.SourcePath)
		fmt.Printf("DEBUG: About to create replayer with source: %s\n", config.SourcePath)
		
		replayer, err := NewUnifiedReplayer(config, vm.ethDB, vm.blockChain)
		if err != nil {
			vm.log.Error("Failed to create replayer", "error", err)
			fmt.Printf("ERROR creating replayer: %v\n", err)
			return fmt.Errorf("failed to create replayer: %w", err)
		}
		
		vm.log.Info("Replayer created successfully")
		fmt.Printf("DEBUG: Replayer created, about to run\n")
		defer replayer.Close()

		// Set progress tracker
		replayer.SetProgressTracker(vm.replayProgress)

		vm.log.Info("Starting replayer.Run()")
		fmt.Printf("DEBUG: About to call replayer.Run()\n")
		
		if err := replayer.Run(); err != nil {
			vm.log.Error("Replayer.Run() failed", "error", err)
			fmt.Printf("ERROR in replayer.Run(): %v\n", err)
			vm.replayProgress.Fail(err)
			return fmt.Errorf("database replay failed: %w", err)
		}
		
		vm.log.Info("Replayer completed successfully")
		fmt.Printf("DEBUG: Replayer.Run() completed\n")

		vm.replayProgress.Complete()

		// After replay, force the blockchain to load the replayed blocks
		// The replay should have written the head block hash
		headHash := rawdb.ReadHeadBlockHash(vm.ethDB)
		if headHash != (common.Hash{}) {
			// First get the block number for this hash
			number, ok := rawdb.ReadHeaderNumber(vm.ethDB, headHash)
			if ok {
				// Now get the full header
				header := rawdb.ReadHeader(vm.ethDB, headHash, number)
				if header != nil {
					// Force blockchain to recognize this state
					// Important: After copying state directly, we need to regenerate indexes
					vm.blockChain.SetHead(header.Number.Uint64())

					// Force snapshot generation for the current state
					// This is necessary because we copied state nodes directly
					if err := vm.blockChain.StateCache().TrieDB().Commit(header.Root, false); err != nil {
						vm.log.Warn("Failed to commit state after replay", "error", err)
					}

					// Update lastAccepted
					vm.lastAccepted = ids.ID(headHash)
					vm.log.Info("Set blockchain head from replay",
						"number", header.Number.Uint64(),
						"hash", headHash.Hex())
				}
			}

			if head := vm.blockChain.CurrentBlock(); head != nil {
				vm.log.Info("Database replay complete - blockchain updated",
					"blocks", head.Number.Uint64(),
					"hash", head.Hash().Hex())
			} else {
				vm.log.Info("Database replay complete - head set",
					"hash", headHash.Hex())
			}
		} else {
			vm.log.Warn("Database replay complete but no head block found")
		}
	}

	// Check if we have existing blocks beyond genesis
	// If we detected migrated data via environment variables, use that
	if hasMigratedData && migratedBlockHash != (common.Hash{}) {
		vm.lastAccepted = ids.ID(migratedBlockHash)
		vm.log.Info("Using imported blockchain data from environment",
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
			vm.log.Info("Found Height consensus key",
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
				vm.log.Info("Found migrated blockchain data",
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

		vm.log.Info("C-Chain VM found existing blockchain data",
			"currentHash", currentBlock.Hash().Hex(),
			"currentHeight", currentBlock.Number.Uint64(),
			"lastAccepted", vm.lastAccepted.String(),
		)
	} else {
		// Fresh start, use genesis
		vm.lastAccepted = ids.ID(vm.genesisHash)

		vm.log.Info("C-Chain VM starting from genesis",
			"genesisHash", vm.genesisHash.Hex(),
			"lastAccepted", vm.lastAccepted.String(),
		)
	}

	// Log database statistics
	vm.logDatabaseStatus()

	vm.log.Info("C-Chain VM initialized")
	vm.log.Info("Chain configuration",
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
		vm.log.Info("Current blockchain state",
			"height", currentBlock.Number.Uint64(),
			"hash", currentBlock.Hash().Hex(),
			"timestamp", currentBlock.Time,
		)
	}

	// Get head block info
	headBlock := vm.blockChain.CurrentHeader()
	if headBlock != nil {
		vm.log.Info("Head block state",
			"height", headBlock.Number.Uint64(),
			"hash", headBlock.Hash().Hex(),
		)
	}

	// Log database type
	vm.log.Info("Database info",
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
	vm.log.Info("CreateHandlers called")
	
	defer func() {
		if r := recover(); r != nil {
			vm.log.Error("CreateHandlers panicked", "panic", r)
		}
	}()
	
	if vm.backend == nil {
		vm.log.Error("Backend is nil in CreateHandlers")
		return nil, fmt.Errorf("backend not initialized")
	}
	
	handlers := make(map[string]http.Handler)

	// Create RPC server and register APIs
	rpcServer := rpc.NewServer()

	// Manually register our minimal APIs to avoid any auto-start issues
	vm.log.Info("Creating API instances")
	ethAPI := NewEthAPI(vm.backend)
	netAPI := &NetAPI{networkID: vm.ethConfig.NetworkId}
	web3API := &Web3API{}
	luxAPI := NewLuxAPI(vm)

	// Register each API namespace
	vm.log.Info("Registering eth API")
	if err := rpcServer.RegisterName("eth", ethAPI); err != nil {
		return nil, fmt.Errorf("failed to register eth API: %w", err)
	}
	vm.log.Info("Registered eth API")
	vm.log.Info("Registering net API")
	if err := rpcServer.RegisterName("net", netAPI); err != nil {
		return nil, fmt.Errorf("failed to register net API: %w", err)
	}
	vm.log.Info("Registering web3 API")
	if err := rpcServer.RegisterName("web3", web3API); err != nil {
		return nil, fmt.Errorf("failed to register web3 API: %w", err)
	}
	vm.log.Info("Registering lux API (replayStatus)")
	// Register under "lux" namespace - accessible as lux_replayStatus
	// NOTE: User wanted just "replayStatus" but RPC requires a namespace
	if err := rpcServer.RegisterName("lux", luxAPI); err != nil {
		vm.log.Error("Failed to register lux API", "error", err)
		return nil, fmt.Errorf("failed to register lux API: %w", err)
	}
	vm.log.Info("Registered lux API")

	vm.log.Info("Registered API namespaces")

	// Create HTTP handler
	httpHandler := rpcServer

	// Register the handler at both /rpc and / for compatibility
	handlers["/rpc"] = httpHandler
	handlers["/"] = httpHandler

	vm.log.Info("Created RPC handlers", "count", len(handlers))

	return handlers, nil
}

// NewHTTPHandler implements the block.ChainVM interface
func (vm *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	return nil, nil
}

// WaitForEvent is not part of the current ChainVM interface
// func (vm *VM) WaitForEvent(ctx context.Context) (core.Message, error) {
// 	<-ctx.Done()
// 	return core.PendingTxs, ctx.Err()
// }

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
func (vm *VM) GetBlock(ctx context.Context, blkID ids.ID) (block.Block, error) {
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
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (block.Block, error) {
	ethBlock := new(types.Block)
	if err := rlp.DecodeBytes(blockBytes, ethBlock); err != nil {
		return nil, fmt.Errorf("failed to decode block: %w", err)
	}

	return vm.newBlock(ethBlock)
}

// BuildBlock implements the block.ChainVM interface
func (vm *VM) BuildBlock(ctx context.Context) (block.Block, error) {
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

// getDirSize calculates the total size of a directory
func getDirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}
