// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpcvm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/log"

	"github.com/luxfi/node/v2/api/metrics"
	"github.com/luxfi/database"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/network/p2p"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/quasar/engine/chain/block"
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/node/v2/utils/profiler"
	"github.com/luxfi/node/v2/utils/timer/mockable"

	statesyncclient "github.com/luxfi/node/v2/state_sync/client"
)

const (
	// MPC-Chain configuration
	mpcChainID         = 1337 // MPC-Chain network ID
	acceptedCacheSize  = 32
	decidedCacheSize   = 128
	missingCacheSize   = 128
	unverifiedCacheSize = 128

	// Database prefixes
	stateDBPrefix       byte = 0x00
	blockDBPrefix       byte = 0x01
	blockHeightDBPrefix byte = 0x02
	metadataDBPrefix    byte = 0x03
	warpDBPrefix        byte = 0x04
	mpcDBPrefix         byte = 0x05
	zkDBPrefix          byte = 0x06
	teleportDBPrefix    byte = 0x07
	validatorsDBPrefix  byte = 0x08
)

var _ block.ChainVM = (*VM)(nil)
var _ block.StateSyncableVM = (*VM)(nil)

// VM implements the MPC-Chain Virtual Machine
type VM struct {
	// IDs
	ctx     *quasar.Context
	mpcvmID ids.ID

	// Databases
	baseDB          database.Database
	db              database.Database
	stateDB         database.Database
	blockDB         database.Database
	metadataDB      database.Database
	warpDB          database.Database
	mpcDB           database.Database
	zkDB            database.Database
	teleportDB      database.Database
	validatorsDB    database.Database

	// Block Management
	acceptedBlockDB database.Database
	chaindb         database.Database
	blockChain      *BlockChain
	genesisHash     common.Hash
	clock           mockable.Clock
	mempool         *Mempool

	// Network
	networkHandler  NetworkHandler
	p2pClient       p2p.Client
	validators      *ValidatorSet

	// MPC Components
	mpcManager      *MPCManager
	mpcWallet       *MPCWallet
	keyGenProtocol  *KeyGenProtocol
	signProtocol    *SignProtocol
	reshareProtocol *ReshareProtocol

	// ZK Components
	zkVerifier      *ZKVerifier
	zkProver        *ZKProver

	// Teleport Protocol
	teleportEngine  *TeleportEngine
	intentPool      *IntentPool
	executorEngine  *ExecutorEngine
	assetRegistry   *AssetRegistry

	// Warp Messaging
	// warpBackend     warp.Backend // TODO: fix warp.Backend type

	// State Management
	stateSyncClient statesyncclient.Client
	// stateSyncServer stateSyncServer.StateSyncServer // TODO: fix type

	// API
	apiServer       *APIServer

	// Configuration
	config          Config

	// Shutdown handling
	shutdownChan    chan struct{}
	shutdownWg      sync.WaitGroup

	// Metrics
	metrics         metrics.MultiGatherer

	// Profiling
	profiler        profiler.ContinuousProfiler
}

// Initialize implements the block.ChainVM interface
func (vm *VM) Initialize(
	_ context.Context,
	ctx *quasar.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*core.Fx,
	appSender core.AppSender,
) error {
	vm.ctx = ctx
	vm.baseDB = db

	// Parse configuration
	if err := vm.config.Parse(configBytes); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Initialize databases
	if err := vm.initializeDBs(); err != nil {
		return fmt.Errorf("failed to initialize databases: %w", err)
	}

	// Initialize blockchain components
	if err := vm.initializeBlockchain(genesisBytes); err != nil {
		return fmt.Errorf("failed to initialize blockchain: %w", err)
	}

	// Initialize MPC components
	if err := vm.initializeMPC(); err != nil {
		return fmt.Errorf("failed to initialize MPC: %w", err)
	}

	// Initialize ZK components
	if err := vm.initializeZK(); err != nil {
		return fmt.Errorf("failed to initialize ZK: %w", err)
	}

	// Initialize Teleport Protocol
	if err := vm.initializeTeleport(); err != nil {
		return fmt.Errorf("failed to initialize Teleport: %w", err)
	}

	// Initialize Warp messaging
	if err := vm.initializeWarp(); err != nil {
		return fmt.Errorf("failed to initialize Warp: %w", err)
	}

	// Initialize network components
	if err := vm.initializeNetwork(appSender); err != nil {
		return fmt.Errorf("failed to initialize network: %w", err)
	}

	// Initialize API server
	if err := vm.initializeAPI(); err != nil {
		return fmt.Errorf("failed to initialize API: %w", err)
	}

	// Start background services
	vm.shutdownChan = make(chan struct{})
	vm.startBackgroundServices()

	log.Info("MPC-Chain VM initialized",
		"chainID", mpcChainID,
		"validators", vm.validators.Count(),
		"mpcEnabled", vm.config.MPCEnabled,
		"teleportEnabled", vm.config.TeleportEnabled,
	)

	return nil
}

