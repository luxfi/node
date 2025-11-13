// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/luxfi/metric"

	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
	consensusctx "github.com/luxfi/consensus/context"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/consensus/core/interfaces"
	"github.com/luxfi/consensus/validator/uptime"
	consensusclock "github.com/luxfi/consensus/utils/timer/mockable"
	validators "github.com/luxfi/consensus/validator"
	consensusversion "github.com/luxfi/consensus/version"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/network"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/utxo"
	"github.com/luxfi/node/vms/platformvm/warp"
	"github.com/luxfi/node/vms/secp256k1fx"

	consensusmanblock "github.com/luxfi/consensus/engine/chain/block"
	blockbuilder "github.com/luxfi/node/vms/platformvm/block/builder"
	blockexecutor "github.com/luxfi/node/vms/platformvm/block/executor"
	platformvmmetrics "github.com/luxfi/node/vms/platformvm/metrics"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	pmempool "github.com/luxfi/node/vms/platformvm/txs/mempool"
	pvalidators "github.com/luxfi/node/vms/platformvm/validators"
)

var (
	_ consensusmanblock.ChainVM                      = (*VM)(nil)
	_ consensusmanblock.BuildBlockWithContextChainVM = (*VM)(nil)
	_ secp256k1fx.VM                            = (*VM)(nil)
	_ validators.State                          = (*VM)(nil)
)

// appSenderAdapter adapts consensusmanblock.AppSender to appsender.AppSender (for network.New)
type appSenderAdapter struct {
	consensusmanblock.AppSender
}

func (a *appSenderAdapter) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	// Send to the first node in the set for compatibility
	for nodeID := range nodeIDs {
		nodeIDsSlice := []ids.NodeID{nodeID}
		return a.AppSender.SendAppRequest(ctx, nodeIDsSlice, requestID, appRequestBytes)
	}
	return nil
}

func (a *appSenderAdapter) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	return a.AppSender.SendAppResponse(ctx, nodeID, requestID, appResponseBytes)
}

func (a *appSenderAdapter) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// Convert set to slice for compatibility
	nodeIDSlice := nodeIDs.List()
	return a.AppSender.SendAppGossip(ctx, nodeIDSlice, appGossipBytes)
}

func (a *appSenderAdapter) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	// Not implemented in linearblock.AppSender, return nil
	return nil
}

func (a *appSenderAdapter) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// Convert set to slice for compatibility
	nodeIDSlice := nodeIDs.List()
	return a.AppSender.SendAppGossip(ctx, nodeIDSlice, appGossipBytes)
}

type VM struct {
	config.Internal
	blockbuilder.Builder
	*network.Network
	validators.State

	metrics platformvmmetrics.Metrics

	// Used to get time. Useful for faking time during tests.
	consensusClock consensusclock.Clock
	nodeClock      mockable.Clock

	uptimeManager uptime.Calculator

	// The context of this vm
	ctx *consensusctx.Context
	db  database.Database

	// Additional fields needed for platformvm
	log        log.Logger
	nodeID     ids.NodeID
	lock       sync.RWMutex
	luxAssetID ids.ID
	chainID    ids.ID
	// bcLookup     consensus.AliasLookup
	// sharedMemory consensus.SharedMemory
	chainDataDir string

	state state.State

	fx            fx.Fx
	codecRegistry codec.Registry

	// Bootstrapped remembers if this chain has finished bootstrapping or not
	bootstrappedConsensus utils.Atomic[bool]
	bootstrapped          utils.Atomic[bool]

	// isInitialized tracks whether VM.Initialize has completed successfully
	// This prevents API calls from accessing uninitialized state
	isInitialized utils.Atomic[bool]

	manager blockexecutor.Manager

	// Cancelled on shutdown
	onShutdownCtx context.Context
	// Call [onShutdownCtxCancel] to cancel [onShutdownCtx] during Shutdown()
	onShutdownCtxCancel context.CancelFunc
}

// GetChainID returns the chain ID of this VM
func (vm *VM) GetChainID(context.Context) (ids.ID, error) {
	return constants.PlatformChainID, nil
}

