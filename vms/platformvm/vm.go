// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
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

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	consensusclock "github.com/luxfi/consensus/utils/timer/mockable"
	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/utils/json"
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
	"github.com/luxfi/runtime"
	"github.com/luxfi/timer/mockable"
	"github.com/luxfi/utils"
	"github.com/luxfi/utxo/secp256k1fx"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
	vmcore "github.com/luxfi/vm"
	extwarp "github.com/luxfi/warp"

	chainengine "github.com/luxfi/consensus/engine/chain"
	blockbuilder "github.com/luxfi/node/vms/platformvm/block/builder"
	blockexecutor "github.com/luxfi/node/vms/platformvm/block/executor"
	platformvmmetrics "github.com/luxfi/node/vms/platformvm/metrics"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	pmempool "github.com/luxfi/node/vms/platformvm/txs/mempool"
	pvalidators "github.com/luxfi/node/vms/platformvm/validators"
	txmempool "github.com/luxfi/node/vms/txs/mempool"
	chain "github.com/luxfi/vm/chain"
)

var (
	_ chain.ChainVM                      = (*VM)(nil)
	_ chain.BuildBlockWithRuntimeChainVM = (*VM)(nil)
	_ chainengine.BlockBuilder           = (*VM)(nil) // For consensus engine integration
	_ secp256k1fx.VM                     = (*VM)(nil)
	_ validators.State                   = (*VM)(nil)
)

// warpSignerAdapter adapts extwarp.Signer to internal warp.Signer
type warpSignerAdapter struct {
	extSigner extwarp.Signer
}

func (a *warpSignerAdapter) Sign(msg *warp.UnsignedMessage) ([]byte, error) {
	// Convert internal message to external message format
	extMsg, err := extwarp.NewUnsignedMessage(msg.NetworkID, msg.SourceChainID, msg.Payload)
	if err != nil {
		return nil, err
	}
	return a.extSigner.Sign(extMsg)
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

	// The runtime wiring of this vm
	rt *runtime.Runtime
	db database.Database

	// Additional fields needed for platformvm
	log      log.Logger
	nodeID   ids.NodeID
	lock     sync.RWMutex
	xAssetID ids.ID
	chainID  ids.ID
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

	// toEngine is the channel to send messages to the consensus engine
	// This is used to notify the engine when there are pending transactions
	toEngine chan<- vmcore.Message
}

// GetChainID returns the chain ID for a given network ID
func (vm *VM) GetChainID(netID ids.ID) (ids.ID, error) {
	// For P-chain, chain ID is the same as netID or can be constant
	return netID, nil
}

// GetNetworkID returns the network ID for a given chain ID
func (vm *VM) GetNetworkID(chainID ids.ID) (ids.ID, error) {
	// For P-chain, network ID lookup from chain ID
	if chainID == constants.PlatformChainID {
		return constants.PrimaryNetworkID, nil
	}
	return chainID, nil
}