// initializeDBs creates all database instances
func (vm *VM) initializeDBs() error {
	// Create versioned database
	vm.db = versiondb.New(vm.baseDB)

	// Create prefixed databases
	vm.stateDB = prefixdb.New([]byte{stateDBPrefix}, vm.db)
	vm.blockDB = prefixdb.New([]byte{blockDBPrefix}, vm.db)
	vm.metadataDB = prefixdb.New([]byte{metadataDBPrefix}, vm.db)
	vm.warpDB = prefixdb.New([]byte{warpDBPrefix}, vm.db)
	vm.mpcDB = prefixdb.New([]byte{mpcDBPrefix}, vm.db)
	vm.zkDB = prefixdb.New([]byte{zkDBPrefix}, vm.db)
	vm.teleportDB = prefixdb.New([]byte{teleportDBPrefix}, vm.db)
	vm.validatorsDB = prefixdb.New([]byte{validatorsDBPrefix}, vm.db)

	return nil
}

// initializeBlockchain sets up the blockchain components
func (vm *VM) initializeBlockchain(genesisBytes []byte) error {
	// Parse genesis
	genesis, err := ParseGenesis(genesisBytes)
	if err != nil {
		return fmt.Errorf("failed to parse genesis: %w", err)
	}

	// Initialize blockchain
	vm.blockChain = NewBlockChain(vm.blockDB, vm.stateDB, genesis, vm.config)
	vm.genesisHash = genesis.Hash()

	// Initialize mempool
	vm.mempool = NewMempool(vm.config.MempoolSize)

	return nil
}

// initializeMPC sets up MPC components for the M-Chain
func (vm *VM) initializeMPC() error {
	if !vm.config.MPCEnabled {
		return nil
	}

	// Initialize MPC manager
	vm.mpcManager = NewMPCManager(vm.mpcDB, vm.config.MPCConfig)

	// Initialize MPC wallet
	vm.mpcWallet = NewMPCWallet(vm.mpcManager)

	// Initialize protocols
	vm.keyGenProtocol = NewKeyGenProtocol(vm.mpcManager)
	vm.signProtocol = NewSignProtocol(vm.mpcManager)
	vm.reshareProtocol = NewReshareProtocol(vm.mpcManager)

	return nil
}

// initializeZK sets up ZK proof components
func (vm *VM) initializeZK() error {
	if !vm.config.ZKEnabled {
		return nil
	}

	vm.zkVerifier = NewZKVerifier(vm.zkDB)
	vm.zkProver = NewZKProver(vm.zkDB)

	return nil
}

// initializeTeleport sets up the Teleport Protocol
func (vm *VM) initializeTeleport() error {
	if !vm.config.TeleportEnabled {
		return nil
	}

	// Initialize asset registry
	vm.assetRegistry = NewAssetRegistry(vm.teleportDB)

	// Initialize intent pool
	vm.intentPool = NewIntentPool(vm.config.IntentPoolSize)

	// Initialize executor engine
	vm.executorEngine = NewExecutorEngine(
		vm.mpcWallet,
		vm.assetRegistry,
		vm.zkProver,
		vm.config.ExecutorConfig,
	)

	// Initialize teleport engine
	vm.teleportEngine = NewTeleportEngine(
		vm.intentPool,
		vm.executorEngine,
		vm.assetRegistry,
		vm.zkVerifier,
		nil, // XChainSettlement - placeholder
		TeleportConfig{}, // Default config - placeholder
	)

	return nil
}