// Initialize this blockchain.
// [vm.ChainManager] and [vm.vdrMgr] must be set before this function is called.
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtxIntf interface{},
	dbManagerIntf interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngineIntf interface{},
	fxsIntf []interface{},
	appSenderIntf interface{},
) error {
	// Extract chain context
	var chainCtx *consensusctx.Context
	if chainCtxIntf != nil {
		var ok bool
		chainCtx, ok = chainCtxIntf.(*consensusctx.Context)
		if !ok {
			return fmt.Errorf("invalid chain context type")
		}
	} else {
		// Create a minimal context if none provided
		chainCtx = &consensusctx.Context{
			NetworkID: 1,
			ChainID:   constants.PlatformChainID,
		}
	}

	// DBManager is an interface, we'll handle it as such
	dbManager := dbManagerIntf

	// Handle the message channel - it's passed as interface{}
	// We'll handle it without type assertion for now
	_ = toEngineIntf // Suppress unused warning

	// Handle fxs - for now we'll skip type assertions as they're not critical
	_ = fxsIntf

	// Handle appSender
	var appSender consensusmanblock.AppSender
	if appSenderIntf != nil {
		var ok bool
		appSender, ok = appSenderIntf.(consensusmanblock.AppSender)
		if !ok {
			return fmt.Errorf("invalid app sender type")
		}
	}
	// Initialize logger
	vm.log = log.NoLog{}
	vm.log.Debug("initializing platform chain")

	// Log initialization parameters

	execConfig, err := config.GetConfig(configBytes)
	if err != nil {
		return fmt.Errorf("failed to get execution config: %w", err)
	}
	vm.log.Info("using VM execution config", "config", execConfig)

	// Get metrics registerer from chain context, or create new one if not available
	var registerer metric.Registry
	if chainCtx != nil && chainCtx.Metrics != nil {
		if reg, ok := chainCtx.Metrics.(metric.Registry); ok {
			registerer = reg
			if registerer == nil {
				registerer = metric.NewRegistry()
			}
		} else {
			// Create new registerer if chainCtx.Metrics is not a Registry
			registerer = metric.NewRegistry()
		}
	} else {
		// Create new registerer if chainCtx.Metrics is nil
		registerer = metric.NewRegistry()
	}

	// Initialize platformvm-specific metrics
	vm.metrics, err = platformvmmetrics.New(registerer)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}
	vm.log.Info("platformvm metrics initialized successfully")

	// Create metric interface for state

	// Set consensus context
	vm.ctx = chainCtx
	// Get the current database from the DBManager
	// Since DBManager is now an interface{}, we need to handle it differently
	if dbManager != nil {
		// Try to get a database from the manager using reflection or type assertion
		// Check if it has a Current() method
		if dbMgr, ok := dbManager.(interface{ Current() database.Database }); ok {
			vm.db = dbMgr.Current()
		} else {
			// If we can't get a database from the manager, create a memory database
			vm.db = memdb.New()
		}
	} else {
		// Create a memory database as fallback
		vm.db = memdb.New()
	}

	// Note: this codec is never used to serialize anything
	vm.codecRegistry = linearcodec.NewDefault()
	vm.fx = &secp256k1fx.Fx{}
	if err := vm.fx.Initialize(vm); err != nil {
		return fmt.Errorf("failed to initialize fx: %w", err)
	}

	rewards := reward.NewCalculator(vm.RewardConfig)

	vm.log.Info("Creating Platform VM state",
		"genesisLen", len(genesisBytes),
	)
	
	vm.state, err = state.New(
		vm.db,
		genesisBytes,
		registerer,
		vm.Internal.Validators,
		vm.Internal.UpgradeConfig,
		execConfig,
		vm.ctx,
		vm.metrics,
		rewards,
	)
	if err != nil {
		vm.log.Error("Failed to create Platform VM state", "error", err)
		return fmt.Errorf("failed to create state: %w", err)
	}
	vm.log.Info("Platform VM state created successfully")

	validatorManager := pvalidators.NewManager(vm.Internal, vm.state, vm.metrics, &vm.nodeClock)
	vm.State = validatorManager
	utxoHandler := utxo.NewHandler(context.Background(), &vm.nodeClock, vm.fx)
	// Create uptime manager with noop implementation for now
	vm.uptimeManager = &uptime.NoOpCalculator{}
	vm.UptimeLockedCalculator.SetCalculator(constants.PrimaryNetworkID, vm.uptimeManager)

	txExecutorBackend := &txexecutor.Backend{
		Config:       &vm.Internal,
		Ctx:          vm.ctx,
		Clk:          &vm.nodeClock,
		Fx:           vm.fx,
		FlowChecker:  utxoHandler,
		Uptimes:      vm.uptimeManager,
		Rewards:      rewards,
		Bootstrapped: &vm.bootstrapped,
	}

	mempool, err := pmempool.New("mempool", registerer)
	if err != nil {
		return fmt.Errorf("failed to create mempool: %w", err)
	}

	vm.manager = blockexecutor.NewManager(
		mempool,
		vm.metrics,
		vm.state,
		txExecutorBackend,
		validatorManager,
	)

	txVerifier := network.NewLockedTxVerifier(&vm.lock, vm.manager)
	// Create wrapper for AppSender to adapt consensusmanblock.AppSender to network expected interface
	adaptedAppSender := &appSenderAdapter{appSender}

	// Type assert WarpSigner (may be nil for Platform chain)
	var warpSigner warp.Signer
	if chainCtx.WarpSigner != nil {
		var ok bool
		warpSigner, ok = chainCtx.WarpSigner.(warp.Signer)
		if !ok {
			return fmt.Errorf("invalid warp signer type: %T", chainCtx.WarpSigner)
		}
	} else {
		// Create a no-op warp signer for Platform chain
		warpSigner = &noOpWarpSigner{}
	}

	// Create network
	
	vm.Network, err = network.New(
	vm.log,
	vm.nodeID,
	constants.PrimaryNetworkID,
	pvalidators.NewLockedState(
		&vm.lock,
		validatorManager,
	),
	txVerifier,
	mempool,
	txExecutorBackend.Config.PartialSyncPrimaryNetwork,
	adaptedAppSender,
	&chainCtx.Lock,
	vm.state,
	warpSigner,
	registerer,
	execConfig.Network,
)
if err != nil {
	return fmt.Errorf("failed to initialize network: %w", err)
}

	vm.onShutdownCtx, vm.onShutdownCtxCancel = context.WithCancel(context.Background())
	// has better control of the context lock.