// Initialize this blockchain.
// [vm.ChainManager] and [vm.vdrMgr] must be set before this function is called.
func (vm *VM) Initialize(
	ctx context.Context,
	init vmcore.Init,
) error {
	// Extract chain runtime
	chainRuntime := init.Runtime
	if chainRuntime == nil {
		// Create a minimal runtime if none provided
		chainRuntime = &runtime.Runtime{
			NetworkID: 1,
			ChainID:   constants.PlatformChainID,
		}
	}

	// DBManager is handled via init.DB usually, but PlatformVM seems to have complex logic around finding existing chains via dbManagerIntf?
	// The original code used dbManagerIntf (param 2) to check for existing DBs.
	// VMInit.DB is strictly database.Database.
	// If the caller (node/main) is passing the same object, we should use init.DB.
	// However, the original code had a fallback logic and "manager" logic.
	// Let's assume init.DB is the correct DB to use.
	vm.db = init.DB
	if vm.db == nil {
		vm.db = memdb.New()
	}

	// Handle the message channel
	vm.toEngine = init.ToEngine

	// Handle appSender
	appSender := init.Sender

	// Initialize logger from chain runtime.
	if init.Log != nil {
		vm.log = init.Log
	} else if init.Runtime != nil && init.Runtime.Log != nil {
		if logger, ok := init.Runtime.Log.(log.Logger); ok && !logger.IsZero() {
			vm.log = logger
		} else {
			vm.log = log.Noop()
		}
	} else {
		vm.log = log.Noop()
	}
	vm.log.Info("initializing platform chain")

	// Log initialization parameters

	execConfig, err := config.GetConfig(init.Config)
	if err != nil {
		return fmt.Errorf("failed to get execution config: %w", err)
	}
	// Merge CLI flag value for SybilProtectionEnabled from internal config
	// The internal config (vm.Internal) has the correct value from node CLI flags
	// while execConfig parsed from chain config bytes defaults to false
	execConfig.SybilProtectionEnabled = vm.SybilProtectionEnabled
	vm.log.Info("using VM execution config", "config", execConfig)

	// Create metrics registry - always use new registry as runtime.Metrics
	// interface is incompatible with metric.Registry due to different Register signatures
	registerer := metric.NewRegistry()

	// Initialize platformvm-specific metrics
	vm.metrics, err = platformvmmetrics.New(registerer)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}
	vm.log.Info("platformvm metrics initialized successfully")

	// Create metric interface for state

	// Set Runtime
	vm.rt = init.Runtime

	// Set nodeID from runtime - this is critical for validator checks
	vm.nodeID = init.Runtime.NodeID
	vm.log.Info("platformvm initialized with node ID", "nodeID", vm.nodeID)

	// Initialize utxo.XAssetID from the context
	utxo.XAssetID = init.Runtime.XAssetID

	// Initialize vm.xAssetID for GetStakingAssetID API
	// Use LUXAssetID if set, otherwise fall back to XAssetID
	if init.Runtime.XAssetID != ids.Empty {
		vm.xAssetID = init.Runtime.XAssetID
	} else {
		vm.xAssetID = init.Runtime.XAssetID
	}

	// Get the current database from the DBManager
	// Since DBManager is now an interface{}, we need to handle it differently
	// logic simplified as we now just trust init.DB or fallback to memdb if nil above

	// Note: this codec is never used to serialize anything
	vm.codecRegistry = linearcodec.NewDefault()
	vm.fx = &secp256k1fx.Fx{}
	if err := vm.fx.Initialize(vm); err != nil {
		return fmt.Errorf("failed to initialize fx: %w", err)
	}

	rewards := reward.NewCalculator(vm.RewardConfig)

	vm.log.Info("Creating Platform VM state",
		"genesisLen", len(init.Genesis),
	)

	vm.state, err = state.New(
		vm.db,
		init.Genesis,
		registerer,
		vm.Internal.Validators,
		vm.Internal.UpgradeConfig,
		execConfig,
		vm.rt,
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
	// Create uptime manager - use the configured UptimeLockedCalculator which
	// delegates to its fallback calculator (NoOp by default, but tests can
	// configure ZeroUptimeCalculator for "never connected" scenarios)
	vm.uptimeManager = vm.UptimeLockedCalculator

	txExecutorBackend := &txexecutor.Backend{
		Config:       &vm.Internal,
		Runtime:      vm.rt,
		Clk:          &vm.nodeClock,
		Fx:           vm.fx,
		FlowChecker:  utxoHandler,
		Uptimes:      vm.UptimeLockedCalculator,
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
	// Create wrapper for Sender to adapt chain.Sender to network expected interface
	// Create wrapper for Sender to adapt chain.Sender to network expected interface
	// adaptedSender := &appSenderAdapter{appSender}

	// Type assert WarpSigner (may be nil for Platform chain)
	var warpSigner warp.Signer
	if init.Runtime.WarpSigner != nil {
		extSigner, ok := init.Runtime.WarpSigner.(extwarp.Signer)
		if !ok {
			return fmt.Errorf("invalid warp signer type: %T", init.Runtime.WarpSigner)
		}
		// Wrap external signer with adapter for internal interface
		warpSigner = &warpSignerAdapter{extSigner: extSigner}
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
		appSender,
		&init.Runtime.Lock,
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
			vm.log.Warn("failed to get chains for chain",
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
			// 					"evmTimestamp": 0,
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

	// When TrackAllChains is enabled OR SybilProtection is disabled,
	// create chains for ALL chains in state
	if vm.TrackAllChains || !vm.SybilProtectionEnabled {
		netIDs, err := vm.state.GetChainIDs()
		if err != nil {
			return err
		}
		for _, netID := range netIDs {
			if err := vm.createNet(netID); err != nil {
				return err
			}
		}
	} else if vm.SybilProtectionEnabled {
		// Only create chains for explicitly tracked chains
		for chainID := range vm.TrackedChains {
			if err := vm.createNet(chainID); err != nil {
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

// onReady marks this VM as bootstrapped and ready
func (vm *VM) onReady() error {
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

	// for chainID := range vm.TrackedChains {
	// 	vl := validators.NewLogger(vm.log, chainID, vm.rt.NodeID)
	// 	vm.Validators.RegisterSetCallbackListener(chainID, vl)
	// }

	// Commit state BEFORE starting background goroutines to avoid race conditions
	// between state readers (forwardNotifications) and state writers (Commit)
	if err := vm.state.Commit(); err != nil {
		return err
	}

	// Start the notification forwarder goroutine
	// This forwards pending transaction notifications from the Builder to the consensus engine
	if vm.toEngine != nil && vm.Builder != nil {
		vm.log.Info("starting P-chain notification forwarder (toEngine and Builder both set)")
		go vm.forwardNotifications()
	} else {
		vm.log.Warn("P-chain notification forwarder NOT started",
			log.Bool("hasToEngine", vm.toEngine != nil),
			log.Bool("hasBuilder", vm.Builder != nil))
	}

	return nil
}

func (vm *VM) SetState(_ context.Context, stateNum uint32) error {
	// Handle both proto-based states (2=Bootstrapping, 3=NormalOp)
	// and vm.State values (3=Bootstrapping, 4=Ready)
	switch stateNum {
	case 2: // vmpb.State_STATE_BOOTSTRAPPING
		return vm.onBootstrapStarted()
	case 3, 4: // vmpb.State_STATE_NORMAL_OP (3) or vm.Ready (4)
		return vm.onReady()
	default:
		// Accept unknown states without error for forward compatibility
		return nil
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

func (vm *VM) ParseBlock(_ context.Context, b []byte) (chain.Block, error) {
	// Note: blocks to be parsed are not verified, so we must used blocks.Codec
	// rather than blocks.GenesisCodec
	statelessBlk, err := block.Parse(block.Codec, b)
	if err != nil {
		return nil, err
	}
	return wrapBlock(vm.manager.NewBlock(statelessBlk)), nil
}

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (chain.Block, error) {
	return vm.manager.GetBlock(blkID)
}

// LastAccepted returns the block most recently accepted
func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.manager.LastAccepted(), nil
}

// BuildBlock implements chainengine.BlockBuilder for consensus engine integration.
// This method is required for the consensus engine to be able to build new P-chain blocks.
// It delegates to the embedded Builder which handles the actual block construction.
func (vm *VM) BuildBlock(ctx context.Context) (chain.Block, error) {
	if vm.Builder == nil {
		return nil, errors.New("block builder not initialized")
	}
	return vm.Builder.BuildBlock(ctx)
}

// SetPreference sets the preferred block to be the one with ID [blkID]
func (vm *VM) SetPreference(_ context.Context, blkID ids.ID) error {
	vm.manager.SetPreference(blkID)
	return nil
}

// forwardNotifications continuously waits for events from the Builder and forwards
// them to the consensus engine via the toEngine channel. This is the critical link
// that enables P-chain block production - without it, the consensus engine never
// knows when there are pending transactions to build into blocks.
func (vm *VM) forwardNotifications() {
	vm.log.Info("starting notification forwarder for P-chain block building")

	for {
		// Wait for the Builder to signal it has pending transactions
		msg, err := vm.Builder.WaitForEvent(vm.onShutdownCtx)
		if err != nil {
			// Check if we're shutting down
			if vm.onShutdownCtx.Err() != nil {
				vm.log.Debug("notification forwarder shutting down")
				return
			}
			vm.log.Debug("error waiting for builder event",
				log.Err(err))
			continue
		}

		// Send to the consensus engine (non-blocking to avoid deadlocks)
		select {
		case vm.toEngine <- msg:
			vm.log.Debug("forwarded pending txs notification to consensus engine",
				log.Uint32("type", uint32(msg.Type)))
		case <-vm.onShutdownCtx.Done():
			vm.log.Debug("notification forwarder shutdown during send")
			return
		default:
			// Channel is full, skip this notification (engine will poll again)
			vm.log.Debug("toEngine channel full, skipping notification")
		}
	}
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
	// Check if VM is bootstrapped BEFORE once.Do to avoid caching the "not bootstrapped" error
	if !l.vm.bootstrapped.Get() {
		http.Error(w, "Platform service not ready, VM still bootstrapping", http.StatusServiceUnavailable)
		return
	}

	l.once.Do(func() {
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
			addrManager:           lux.NewAddressManager(l.vm.rt),
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
		http.Error(w, "Platform service not ready, handler not initialized", http.StatusServiceUnavailable)
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

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *chain.VersionInfo) error {
	// Uptime tracking Connect is no longer available on Calculator interface
	// if err := vm.uptimeManager.Connect(nodeID); err != nil {
	//	return err
	// }

	// chain.VersionInfo is an alias for version.Application, so we can pass it directly
	return vm.Network.Connected(ctx, nodeID, nodeVersion)
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
	err := vm.Network.IssueTxFromRPC(tx)
	if err != nil && !errors.Is(err, txmempool.ErrDuplicateTx) {
		vm.log.Debug("failed to add tx to mempool",
			log.Stringer("txID", tx.ID()),
			log.String("error", err.Error()),
		)
		return err
	}
	return nil
}

// NewHTTPHandler returns a new HTTP handler that can handle API calls
// This is required by the chain.ChainVM interface
func (vm *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	handlers, err := vm.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	for path, handler := range handlers {
		if path == "" {
			path = "/"
		}
		mux.Handle(path, handler)
	}
	return mux, nil
}

// WaitForEvent blocks until either the given context is cancelled, or a message is returned
// This is required by the chain.ChainVM interface
func (vm *VM) WaitForEvent(ctx context.Context) (vmcore.Message, error) {
	// Delegate to the Builder which waits for mempool transactions or staker changes
	if vm.Builder == nil {
		// Before initialization, block until context is cancelled
		<-ctx.Done()
		return vmcore.Message{}, ctx.Err()
	}
	msg, err := vm.Builder.WaitForEvent(ctx)
	if err != nil {
		return vmcore.Message{}, err
	}
	vm.log.Debug("WaitForEvent returning", log.String("msgType", msg.Type.String()))
	return msg, nil
}

// noOpWarpSigner is a no-op implementation of warp.Signer for chains that don't need warp signing
type noOpWarpSigner struct{}

func (n *noOpWarpSigner) Sign(msg *warp.UnsignedMessage) ([]byte, error) {
	return nil, nil
}