// initializeWarp sets up Warp messaging
func (vm *VM) initializeWarp() error {
	// TODO: Implement warp backend initialization when warp.Backend type is fixed
	/*
	var err error
	vm.warpBackend, err = warp.NewBackend(
		vm.ctx.NetworkID,
		vm.ctx.ChainID,
		vm.warpDB,
		vm.GetBlockSignature,
		vm.GetMessageSignature,
		vm.config.WarpConfig,
	)
	return err
	*/
	return nil
}

// initializeNetwork sets up network components
func (vm *VM) initializeNetwork(appSender core.AppSender) error {
	// Initialize validator set
	vm.validators = NewValidatorSet(vm.validatorsDB, vm.ctx.ValidatorState)

	// Initialize network handler
	vm.networkHandler = NewNetworkHandler(vm, appSender)

	// Initialize P2P client
	// TODO: Fix p2p.NewClient initialization
	/*
	var err error
	vm.p2pClient, err = p2p.NewClient(
		vm.ctx.NetworkID,
		vm.ctx.NodeID,
		vm.networkHandler,
		vm.config.P2PConfig,
	)
	return err
	*/
	return nil
}

// initializeAPI sets up the API server
func (vm *VM) initializeAPI() error {
	vm.apiServer = NewAPIServer(vm)
	return nil
}

// startBackgroundServices starts all background services
func (vm *VM) startBackgroundServices() {
	// Start block building
	vm.shutdownWg.Add(1)
	go vm.blockBuildingLoop()

	// Start MPC services
	if vm.config.MPCEnabled {
		vm.shutdownWg.Add(1)
		go vm.mpcManager.Run(vm.shutdownChan, &vm.shutdownWg)
	}

	// Start Teleport services
	if vm.config.TeleportEnabled {
		vm.shutdownWg.Add(1)
		go vm.teleportEngine.Run(vm.shutdownChan, &vm.shutdownWg)
	}

	// Start network services
	vm.shutdownWg.Add(1)
	go vm.networkHandler.Run(vm.shutdownChan, &vm.shutdownWg)
}

// blockBuildingLoop continuously builds blocks
func (vm *VM) blockBuildingLoop() {
	defer vm.shutdownWg.Done()

	ticker := time.NewTicker(vm.config.BlockBuildInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if vm.shouldBuildBlock() {
				if err := vm.buildBlock(); err != nil {
					log.Error("failed to build block", "error", err)
				}
			}
		case <-vm.shutdownChan:
			return
		}
	}
}

// shouldBuildBlock determines if a new block should be built
func (vm *VM) shouldBuildBlock() bool {
	// Check if mempool has transactions
	if vm.mempool.Len() > 0 {
		return true
	}

	// Check if intent pool has intents
	if vm.config.TeleportEnabled && vm.intentPool.Len() > 0 {
		return true
	}

	// Check if MPC operations are pending
	if vm.config.MPCEnabled && vm.mpcManager.HasPendingOperations() {
		return true
	}

	return false
}

// buildBlock creates a new block
func (vm *VM) buildBlock() error {
	// This is a simplified version - actual implementation would be more complex
	log.Debug("building new block")
	return nil
}

// Shutdown implements the snowman.ChainVM interface
func (vm *VM) Shutdown(context.Context) error {
	if vm.shutdownChan != nil {
		close(vm.shutdownChan)
		vm.shutdownWg.Wait()
	}

	// Close all services
	if vm.apiServer != nil {
		vm.apiServer.Shutdown()
	}

	// TODO: Fix p2pClient close when implemented
	/*
	if vm.p2pClient != nil {
		vm.p2pClient.Close()
	}
	*/

	// TODO: Fix stateSyncClient close when implemented
	/*
	if vm.stateSyncClient != nil {
		vm.stateSyncClient.Close()
	}
	*/

	// Close databases
	return vm.db.Close()
}

// BuildBlock implements the block.ChainVM interface
func (vm *VM) BuildBlock(context.Context) (block.Block, error) {
	return vm.blockChain.BuildBlock()
}