// 	go vm.Network.PushGossip(vm.onShutdownCtx)
// 	go vm.Network.PullGossip(vm.onShutdownCtx)

	vm.Builder = blockbuilder.New(
		mempool,
		txExecutorBackend,
		vm.manager,
	)

	// Create all of the chains that the database says exist
	vm.log.Info("about to call initBlockchains")
	if err := vm.initBlockchains(); err != nil {
		return fmt.Errorf(
			"failed to initialize blockchains: %w",
			err,
		)
	}

	lastAcceptedID := vm.state.GetLastAccepted()
	vm.log.Info("initializing last accepted",
		"blkID", lastAcceptedID,
	)
	if err := vm.SetPreference(ctx, lastAcceptedID); err != nil {
		return err
	}

	// Incrementing [awaitShutdown] would cause a deadlock since
	// [periodicallyPruneMempool] grabs the context lock.
	go vm.periodicallyPruneMempool(execConfig.MempoolPruneFrequency)

	go func() {
		// Check if shutdown has been called before starting the reindex
		select {
		case <-vm.onShutdownCtx.Done():
			return
		default:
		}

		err := vm.state.ReindexBlocks(&vm.lock, vm.log)
		if err != nil {
			vm.log.Warn("reindexing blocks failed",
				"error", err,
			)
		}
	}()

	// Mark VM as initialized - this must be done at the very end
	// after all components are properly set up
	vm.isInitialized.Set(true)
	vm.log.Info("Platform VM initialization complete")

	return nil
}

func (vm *VM) periodicallyPruneMempool(frequency time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	for {
		select {
		case <-vm.onShutdownCtx.Done():
			return
		case <-ticker.C:
			if err := vm.pruneMempool(); err != nil {
				vm.log.Debug("pruning mempool failed",
					"error", err,
				)
			}
		}
	}
}

