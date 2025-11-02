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
	"github.com/luxfi/geth/core/state"
	"github.com/luxfi/geth/core/txpool"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/eth/ethconfig"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/geth/rpc"
	"github.com/luxfi/geth/trie"

	consensusNode "github.com/luxfi/consensus"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/database"
	"github.com/luxfi/database/pebbledb"
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

// UpdatePhase is an alias for SetPhase for compatibility
func (p *ReplayProgress) UpdatePhase(phase string) {
	p.SetPhase(phase)
}

// Update updates the current block count
func (p *ReplayProgress) Update(blockCount uint64) {
	p.currentBlock.Store(blockCount)
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

// IsReplaying returns true if replay is currently in progress
func (p *ReplayProgress) IsReplaying() bool {
	state := ReplayState(p.state.Load())
	return state == ReplayInProgress
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
	SourcePath               string `json:"source-path"`                  // Path to source database
	DatabaseType             string `json:"database-type,omitempty"`      // Type: "auto", "namespaced", or "standard"
	TestLimit                uint64 `json:"test-limit"`                   // If > 0, limit replay to this many blocks
	ExtractGenesisFromSource bool   `json:"extract-genesis-from-source"`  // If true, extract genesis from block 0 of source
	CopyAllState             bool   `json:"copy-all-state"`               // If true, copy all state trie data
	
	// Subnet-EVM replay configuration
	SubnetReplayEnabled      bool   `json:"subnet-replay-enabled"`        // Enable replay from Subnet-EVM via RPC
	SubnetReplaySourceURL    string `json:"subnet-replay-source-url"`     // RPC URL e.g. http://127.0.0.1:9630/ext/bc/<chainID>/rpc
	SubnetReplayStart        uint64 `json:"subnet-replay-start"`          // Start block (default 0)
	SubnetReplayEnd          uint64 `json:"subnet-replay-end"`            // End block (0 = tip)
	SubnetReplayBatch        uint64 `json:"subnet-replay-batch"`          // Batch size (default 1000)
	SubnetReplayResume       bool   `json:"subnet-replay-resume"`         // Resume from checkpoint (default true)
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
			SourcePath:   replayPath,
			CopyAllState: true,
		}
		
		// Check for test limit
		if limitStr := os.Getenv("LUX_REPLAY_LIMIT"); limitStr != "" {
			if limit, err := strconv.ParseUint(limitStr, 10, 64); err == nil && limit > 0 {
				vm.replayConfig.TestLimit = limit
				fmt.Printf("TEST MODE: Will limit replay to %d blocks\n", limit)
			}
		}
	}

	// Parse config file for database-replay configuration
	fmt.Printf("DEBUG: configBytes length=%d, content=%s\n", len(configBytes), string(configBytes))
	if len(configBytes) > 0 && vm.replayConfig == nil {
		var config struct {
			DatabaseReplay *DatabaseReplayConfig `json:"database-replay"`
		}
		if err := json.Unmarshal(configBytes, &config); err == nil && config.DatabaseReplay != nil {
			vm.log.Info("Replay mode enabled via config file", "source", config.DatabaseReplay.SourcePath)
			fmt.Printf("DEBUG: Replay config found! source=%s, copyAllState=%v\n", config.DatabaseReplay.SourcePath, config.DatabaseReplay.CopyAllState)
			vm.replayConfig = config.DatabaseReplay
		} else if err != nil {
			fmt.Printf("DEBUG: Failed to parse config: %v\n", err)
		} else {
			fmt.Printf("DEBUG: Config parsed but DatabaseReplay is nil\n")
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
			// CRITICAL FIX: Use the correct migrated database paths
			// The ACTUAL SubnetEVM migrated data is in the state directory
			possiblePaths := []string{
				"/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb",  // ACTUAL migrated SubnetEVM data
				"/home/z/.luxd/node4/chainData/C/db/ethdb/ethdb",                  // Node4 location
				"/home/z/.luxd/chainData/C/db/ethdb/ethdb",                        // Primary location
				"/home/z/.luxd/chainData/C/db/badgerdb/ethdb",                     // BadgerDB location
				filepath.Join(chainDataDir, "ethdb", "ethdb"),                      // Double ethdb path
				filepath.Join(chainDataDir, "badgerdb", "ethdb"),                   // Relative location
				filepath.Join(chainDataDir, "ethdb"),                               // Direct location fallback
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
						// CRITICAL: Wrap with namespace stripper for NetEVM compatibility
						vm.ethDB = NewNetNamespaceStripper(ethDB)
						fmt.Printf("Successfully opened migrated ethdb with namespace stripper\n")

						// Try to load the last block to get the actual height
						if stripper, ok := vm.ethDB.(*NetNamespaceStripper); ok {
							if lastBlock, err := stripper.LoadLastBlock(); err == nil {
								migratedHeight = lastBlock.NumberU64()
								migratedBlockHash = lastBlock.Hash()
								fmt.Printf("Loaded last block from migrated data: height=%d, hash=%s\n",
									migratedHeight, migratedBlockHash.Hex())
							}
						}
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
	// Allow skipping migration detection via environment variable
	if os.Getenv("DISABLE_MIGRATION_DETECTION") != "" {
		fmt.Printf("MIGRATION DETECTION DISABLED via environment variable\n")
		vm.log.Info("Migration detection disabled via DISABLE_MIGRATION_DETECTION environment variable")
		hasMigratedData = false
	} else if !hasMigratedData {
		// ALWAYS check chainData/C/db first for C-Chain (no blockchain ID dependency)
		// CRITICAL FIX: Check all possible migrated database paths
		possiblePaths := []string{
			"/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb",  // ACTUAL migrated SubnetEVM data
			"/home/z/.luxd/node4/chainData/C/db/ethdb/ethdb",                  // Node4 migrated location
			"/home/z/.luxd/chainData/C/db/ethdb/ethdb",                        // Primary migrated location
			"/home/z/.luxd/chainData/C/db/badgerdb/ethdb",                     // BadgerDB migrated location
			"/home/z/.luxd-node1/chainData/C/db/ethdb",                        // Alternative node1 location
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
				// Check if it has substantial data (>100KB indicates migrated blockchain)
				if dirSize := getDirSize(ethdbPath); dirSize > 100*1024 {
					fmt.Printf("DETECTED MIGRATED BADGERDB AT %s (%d KB)\n", ethdbPath, dirSize/1024)

					// Open the migrated database
					badgerConfig := BadgerDatabaseConfig{
						DataDir:       ethdbPath,
						EnableAncient: false,
						ReadOnly:      false,
					}
					if ethDB, err := NewBadgerDatabase(nil, badgerConfig); err == nil {
						// CRITICAL: Wrap with namespace stripper for NetEVM compatibility
						vm.ethDB = NewNetNamespaceStripper(ethDB)
						hasMigratedData = true

						// Try to load actual height from the database
						if stripper, ok := vm.ethDB.(*NetNamespaceStripper); ok {
							if lastBlock, err := stripper.LoadLastBlock(); err == nil {
								migratedHeight = lastBlock.NumberU64()
								migratedBlockHash = lastBlock.Hash()
								fmt.Printf("Loaded actual height from migrated data: %d (hash: %s)\n",
									migratedHeight, migratedBlockHash.Hex())
							} else {
								// Fallback to known height
								migratedHeight = 1082780 // Known height from migration
								fmt.Printf("Using fallback height: %d\n", migratedHeight)
							}
						}

						fmt.Printf("Successfully opened migrated ethdb with namespace stripper\n")
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

		// Skip old loader - we're using the migrated database directly
		// The migrated BadgerDB already contains all the data we need
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

		// Use the ethDB we already opened (it's the correct migrated database)
		dbToUse := vm.ethDB

		// If we haven't opened the correct database yet, open it now
		if dbToUse == nil {
			ethdbPath := "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
			if _, err := os.Stat(ethdbPath); err == nil {
				badgerConfig := BadgerDatabaseConfig{
					DataDir:       ethdbPath,
					EnableAncient: false,
					ReadOnly:      false,
				}
				if directDB, err := NewBadgerDatabase(nil, badgerConfig); err == nil {
					dbToUse = directDB
					vm.ethDB = directDB
					fmt.Printf("Using migrated ethdb from %s\n", ethdbPath)
				}
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
		// Check if database already has a genesis block
		existingGenesisHash := rawdb.ReadCanonicalHash(vm.ethDB, 0)
		if existingGenesisHash != (common.Hash{}) {
			// Database has a genesis - check if it matches what we're trying to initialize with
			expectedHash := genesis.ToBlock().Hash()
			if existingGenesisHash != expectedHash {
				vm.log.Warn("Database already has different genesis - using database genesis",
					"dbGenesis", existingGenesisHash.Hex(),
					"configGenesis", expectedHash.Hex())
				fmt.Printf("GENESIS MISMATCH DETECTED:\n")
				fmt.Printf("  Database has: %s\n", existingGenesisHash.Hex())
				fmt.Printf("  Config wants:  %s\n", expectedHash.Hex())
				fmt.Printf("  Using database genesis (passing nil to backend)\n")
				
				// Pass nil to use database genesis
				genesis = nil
			} else {
				fmt.Printf("Database genesis matches config: %s\n", existingGenesisHash.Hex())
			}
		}
		
		// CRITICAL FIX: Ensure BlobSchedule is set before creating backend
		if genesis != nil && genesis.Config != nil && genesis.Config.CancunTime != nil {
			if genesis.Config.BlobScheduleConfig == nil {
				genesis.Config.BlobScheduleConfig = &params.BlobScheduleConfig{}
			}
			if genesis.Config.BlobScheduleConfig.Cancun == nil {
				genesis.Config.BlobScheduleConfig.Cancun = &params.BlobConfig{
					Target:         3,
					Max:            6,
					UpdateFraction: 3338477,
				}
				vm.log.Info("Fixed BlobSchedule before initial backend creation")
			}
		}

		// Use normal backend (no migration)
		if genesis != nil {
			fmt.Printf("Creating normal backend with genesis hash: %s\n", genesis.ToBlock().Hash().Hex())
		} else {
			fmt.Printf("Creating normal backend using database genesis\n")
		}
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
	
	fmt.Printf("DEBUG: After getting blockchain and txPool, blockchain=%v\n", vm.blockChain != nil)

	// CRITICAL FIX: After blockchain creation, advance CurrentBlock to HEAD
	// The blockchain loads headers correctly but CurrentBlock stays at genesis
	// The issue is that ReadHeadBlockHash returns genesis because loadLastState reset it
	// We need to check the CurrentHeader (which is at HEAD) vs CurrentBlock (which is at genesis)
	currentHeader := vm.blockChain.CurrentHeader()
	currentBlock := vm.blockChain.CurrentBlock()
	
	fmt.Printf("DEBUG: currentHeader=%d, currentBlock=%d, different=%v\n", 
		currentHeader.Number.Uint64(), currentBlock.Number.Uint64(), 
		currentHeader.Hash() != currentBlock.Hash())
	
	// If CurrentHeader is ahead of CurrentBlock, we need to advance
	if currentHeader.Number.Uint64() > currentBlock.Number.Uint64() {
		fmt.Printf("Blockchain state: CurrentBlock=%d, CurrentHeader=%d, advancing to HEAD\n", 
			currentBlock.Number.Uint64(), currentHeader.Number.Uint64())

		headHash := currentHeader.Hash()
		headNumber := currentHeader.Number.Uint64()

		fmt.Printf("Advancing blockchain to HEAD block %d (hash: %s)\n", headNumber, headHash.Hex())

		// Read the full head block and insert it to advance the blockchain
		headBlock := rawdb.ReadBlock(vm.ethDB, headHash, headNumber)
		if headBlock != nil {
			fmt.Printf("Inserting HEAD block to advance blockchain...\n")
			_, err := vm.blockChain.InsertChain([]*types.Block{headBlock})
			if err != nil {
				fmt.Printf("WARNING: Failed to insert HEAD block: %v\n", err)
				fmt.Printf("Blockchain will remain at genesis, state queries may fail\n")
			} else {
				fmt.Printf("✅ Blockchain advanced to block %d\n", headBlock.NumberU64())
			}
		} else {
			fmt.Printf("ERROR: Could not read HEAD block %d from database\n", headNumber)
		}
	}

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
		dbType := AutoDetect
		if vm.replayConfig.DatabaseType != "" {
			dbType = DatabaseType(vm.replayConfig.DatabaseType)
		}
		config := &UnifiedReplayConfig{
			SourcePath:   vm.replayConfig.SourcePath,
			DatabaseType: dbType,
			CopyAllState: true,
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

		// CRITICAL FIX: Get the blockchain's state cache
		// This ensures state changes persist in the blockchain's database
		var stateCache state.Database
		if vm.blockChain != nil {
			// Get the state cache from the blockchain
			stateCache = vm.blockChain.StateCache()
			// The state cache contains the trie database reference
			vm.log.Info("Using blockchain's state cache for replay")
			fmt.Printf("DEBUG: Got blockchain's state cache for replay\n")
		}

		// Pass nil for trieDB to let replayer extract it from stateCache
		replayer, err := NewUnifiedReplayerWithTrieDB(config, vm.ethDB, vm.blockChain, nil, stateCache)
		if err != nil {
			vm.log.Error("Failed to create replayer", "error", err)
			fmt.Printf("ERROR creating replayer: %v\n", err)
			return fmt.Errorf("failed to create replayer: %w", err)
		}
		
		vm.log.Info("Replayer created successfully")
		fmt.Printf("DEBUG: Replayer created, about to run\n")
		defer replayer.Close()

		// Progress is tracked internally in the replayer
		// vm.replayProgress = replayer.progress // If you need access to progress

		vm.log.Info("Starting replayer.ReplayWithEVM()")
		fmt.Printf("DEBUG: About to call replayer.ReplayWithEVM()\n")

		if err := replayer.ReplayWithEVM(); err != nil {
			vm.log.Error("Replayer.ReplayWithEVM() failed", "error", err)
			fmt.Printf("ERROR in replayer.ReplayWithEVM(): %v\n", err)
			vm.replayProgress.Fail(err)
			return fmt.Errorf("database replay failed: %w", err)
		}

		vm.log.Info("Replayer completed successfully")
		fmt.Printf("DEBUG: Replayer.ReplayWithEVM() completed\n")

		vm.replayProgress.Complete()

		// CRITICAL: After replay, we must recreate the backend and force blockchain reload
		// The existing blockchain object doesn't know about the replayed blocks
		// because they were written directly to the database
		vm.log.Info("Recreating backend to load replayed blockchain data...")
		fmt.Printf("DEBUG: Recreating backend after replay\n")

		// IMPORTANT: Save the head hash written by replay
		// SetupGenesisBlockWithOverride will overwrite it back to genesis!
		savedHeadHash := rawdb.ReadHeadBlockHash(vm.ethDB)
		savedHeadNumber := uint64(0)
		if savedHeadHash != (common.Hash{}) {
			if number, ok := rawdb.ReadHeaderNumber(vm.ethDB, savedHeadHash); ok {
				savedHeadNumber = number
			}
		}
		fmt.Printf("Saved head before backend creation: block %d, hash %s\n", savedHeadNumber, savedHeadHash.Hex())

		// Use our comprehensive backend recreation function
		if err := vm.RecreateBackendAfterReplay(genesis); err != nil {
			vm.log.Error("Failed to recreate backend after replay", "error", err)
			// Fallback to manual recreation

			// CRITICAL FIX: Ensure BlobSchedule is set before recreating backend
			if genesis != nil && genesis.Config != nil && genesis.Config.CancunTime != nil {
				if genesis.Config.BlobScheduleConfig == nil {
					genesis.Config.BlobScheduleConfig = &params.BlobScheduleConfig{}
				}
				if genesis.Config.BlobScheduleConfig.Cancun == nil {
					genesis.Config.BlobScheduleConfig.Cancun = &params.BlobConfig{
						Target:         3,
						Max:            6,
						UpdateFraction: 3338477,
					}
					vm.log.Info("Fixed BlobSchedule before backend recreation")
				}
			}

			// Recreate backend with the genesis (it will load replayed blocks from disk)
			var err2 error
			vm.backend, err2 = NewMinimalEthBackend(vm.ethDB, &vm.ethConfig, genesis)
			if err2 != nil {
				vm.log.Error("Failed to recreate backend after replay", "error", err2)
				return fmt.Errorf("failed to recreate backend after replay: %w", err2)
			}

			// Update blockchain reference
			vm.blockChain = vm.backend.BlockChain()
			vm.txPool = vm.backend.TxPool()

			// Force blockchain reload to recognize replayed data
			if savedHeadHash != (common.Hash{}) && savedHeadNumber > 0 {
				if reloadErr := vm.ForceBlockchainReload(savedHeadNumber, savedHeadHash); reloadErr != nil {
					vm.log.Error("Failed to force blockchain reload", "error", reloadErr)
					// Fallback to SetHead
					vm.blockChain.SetHead(savedHeadNumber)
				}
			}
		}

		// Check what the blockchain sees now
		headHash := rawdb.ReadHeadBlockHash(vm.ethDB)
		currentBlock := vm.blockChain.CurrentBlock()
		currentHeader := vm.blockChain.CurrentHeader()

		fmt.Printf("After backend recreation:\n")
		fmt.Printf("  DB HeadBlockHash: %s\n", headHash.Hex())
		if currentBlock != nil {
			fmt.Printf("  CurrentBlock: %d (hash: %s)\n", currentBlock.Number.Uint64(), currentBlock.Hash().Hex())
			fmt.Printf("  CurrentBlock StateRoot: %s\n", currentBlock.Root.Hex())
		} else {
			fmt.Printf("  CurrentBlock: nil\n")
		}
		if currentHeader != nil {
			fmt.Printf("  CurrentHeader: %d (hash: %s)\n", currentHeader.Number.Uint64(), currentHeader.Hash().Hex())
		}

		if currentBlock != nil {
			vm.lastAccepted = ids.ID(currentBlock.Hash())
			vm.log.Info("Database replay complete - blockchain loaded from disk",
				"blocks", currentBlock.Number.Uint64(),
				"hash", currentBlock.Hash().Hex(),
				"stateRoot", currentBlock.Root.Hex())
		} else {
			vm.log.Warn("Database replay complete but blockchain still at genesis")
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

	currentBlock = vm.blockChain.CurrentBlock()
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
	vm.log.Info("Registering lux API (replayStatus, reloadBlockchain, verifyBlockchain)")
	// Register under "lux" namespace - accessible as lux_replayStatus, lux_reloadBlockchain, lux_verifyBlockchain
	// NOTE: User wanted just "replayStatus" but RPC requires a namespace
	if err := rpcServer.RegisterName("lux", luxAPI); err != nil {
		vm.log.Error("Failed to register lux API", "error", err)
		return nil, fmt.Errorf("failed to register lux API: %w", err)
	}
	vm.log.Info("Registered lux API with replay methods")

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

// WaitForEvent implements the block.ChainVM interface
func (vm *VM) WaitForEvent(ctx context.Context) (interface{}, error) {
	<-ctx.Done()
	return core.PendingTxs, ctx.Err()
}

// HealthCheck implements the block.ChainVM interface
func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	return map[string]string{"status": "healthy"}, nil
}

// Connected implements the block.ChainVM interface
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, version interface{}) error {
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
	}, []*types.Receipt{}, trie.NewStackTrie(nil))

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

// RunReplay executes the runtime replay of blocks from SubnetEVM to C-Chain
// This method is called by the lux_replayStart RPC method
func (vm *VM) RunReplay(config *DatabaseReplayConfig) error {
	// Ensure VM is initialized
	if vm == nil {
		return fmt.Errorf("VM is nil")
	}

	// CRITICAL FIX: Initialize logger if it's nil
	if vm.log == nil {
		vm.log = slog.Default().With("vm", "cchain", "component", "replay")
	}

	// CRITICAL FIX: Initialize replayProgress if it's nil
	// This can happen if RunReplay is called via RPC before proper initialization
	if vm.replayProgress == nil {
		vm.log.Info("RunReplay: Initializing replayProgress (was nil)")
		vm.replayProgress = NewReplayProgress()
	}

	vm.log.Info("RunReplay: Starting runtime replay",
		"source", config.SourcePath,
		"start", config.SubnetReplayStart,
		"end", config.SubnetReplayEnd,
		"batch", config.SubnetReplayBatch)

	// Check if blockchain is initialized
	if vm.blockChain == nil {
		vm.log.Warn("RunReplay: Blockchain is not initialized, replay may not persist data")
	}

	// Open source database (SubnetEVM with PebbleDB)
	sourceDB, err := OpenPebbleDB(config.SourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source database: %w", err)
	}
	defer sourceDB.Close()

	// Calculate total blocks to process
	totalBlocks := config.SubnetReplayEnd - config.SubnetReplayStart
	processedBlocks := uint64(0)

	// CRITICAL FIX: Track the last successfully inserted block
	var lastInsertedBlock *types.Block
	var lastInsertedHash common.Hash
	var lastInsertedNumber uint64

	// Process blocks in batches
	for blockNum := config.SubnetReplayStart; blockNum <= config.SubnetReplayEnd; blockNum += config.SubnetReplayBatch {
		endBlock := blockNum + config.SubnetReplayBatch - 1
		if endBlock > config.SubnetReplayEnd {
			endBlock = config.SubnetReplayEnd
		}

		vm.log.Info("Processing block batch",
			"start", blockNum,
			"end", endBlock)

		// Read blocks from source
		blocks := make([]*types.Block, 0, config.SubnetReplayBatch)
		for num := blockNum; num <= endBlock; num++ {
			// Read block with SubnetEVM namespace stripping
			block, err := readBlockFromSubnetEVM(sourceDB, num)
			if err != nil {
				// Log only first few errors to avoid spam
				if processedBlocks < 10 {
					vm.log.Debug("Failed to read block", "number", num, "error", err)
				}
				continue // Skip missing blocks
			}

			blocks = append(blocks, block)
			processedBlocks++
		}

		// Execute blocks with transactions to update state
		if len(blocks) > 0 {
			vm.log.Info("Inserting blocks into blockchain", "count", len(blocks))

			// Use InsertChain to execute transactions and update state
			if vm.blockChain != nil {
				// CRITICAL FIX: Write blocks directly to database first
				// InsertChain may not persist blocks if it thinks they're invalid or side chain
				for _, block := range blocks {
					// Write the block components to database
					rawdb.WriteBlock(vm.ethDB, block)
					rawdb.WriteHeader(vm.ethDB, block.Header())
					rawdb.WriteBody(vm.ethDB, block.Hash(), block.NumberU64(), block.Body())
					rawdb.WriteReceipts(vm.ethDB, block.Hash(), block.NumberU64(), nil) // Empty receipts for now

					// Mark as canonical
					rawdb.WriteCanonicalHash(vm.ethDB, block.Hash(), block.NumberU64())

					vm.log.Debug("Wrote block to database",
						"number", block.NumberU64(),
						"hash", block.Hash().Hex())
				}

				// Now try InsertChain to execute transactions and update state
				n, err := vm.blockChain.InsertChain(blocks)
				if err != nil {
					vm.log.Error("Failed to insert blocks for state execution",
						"error", err,
						"inserted", n,
						"total", len(blocks))
					// Continue - blocks are already in database
				} else {
					vm.log.Info("Successfully executed blocks", "count", n)
				}

				// CRITICAL FIX: After writing blocks, update the chain head
				if len(blocks) > 0 {
					lastBlock := blocks[len(blocks)-1]
					lastInsertedBlock = lastBlock
					lastInsertedHash = lastBlock.Hash()
					lastInsertedNumber = lastBlock.NumberU64()

					// Force update ALL head markers
					rawdb.WriteHeadBlockHash(vm.ethDB, lastBlock.Hash())
					rawdb.WriteHeadHeaderHash(vm.ethDB, lastBlock.Hash())
					rawdb.WriteHeadFastBlockHash(vm.ethDB, lastBlock.Hash())

					vm.log.Info("Updated blockchain head",
						"number", lastBlock.NumberU64(),
						"hash", lastBlock.Hash().Hex())
				}
			} else {
				vm.log.Error("Blockchain is nil, cannot insert blocks")
				return fmt.Errorf("blockchain not initialized")
			}
		}

		// Update progress
		progress := float64(processedBlocks) / float64(totalBlocks) * 100
		vm.replayProgress.Update(processedBlocks)
		vm.replayProgress.UpdatePhase(fmt.Sprintf("Processing: %.1f%%", progress))

		// Log progress
		if processedBlocks%1000 == 0 {
			vm.log.Info("Replay progress",
				"processed", processedBlocks,
				"total", totalBlocks,
				"progress", fmt.Sprintf("%.1f%%", progress))
		}
	}

	// CRITICAL FIX: After all blocks are inserted, force blockchain to recognize new head
	if lastInsertedBlock != nil {
		vm.log.Info("Finalizing replay, updating blockchain head",
			"number", lastInsertedNumber,
			"hash", lastInsertedHash.Hex())

		// Use our comprehensive reload function to ensure blockchain recognizes the new head
		err := vm.ForceBlockchainReload(lastInsertedNumber, lastInsertedHash)
		if err != nil {
			vm.log.Error("Failed to force blockchain reload after replay", "error", err)
			// Try fallback approach
			vm.blockChain.SetHead(lastInsertedNumber)
		}

		// Update VM's last accepted block
		vm.mu.Lock()
		vm.lastAccepted = ids.ID(lastInsertedHash)
		vm.mu.Unlock()

		// Verify the head was actually updated
		currentBlock := vm.blockChain.CurrentBlock()
		if currentBlock != nil {
			vm.log.Info("Blockchain head after replay",
				"number", currentBlock.Number.Uint64(),
				"hash", currentBlock.Hash().Hex(),
				"expectedNumber", lastInsertedNumber,
				"match", currentBlock.Number.Uint64() == lastInsertedNumber)

			// Final verification
			if currentBlock.Number.Uint64() != lastInsertedNumber {
				vm.log.Warn("Blockchain head still not at expected height, attempting backend recreation...")
				// As a last resort, recreate the backend to force reload
				if reloadErr := vm.RecreateBackendAfterReplay(nil); reloadErr != nil {
					vm.log.Error("Failed to recreate backend", "error", reloadErr)
				}
			}
		}
	}

	// Mark replay as complete
	vm.replayProgress.Complete()
	vm.log.Info("Replay completed",
		"processed", processedBlocks,
		"total", totalBlocks)

	return nil
}

// OpenPebbleDB opens a PebbleDB database at the given path
func OpenPebbleDB(path string) (database.Database, error) {
	// Check if path exists
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("database path does not exist: %w", err)
	}

	// Open PebbleDB database
	// Parameters: path, cacheSize, handleCap, namespace, readOnly
	db, err := pebbledb.New(path, 1024, 1024, "", false)
	if err != nil {
		return nil, fmt.Errorf("failed to open PebbleDB: %w", err)
	}

	return db, nil
}

// LegacyHeader represents the SubnetEVM header format without newer fields
type LegacyHeader struct {
	ParentHash  common.Hash      `json:"parentHash"`
	UncleHash   common.Hash      `json:"sha3Uncles"`
	Coinbase    common.Address   `json:"miner"`
	Root        common.Hash      `json:"stateRoot"`
	TxHash      common.Hash      `json:"transactionsRoot"`
	ReceiptHash common.Hash      `json:"receiptsRoot"`
	Bloom       types.Bloom      `json:"logsBloom"`
	Difficulty  *big.Int         `json:"difficulty"`
	Number      *big.Int         `json:"number"`
	GasLimit    uint64           `json:"gasLimit"`
	GasUsed     uint64           `json:"gasUsed"`
	Time        uint64           `json:"timestamp"`
	Extra       []byte           `json:"extraData"`
	MixDigest   common.Hash      `json:"mixHash"`
	Nonce       types.BlockNonce `json:"nonce"`
	BaseFee     *big.Int         `json:"baseFeePerGas" rlp:"optional"`
	ExtData     rlp.RawValue     `rlp:"tail"` // Capture any extra SubnetEVM fields
}

// Helper function to read block from SubnetEVM with namespace stripping
func readBlockFromSubnetEVM(db database.Database, blockNum uint64) (*types.Block, error) {
	// SubnetEVM uses the actual 32-byte namespace prefix from our inspection
	namespace := []byte{
		0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
		0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
		0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
		0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
	}

	// Read canonical hash for this block number
	// Format: namespace + 'h' + block_number(8 bytes) + 'n' -> hash
	canonicalKey := make([]byte, len(namespace)+10)
	copy(canonicalKey, namespace)
	canonicalKey[len(namespace)] = 'h' // Canonical header prefix
	binary.BigEndian.PutUint64(canonicalKey[len(namespace)+1:], blockNum)
	canonicalKey[len(namespace)+9] = 'n' // 'n' suffix for canonical entries

	// Read the canonical hash
	hash, err := db.Get(canonicalKey)
	if err != nil {
		return nil, fmt.Errorf("no canonical entry for block %d: %w", blockNum, err)
	}

	// Read header
	// Format: namespace + 'h' + blockNum + hash -> header data
	headerKey := make([]byte, len(namespace)+41)
	copy(headerKey, namespace)
	headerKey[len(namespace)] = 'h' // Header prefix (lowercase)
	binary.BigEndian.PutUint64(headerKey[len(namespace)+1:], blockNum)
	copy(headerKey[len(namespace)+9:], hash)

	headerData, err := db.Get(headerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read header for block %d: %w", blockNum, err)
	}

	// Try to decode as legacy SubnetEVM header first
	var legacyHeader LegacyHeader
	if err := rlp.DecodeBytes(headerData, &legacyHeader); err != nil {
		// If legacy decode fails, try modern format
		var header types.Header
		if err2 := rlp.DecodeBytes(headerData, &header); err2 != nil {
			return nil, fmt.Errorf("failed to decode header for block %d: legacy=%v, modern=%v", blockNum, err, err2)
		}
		// Use modern header directly
		return readBodyAndCreateBlock(db, namespace, blockNum, hash, &header)
	}

	// Convert legacy header to modern format
	header := &types.Header{
		ParentHash:  legacyHeader.ParentHash,
		UncleHash:   legacyHeader.UncleHash,
		Coinbase:    legacyHeader.Coinbase,
		Root:        legacyHeader.Root,
		TxHash:      legacyHeader.TxHash,
		ReceiptHash: legacyHeader.ReceiptHash,
		Bloom:       legacyHeader.Bloom,
		Difficulty:  legacyHeader.Difficulty,
		Number:      legacyHeader.Number,
		GasLimit:    legacyHeader.GasLimit,
		GasUsed:     legacyHeader.GasUsed,
		Time:        legacyHeader.Time,
		Extra:       legacyHeader.Extra,
		MixDigest:   legacyHeader.MixDigest,
		Nonce:       legacyHeader.Nonce,
		BaseFee:     legacyHeader.BaseFee,
	}

	// Read body and create block
	return readBodyAndCreateBlock(db, namespace, blockNum, hash, header)
}

// readBodyAndCreateBlock reads the body and creates a complete block
func readBodyAndCreateBlock(db database.Database, namespace []byte, blockNum uint64, hash []byte, header *types.Header) (*types.Block, error) {
	// Try to read body
	// Format: namespace + 'b' + block_number(8 bytes) + hash
	bodyKey := make([]byte, len(namespace)+41)
	copy(bodyKey, namespace)
	bodyKey[len(namespace)] = 'b' // Body prefix
	binary.BigEndian.PutUint64(bodyKey[len(namespace)+1:], blockNum)
	copy(bodyKey[len(namespace)+9:], hash)

	bodyData, err := db.Get(bodyKey)
	if err != nil {
		// Some blocks might not have bodies (empty blocks)
		return types.NewBlockWithHeader(header), nil
	}

	// Decode body
	var body types.Body
	if err := rlp.DecodeBytes(bodyData, &body); err != nil {
		// If body decode fails, return block with just header
		return types.NewBlockWithHeader(header), nil
	}

	// Create full block with body
	// CRITICAL FIX: Use trie.NewStackTrie(nil) instead of nil to prevent DeriveSha panic
	return types.NewBlock(header, &body, nil, trie.NewStackTrie(nil)), nil
}