// ParseBlock implements the block.ChainVM interface
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (block.Block, error) {
	return vm.blockChain.ParseBlock(blockBytes)
}

// GetBlock implements the block.ChainVM interface
func (vm *VM) GetBlock(ctx context.Context, blockID ids.ID) (block.Block, error) {
	return vm.blockChain.GetBlock(blockID)
}

// SetPreference implements the block.ChainVM interface
func (vm *VM) SetPreference(ctx context.Context, blockID ids.ID) error {
	return vm.blockChain.SetPreference(blockID)
}

// LastAccepted implements the block.ChainVM interface
func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.blockChain.LastAccepted()
}

// SetState implements the block.ChainVM interface
func (vm *VM) SetState(ctx context.Context, state quasar.State) error {
	// Handle state transitions
	return nil
}

// WaitForEvent implements the block.ChainVM interface
func (vm *VM) WaitForEvent(ctx context.Context) (core.Message, error) {
	// Wait for the next event
	select {
	case <-ctx.Done():
		return core.Message{}, ctx.Err()
	case <-vm.shutdownChan:
		return core.Message{}, nil
	}
}

// HealthCheck implements the common.VM interface
func (vm *VM) HealthCheck(context.Context) (interface{}, error) {
	health := &Health{
		Healthy:        true,
		BlockchainSync: vm.blockChain.IsSynced(),
		MPCStatus:      "healthy",
		TeleportStatus: "healthy",
	}

	if vm.config.MPCEnabled && vm.mpcManager != nil {
		health.MPCStatus = vm.mpcManager.HealthStatus()
	}

	if vm.config.TeleportEnabled && vm.teleportEngine != nil {
		health.TeleportStatus = vm.teleportEngine.HealthStatus()
	}

	return health, nil
}

// Version implements the common.VM interface
func (vm *VM) Version(context.Context) (string, error) {
	return Version, nil
}

// CreateHandlers implements the common.VM interface
func (vm *VM) CreateHandlers(context.Context) (map[string]interface{}, error) {
	handlers, err := vm.apiServer.CreateHandlers()
	if err != nil {
		return nil, err
	}
	
	// Convert map[string]http.Handler to map[string]interface{}
	result := make(map[string]interface{})
	for k, v := range handlers {
		result[k] = v
	}
	return result, nil
}

// CreateStaticHandlers implements the common.VM interface
func (vm *VM) CreateStaticHandlers(context.Context) (map[string]interface{}, error) {
	handlers, err := vm.apiServer.CreateStaticHandlers()
	if err != nil {
		return nil, err
	}
	
	// Convert map[string]http.Handler to map[string]interface{}
	result := make(map[string]interface{})
	for k, v := range handlers {
		result[k] = v
	}
	return result, nil
}

// GetBlockSignature returns the BLS signature for a block
func (vm *VM) GetBlockSignature(blockID ids.ID) ([bls.SignatureLen]byte, error) {
	// Implementation would retrieve the aggregated BLS signature for the block
	return [bls.SignatureLen]byte{}, nil
}

// GetMessageSignature returns the BLS signature for a Warp message
func (vm *VM) GetMessageSignature(messageID ids.ID) ([bls.SignatureLen]byte, error) {
	// Implementation would retrieve the aggregated BLS signature for the message
	return [bls.SignatureLen]byte{}, nil
}

// StateSyncEnabled implements block.StateSyncableVM
func (vm *VM) StateSyncEnabled(context.Context) (bool, error) {
	return false, nil
}

// GetOngoingSyncStateSummary implements block.StateSyncableVM
func (vm *VM) GetOngoingSyncStateSummary(context.Context) (block.StateSummary, error) {
	return nil, nil
}

// GetLastStateSummary implements block.StateSyncableVM
func (vm *VM) GetLastStateSummary(context.Context) (block.StateSummary, error) {
	return nil, nil
}

// ParseStateSummary implements block.StateSyncableVM
func (vm *VM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return nil, nil
}

// GetStateSummary implements block.StateSyncableVM
func (vm *VM) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	return nil, nil
}