func (vm *VM) pruneMempool() error {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	// Packing all of the transactions in order performs additional checks that
	// the MempoolTxVerifier doesn't include. So, evicting transactions from
	// here is expected to happen occasionally.
	blockTxs, err := vm.Builder.PackAllBlockTxs()
	if err != nil {
		return err
	}

	for _, tx := range blockTxs {
		if err := vm.Builder.Add(tx); err != nil {
			vm.log.Debug(
				"failed to reissue tx",
				"txID", tx.ID(),
				"error", err,
			)
		}
	}

	return nil
}

// checkExistingChains looks for existing blockchain data and registers them
func (vm *VM) checkExistingChains() error {
	// Scan chainData directory for existing chains
	// We need the parent chainData directory, not the P-Chain specific one
	chainDataDir := filepath.Dir(vm.chainDataDir)
	vm.log.Info("checking for existing chains in chainData directory",
		"chainDataDir", chainDataDir,
	)

	entries, err := os.ReadDir(chainDataDir)
	if err != nil {
		vm.log.Info("chainData directory read error",
			"error", err,
		)
		// Directory might not exist yet, that's ok
		return nil
	}

	vm.log.Info("found chainData entries",
		"count", len(entries),
	)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		vm.log.Info("checking chainData entry",
			"name", entry.Name(),
		)

		// Try to parse as chain ID
		chainID, err := ids.FromString(entry.Name())
		if err != nil {
			vm.log.Debug("failed to parse chain ID",
				"name", entry.Name(),
				"error", err,
			)
			continue
		}

		// Check if this chain has a config.json indicating it's an EVM chain
		configPath := filepath.Join(chainDataDir, entry.Name(), "config.json")
		configData, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		// Determine VM type based on directory contents
		var vmID ids.ID
		var netID ids.ID = constants.PrimaryNetworkID // Default to primary network

		// Check for EVM chain (C-Chain)
		if bytes.Contains(configData, []byte("chain-id")) || bytes.Contains(configData, []byte("chainId")) {
			vmID = constants.EVMID
			vm.log.Info("detected EVM chain from config",
				"chainID", chainID.String(),
			)
		} else {
			// Check for other VM types by looking at other files
			// For now, we'll skip non-EVM chains
			vm.log.Debug("skipping non-EVM chain",
				"chainID", chainID.String(),
			)
			continue
		}

		// Check if we need to determine net ID from somewhere
		// For now, assume primary network for orphaned chains

		// Check if this chain is already known
		chains, err := vm.state.GetChains(netID)
		if err != nil {
			vm.log.Warn("failed to get chains for subnet",
				"netID", netID.String(),
				"error", err,
			)
			continue
		}

		chainExists := false
		for _, chain := range chains {
			if chain.ID() == chainID {
				chainExists = true
				break
			}
		}

		if !chainExists {
			// This is an orphaned chain, queue it for creation
			vm.log.Info("found orphaned chain, queuing for creation",
				"chainID", chainID.String(),
				"vmID", vmID.String(),
				"netID", netID.String(),
				"path", filepath.Join(chainDataDir, entry.Name()),
			)

			// For existing chains, we need to provide a minimal but valid genesis
			// The EVM will match this against the existing chain data
			// Extract chainId from config if possible
// 			var chainIDNum uint64 = 96369 // default
// 			if bytes.Contains(configData, []byte(`"chainId": 96369`)) || bytes.Contains(configData, []byte(`"chainId":96369`)) {
// 				chainIDNum = 96369
// 			}

// 			minimalGenesis := fmt.Sprintf(`{
// 				"config": {
// 					"chainId": %d,
// 					"homesteadBlock": 0,
// 					"eip150Block": 0,
// 					"eip155Block": 0,
// 					"eip158Block": 0,
// 					"byzantiumBlock": 0,
// 					"constantinopleBlock": 0,
// 					"petersburgBlock": 0,
// 					"istanbulBlock": 0,
// 					"muirGlacierBlock": 0,
// 					"subnetEVMTimestamp": 0,
// 					"feeConfig": {
// 						"gasLimit": 8000000,
// 						"targetBlockRate": 2,
// 						"minBaseFee": 25000000000,
// 						"targetGas": 15000000,
// 						"baseFeeChangeDenominator": 36,
// 						"minBlockGasCost": 0,
// 						"maxBlockGasCost": 1000000,
// 						"blockGasCostStep": 200000
// 					}
// 				},
// 				"gasLimit": "0x7a1200",
// 				"difficulty": "0x0",
// 				"alloc": {}
// 			}`, chainIDNum)

// 			vm.Internal.QueueExistingChainWithGenesis(chainID, netID, vmID, []byte(minimalGenesis))
		} else {
			vm.log.Debug("chain already registered",
				"chainID", chainID.String(),
			)
		}
	}
	return nil
}

// Create all chains that exist that this node validates.
func (vm *VM) initBlockchains() error {
	if vm.Internal.PartialSyncPrimaryNetwork {
		vm.log.Info("skipping primary network chain creation")
	} else if err := vm.createNet(constants.PrimaryNetworkID); err != nil {
		return err
	}

	// Check if C-Chain needs to be created with migrated data
	// This handles the case where we have migrated blockchain data but no CreateChainTx
	if err := vm.createCChainIfNeeded(); err != nil {
		vm.log.Error("Failed to create C-Chain with migrated data", "error", err)
		// Don't fail initialization, just log the error
	}

	if vm.SybilProtectionEnabled {
		for netID := range vm.TrackedNets {
			if err := vm.createNet(netID); err != nil {
				return err
			}
		}
	} else {
		netIDs, err := vm.state.GetNetIDs()
		if err != nil {
			return err
		}
		for _, netID := range netIDs {
			if err := vm.createNet(netID); err != nil {
				return err
			}
		}
	}
	return nil
}

// createCChainIfNeeded creates the C-Chain if we have migrated data but no CreateChainTx
func (vm *VM) createCChainIfNeeded() error {
	// Check if C-Chain data exists in the chains directory
	// Note: This is the actual blockchain ID generated for C-Chain
	cChainID, _ := ids.FromString("2DZ8vjwArzfrRph2aFK7Zm9YLhx6PRuZqasVPQFH")
	// Use the data directory from the node configuration
	dataDir := os.Getenv("HOME") + "/.luxd"
	chainDataPath := filepath.Join(dataDir, "chains", cChainID.String())

	if _, err := os.Stat(chainDataPath); os.IsNotExist(err) {
		// No C-Chain data, nothing to do
		vm.log.Debug("No C-Chain data found, skipping creation")
		return nil
	}

	// Check if C-Chain is already registered
	chains, err := vm.state.GetChains(constants.PrimaryNetworkID)
	if err != nil {
		return fmt.Errorf("failed to get chains: %w", err)
	}

	for _, chain := range chains {
		if chain.ID() == cChainID {
			// C-Chain already exists
			vm.log.Debug("C-Chain already registered", "chainID", cChainID)
			return nil
		}
	}

	// C-Chain data exists but not registered, create it
	vm.log.Info("Creating C-Chain with migrated data",
		"chainID", cChainID,
		"vmID", constants.EVMID,
		"dataPath", chainDataPath,
	)

	// Create minimal genesis for the migrated C-Chain
	// This matches the migrated blockchain data at height 1,082,780
// 	genesisBytes := []byte(`{
// 		"config": {
// 			"chainId": 96369,
// 			"homesteadBlock": 0,
// 			"eip150Block": 0,
// 			"eip155Block": 0,
// 			"eip158Block": 0,
// 			"byzantiumBlock": 0,
// 			"constantinopleBlock": 0,
// 			"petersburgBlock": 0,
// 			"istanbulBlock": 0,
// 			"muirGlacierBlock": 0,
// 			"berlinBlock": 0,
// 			"londonBlock": 0,
// 			"shanghaiTime": 1607144400,
// 			"cancunTime": 253399622400,
// 			"terminalTotalDifficulty": 0,
// 			"terminalTotalDifficultyPassed": true
// 		},
// 		"nonce": "0x0",
// 		"timestamp": "0x672485c2",
// 		"gasLimit": "0xb71b00",
// 		"difficulty": "0x0",
// 		"alloc": {
// 			"0x9011E888251AB053B7bD1cdB598Db4f9DEd94714": {
// 				"balance": "0x193e5939a08ce9dbd480000000"
// 			}
// 		},
// 		"useMigratedData": true
// 	}`)

	// Queue the C-Chain for creation
	// vm.Internal.QueueExistingChainWithGenesis(
	// 	cChainID,
	// 	constants.PrimaryNetworkID,
	// 	constants.EVMID,
	// 	genesisBytes,
	// )

	// vm.log.Info("C-Chain queued for creation with migrated data")
	return nil
}

// Create the net with ID [netID]
func (vm *VM) createNet(netID ids.ID) error {
	chains, err := vm.state.GetChains(netID)
	if err != nil {
		return err
	}
	for _, chain := range chains {
		tx, ok := chain.Unsigned.(*txs.CreateChainTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chain.Unsigned)
		}
		vm.Internal.CreateChain(chain.ID(), tx)
	}
	return nil
}

// onBootstrapStarted marks this VM as bootstrapping
func (vm *VM) onBootstrapStarted() error {
	vm.bootstrapped.Set(false)
	vm.bootstrappedConsensus.Set(false)
	return vm.fx.Bootstrapping()
}

// onNormalOperationsStarted marks this VM as bootstrapped
func (vm *VM) onNormalOperationsStarted() error {
	if vm.bootstrapped.Get() {
		return nil
	}
	vm.bootstrapped.Set(true)
	vm.bootstrappedConsensus.Set(true)

	if err := vm.fx.Bootstrapped(); err != nil {
		return err
	}

// 	if !vm.uptimeManager.StartedTracking() {
// 		primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
// 		if err := vm.uptimeManager.StartTracking(primaryVdrIDs); err != nil {
// 			return err
// 		}
// 	}

	// Validator logging is not needed for minimal implementation
	// vl := validators.NewLogger(vm.log, constants.PrimaryNetworkID, vm.nodeID)
	// vm.Validators.RegisterSetCallbackListener(constants.PrimaryNetworkID, vl)

	// for subnetID := range vm.TrackedNets {
	// 	vl := validators.NewLogger(vm.log, subnetID, vm.ctx.NodeID)
	// 	vm.Validators.RegisterSetCallbackListener(subnetID, vl)
	// }

	return vm.state.Commit()
}

func (vm *VM) SetState(_ context.Context, stateNum uint32) error {
	state := interfaces.StateEnum(stateNum)
	switch state {
	case interfaces.Bootstrapping:
		return vm.onBootstrapStarted()
	case interfaces.NormalOp:
		return vm.onNormalOperationsStarted()
	default:
		return fmt.Errorf("unknown state: %v", state)
	}
}

// Shutdown this blockchain
func (vm *VM) Shutdown(context.Context) error {
	if vm.db == nil {
		return nil
	}

	vm.onShutdownCtxCancel()

// 	if vm.uptimeManager.StartedTracking() {
// 		primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
// 		if err := vm.uptimeManager.StopTracking(primaryVdrIDs); err != nil {
// 			return err
// 		}
//
// 		if err := vm.state.Commit(); err != nil {
// 			return err
// 		}
// 	}

	var errs []error
	if vm.state != nil {
		errs = append(errs, vm.state.Close())
		vm.state = nil
	}
	// Don't close vm.db as it was provided externally and the caller
	// is responsible for managing its lifecycle
	vm.db = nil
	return errors.Join(errs...)
}

func (vm *VM) ParseBlock(_ context.Context, b []byte) (consensusmanblock.Block, error) {
	// Note: blocks to be parsed are not verified, so we must used blocks.Codec
	// rather than blocks.GenesisCodec
	statelessBlk, err := block.Parse(block.Codec, b)
	if err != nil {
		return nil, err
	}
	return wrapBlock(vm.manager.NewBlock(statelessBlk)), nil
}

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (consensusmanblock.Block, error) {
	return vm.manager.GetBlock(blkID)
}

// LastAccepted returns the block most recently accepted
func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.manager.LastAccepted(), nil
}

// SetPreference sets the preferred block to be the one with ID [blkID]
func (vm *VM) SetPreference(_ context.Context, blkID ids.ID) error {
	vm.manager.SetPreference(blkID)
	return nil
}

func (*VM) Version(context.Context) (string, error) {
	return version.Current.String(), nil
}

// lazyHandlerWrapper delays Service creation until the VM is fully initialized
type lazyHandlerWrapper struct {
	vm      *VM
	handler http.Handler
	once    sync.Once
	err     error
}

// ServeHTTP creates the handler on first request when VM is ready
func (l *lazyHandlerWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.once.Do(func() {
		// Check if VM is bootstrapped and ready for API calls
		if !l.vm.bootstrapped.Get() {
			l.err = fmt.Errorf("VM not fully bootstrapped")
			return
		}

		// Create the actual RPC server now that VM is ready
		server := rpc.NewServer()
		server.RegisterCodec(json.NewCodec(), "application/json")
		server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")

		// Add metrics interceptors if available
		if l.vm.metrics != nil {
			server.RegisterInterceptFunc(l.vm.metrics.InterceptRequest)
			server.RegisterAfterFunc(l.vm.metrics.AfterRequest)
		}

		// Create the service with fully initialized VM
		service := &Service{
			vm:                    l.vm,
			addrManager:           lux.NewAddressManager(l.vm.ctx),
			stakerAttributesCache: lru.NewCache[ids.ID, *stakerAttributes](stakerAttributesCacheSize),
		}

		if err := server.RegisterService(service, "platform"); err != nil {
			l.err = fmt.Errorf("failed to register platform service: %w", err)
			return
		}

		l.handler = server
	})

	// Handle the request or return error
	if l.err != nil {
		http.Error(w, fmt.Sprintf("Platform service initialization error: %v", l.err), http.StatusServiceUnavailable)
		return
	}
	if l.handler == nil {
		http.Error(w, "Platform service not ready, VM still bootstrapping", http.StatusServiceUnavailable)
		return
	}

	l.handler.ServeHTTP(w, r)
}

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
// This now uses lazy initialization to avoid race conditions during VM startup
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	// Return a lazy wrapper that will create the actual handler when ready
	return map[string]http.Handler{
		"": &lazyHandlerWrapper{vm: vm},
	}, nil
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion interface{}) error {
	// Uptime tracking Connect is no longer available on Calculator interface
	// if err := vm.uptimeManager.Connect(nodeID); err != nil {
	//	return err
	// }

	// Type assert nodeVersion to *consensusversion.Application
	var versionApp *consensusversion.Application
	if nodeVersion != nil {
		var ok bool
		versionApp, ok = nodeVersion.(*consensusversion.Application)
		if !ok {
			return fmt.Errorf("invalid node version type: %T", nodeVersion)
		}
	}
	return vm.Network.Connected(ctx, nodeID, versionApp)
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	// Uptime tracking is handled by NoOpCalculator for now
	// if err := vm.uptimeManager.Disconnect(nodeID); err != nil {
	//	return err
	// }
	if err := vm.state.Commit(); err != nil {
		return err
	}
	return vm.Network.Disconnected(ctx, nodeID)
}

func (vm *VM) CodecRegistry() codec.Registry {
	return vm.codecRegistry
}

func (vm *VM) Clock() *mockable.Clock {
	return &vm.nodeClock
}

func (vm *VM) Logger() log.Logger {
	return vm.log
}

func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	return vm.state.GetBlockIDAtHeight(height)
}

func (vm *VM) issueTxFromRPC(tx *txs.Tx) error {
// 	err := vm.Network.IssueTxFromRPC(tx)
// 	if err != nil && !errors.Is(err, mempool.ErrDuplicateTx) {
// 		vm.log.Debug("failed to add tx to mempool",
// 			"txID", tx.ID(),
// 			"error", err,
// 		)
// 		return err
	return nil
}

// NewHTTPHandler returns a new HTTP handler that can handle API calls
// This is required by the consensusmanblock.ChainVM interface
func (vm *VM) NewHTTPHandler(context.Context) (interface{}, error) {
	return nil, nil
}

// WaitForEvent blocks until either the given context is cancelled, or a message is returned
// This is required by the linearblock.ChainVM interface
func (vm *VM) WaitForEvent(ctx context.Context) (interface{}, error) {
	// For now, just block until context is cancelled
	<-ctx.Done()
	return nil, ctx.Err()
}

// noOpWarpSigner is a no-op implementation of warp.Signer for chains that don't need warp signing
type noOpWarpSigner struct{}

func (n *noOpWarpSigner) Sign(msg *warp.UnsignedMessage) ([]byte, error) {
	return nil, nil
}
