// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/rpc/v2"
	"go.uber.org/zap"

	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/consensus/uptime"
	"github.com/luxfi/node/consensus/validators"
	"github.com/luxfi/node/database"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/utils/logging"
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
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/txs/mempool"

	linearblock "github.com/luxfi/node/consensus/engine/linear/block"
	blockbuilder "github.com/luxfi/node/vms/platformvm/block/builder"
	blockexecutor "github.com/luxfi/node/vms/platformvm/block/executor"
	platformvmmetrics "github.com/luxfi/node/vms/platformvm/metrics"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	pmempool "github.com/luxfi/node/vms/platformvm/txs/mempool"
	pvalidators "github.com/luxfi/node/vms/platformvm/validators"
)

var (
	_ linearblock.ChainVM        = (*VM)(nil)
	_ secp256k1fx.VM             = (*VM)(nil)
	_ validators.State           = (*VM)(nil)
	_ validators.SubnetConnector = (*VM)(nil)
)

type VM struct {
	config.Config
	blockbuilder.Builder
	*network.Network
	validators.State

	metrics platformvmmetrics.Metrics

	// Used to get time. Useful for faking time during tests.
	clock mockable.Clock

	uptimeManager uptime.Manager

	// The context of this vm
	ctx *consensus.Context
	db  database.Database

	state state.State

	fx            fx.Fx
	codecRegistry codec.Registry

	// Bootstrapped remembers if this chain has finished bootstrapping or not
	bootstrapped utils.Atomic[bool]

	manager blockexecutor.Manager

	// Cancelled on shutdown
	onShutdownCtx context.Context
	// Call [onShutdownCtxCancel] to cancel [onShutdownCtx] during Shutdown()
	onShutdownCtxCancel context.CancelFunc
}

// Initialize this blockchain.
// [vm.ChainManager] and [vm.vdrMgr] must be set before this function is called.
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *consensus.Context,
	db database.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	chainCtx.Log.Verbo("initializing platform chain")

	execConfig, err := config.GetExecutionConfig(configBytes)
	if err != nil {
		return err
	}
	chainCtx.Log.Info("using VM execution config", zap.Reflect("config", execConfig))

	registerer, err := metrics.MakeAndRegister(chainCtx.Metrics, "")
	if err != nil {
		return err
	}

	// Initialize metrics as soon as possible
	vm.metrics, err = platformvmmetrics.New(registerer)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	vm.ctx = chainCtx
	vm.db = db

	// Note: this codec is never used to serialize anything
	vm.codecRegistry = linearcodec.NewDefault()
	vm.fx = &secp256k1fx.Fx{}
	if err := vm.fx.Initialize(vm); err != nil {
		return err
	}

	rewards := reward.NewCalculator(vm.RewardConfig)

	vm.state, err = state.New(
		vm.db,
		genesisBytes,
		registerer,
		&vm.Config,
		execConfig,
		vm.ctx,
		vm.metrics,
		rewards,
	)
	if err != nil {
		return err
	}

	validatorManager := pvalidators.NewManager(chainCtx.Log, vm.Config, vm.state, vm.metrics, &vm.clock)
	vm.State = validatorManager
	utxoHandler := utxo.NewHandler(vm.ctx, &vm.clock, vm.fx)
	vm.uptimeManager = uptime.NewManager(vm.state, &vm.clock)
	vm.UptimeLockedCalculator.SetCalculator(&vm.bootstrapped, &chainCtx.Lock, vm.uptimeManager)

	txExecutorBackend := &txexecutor.Backend{
		Config:       &vm.Config,
		Ctx:          vm.ctx,
		Clk:          &vm.clock,
		Fx:           vm.fx,
		FlowChecker:  utxoHandler,
		Uptimes:      vm.uptimeManager,
		Rewards:      rewards,
		Bootstrapped: &vm.bootstrapped,
	}

	mempool, err := pmempool.New("mempool", registerer, toEngine)
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

	txVerifier := network.NewLockedTxVerifier(&txExecutorBackend.Ctx.Lock, vm.manager)
	vm.Network, err = network.New(
		chainCtx.Log,
		chainCtx.NodeID,
		chainCtx.SubnetID,
		validators.NewLockedState(
			&chainCtx.Lock,
			validatorManager,
		),
		txVerifier,
		mempool,
		txExecutorBackend.Config.PartialSyncPrimaryNetwork,
		appSender,
		registerer,
		execConfig.Network,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize network: %w", err)
	}

	vm.onShutdownCtx, vm.onShutdownCtxCancel = context.WithCancel(context.Background())
	// TODO: Wait for this goroutine to exit during Shutdown once the platformvm
	// has better control of the context lock.
	go vm.Network.PushGossip(vm.onShutdownCtx)
	go vm.Network.PullGossip(vm.onShutdownCtx)

	vm.Builder = blockbuilder.New(
		mempool,
		txExecutorBackend,
		vm.manager,
	)

	// Create all of the chains that the database says exist
	chainCtx.Log.Info("about to call initBlockchains")
	if err := vm.initBlockchains(); err != nil {
		return fmt.Errorf(
			"failed to initialize blockchains: %w",
			err,
		)
	}

	lastAcceptedID := vm.state.GetLastAccepted()
	chainCtx.Log.Info("initializing last accepted",
		zap.Stringer("blkID", lastAcceptedID),
	)
	if err := vm.SetPreference(ctx, lastAcceptedID); err != nil {
		return err
	}

	// Incrementing [awaitShutdown] would cause a deadlock since
	// [periodicallyPruneMempool] grabs the context lock.
	go vm.periodicallyPruneMempool(execConfig.MempoolPruneFrequency)

	go func() {
		err := vm.state.ReindexBlocks(&vm.ctx.Lock, vm.ctx.Log)
		if err != nil {
			vm.ctx.Log.Warn("reindexing blocks failed",
				zap.Error(err),
			)
		}
	}()

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
				vm.ctx.Log.Debug("pruning mempool failed",
					zap.Error(err),
				)
			}
		}
	}
}

func (vm *VM) pruneMempool() error {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Packing all of the transactions in order performs additional checks that
	// the MempoolTxVerifier doesn't include. So, evicting transactions from
	// here is expected to happen occasionally.
	blockTxs, err := vm.Builder.PackBlockTxs(math.MaxInt)
	if err != nil {
		return err
	}

	for _, tx := range blockTxs {
		if err := vm.Builder.Add(tx); err != nil {
			vm.ctx.Log.Debug(
				"failed to reissue tx",
				zap.Stringer("txID", tx.ID()),
				zap.Error(err),
			)
		}
	}

	return nil
}

// checkExistingChains looks for existing blockchain data and registers them
func (vm *VM) checkExistingChains() error {
	// Scan chainData directory for existing chains
	// We need the parent chainData directory, not the P-Chain specific one
	chainDataDir := filepath.Dir(vm.ctx.ChainDataDir)
	vm.ctx.Log.Info("checking for existing chains in chainData directory",
		zap.String("chainDataDir", chainDataDir),
	)

	entries, err := os.ReadDir(chainDataDir)
	if err != nil {
		vm.ctx.Log.Info("chainData directory read error",
			zap.Error(err),
		)
		// Directory might not exist yet, that's ok
		return nil
	}

	vm.ctx.Log.Info("found chainData entries",
		zap.Int("count", len(entries)),
	)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		vm.ctx.Log.Info("checking chainData entry",
			zap.String("name", entry.Name()),
		)

		// Try to parse as chain ID
		chainID, err := ids.FromString(entry.Name())
		if err != nil {
			vm.ctx.Log.Debug("failed to parse chain ID",
				zap.String("name", entry.Name()),
				zap.Error(err),
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
		var subnetID ids.ID = constants.PrimaryNetworkID // Default to primary network

		// Check for EVM chain (C-Chain)
		if bytes.Contains(configData, []byte("chain-id")) || bytes.Contains(configData, []byte("chainId")) {
			vmID = constants.EVMID
			vm.ctx.Log.Info("detected EVM chain from config",
				zap.String("chainID", chainID.String()),
			)
		} else {
			// Check for other VM types by looking at other files
			// For now, we'll skip non-EVM chains
			vm.ctx.Log.Debug("skipping non-EVM chain",
				zap.String("chainID", chainID.String()),
			)
			continue
		}

		// Check if we need to determine subnet ID from somewhere
		// For now, assume primary network for orphaned chains

		// Check if this chain is already known
		chains, err := vm.state.GetChains(subnetID)
		if err != nil {
			vm.ctx.Log.Warn("failed to get chains for subnet",
				zap.String("subnetID", subnetID.String()),
				zap.Error(err),
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
			vm.ctx.Log.Info("found orphaned chain, queuing for creation",
				zap.String("chainID", chainID.String()),
				zap.String("vmID", vmID.String()),
				zap.String("subnetID", subnetID.String()),
				zap.String("path", filepath.Join(chainDataDir, entry.Name())),
			)

			// For existing chains, we need to provide a minimal but valid genesis
			// The EVM will match this against the existing chain data
			// Extract chainId from config if possible
			var chainIDNum uint64 = 96369 // default
			if bytes.Contains(configData, []byte(`"chainId": 96369`)) || bytes.Contains(configData, []byte(`"chainId":96369`)) {
				chainIDNum = 96369
			}

			minimalGenesis := fmt.Sprintf(`{
				"config": {
					"chainId": %d,
					"homesteadBlock": 0,
					"eip150Block": 0,
					"eip155Block": 0,
					"eip158Block": 0,
					"byzantiumBlock": 0,
					"constantinopleBlock": 0,
					"petersburgBlock": 0,
					"istanbulBlock": 0,
					"muirGlacierBlock": 0,
					"subnetEVMTimestamp": 0,
					"feeConfig": {
						"gasLimit": 8000000,
						"targetBlockRate": 2,
						"minBaseFee": 25000000000,
						"targetGas": 15000000,
						"baseFeeChangeDenominator": 36,
						"minBlockGasCost": 0,
						"maxBlockGasCost": 1000000,
						"blockGasCostStep": 200000
					}
				},
				"gasLimit": "0x7a1200",
				"difficulty": "0x0",
				"alloc": {}
			}`, chainIDNum)

			vm.Config.QueueExistingChainWithGenesis(chainID, subnetID, vmID, []byte(minimalGenesis))
		} else {
			vm.ctx.Log.Debug("chain already registered",
				zap.String("chainID", chainID.String()),
			)
		}
	}
	return nil
}

// Create all chains that exist that this node validates.
func (vm *VM) initBlockchains() error {
	vm.ctx.Log.Info("initBlockchains called")

	// Check for existing chains in chainData directory
	if err := vm.checkExistingChains(); err != nil {
		vm.ctx.Log.Warn("failed to check existing chains", zap.Error(err))
	}

	if vm.Config.PartialSyncPrimaryNetwork {
		vm.ctx.Log.Info("skipping primary network chain creation")
	} else if err := vm.createSubnet(constants.PrimaryNetworkID); err != nil {
		return err
	}

	if vm.SybilProtectionEnabled {
		for subnetID := range vm.TrackedSubnets {
			if err := vm.createSubnet(subnetID); err != nil {
				return err
			}
		}
	} else {
		subnetIDs, err := vm.state.GetSubnetIDs()
		if err != nil {
			return err
		}
		for _, subnetID := range subnetIDs {
			if err := vm.createSubnet(subnetID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Create the subnet with ID [subnetID]
func (vm *VM) createSubnet(subnetID ids.ID) error {
	chains, err := vm.state.GetChains(subnetID)
	if err != nil {
		return err
	}
	for _, chain := range chains {
		tx, ok := chain.Unsigned.(*txs.CreateChainTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chain.Unsigned)
		}

		chainID := chain.ID()

		// Check for chain ID mapping override
		// Support mapping for C-Chain to use existing blockchain ID
		vm.ctx.Log.Info("Checking chain ID mapping",
			zap.String("vmID", tx.VMID.String()),
			zap.String("EVMID", constants.EVMID.String()),
			zap.String("originalChainID", chainID.String()),
			zap.String("envVar", os.Getenv("LUX_CHAIN_ID_MAPPING_C")),
		)

		if tx.VMID == constants.EVMID && os.Getenv("LUX_CHAIN_ID_MAPPING_C") != "" {
			mappedID := os.Getenv("LUX_CHAIN_ID_MAPPING_C")
			parsedID, err := ids.FromString(mappedID)
			if err == nil {
				vm.ctx.Log.Info("Using mapped blockchain ID for C-Chain",
					zap.String("original", chainID.String()),
					zap.String("mapped", parsedID.String()),
				)
				chainID = parsedID
			} else {
				vm.ctx.Log.Warn("Invalid chain ID mapping",
					zap.String("mapping", mappedID),
					zap.Error(err),
				)
			}
		}

		vm.Config.CreateChain(chainID, tx)
	}
	return nil
}

// onBootstrapStarted marks this VM as bootstrapping
func (vm *VM) onBootstrapStarted() error {
	vm.bootstrapped.Set(false)
	return vm.fx.Bootstrapping()
}

// onNormalOperationsStarted marks this VM as bootstrapped
func (vm *VM) onNormalOperationsStarted() error {
	if vm.bootstrapped.Get() {
		return nil
	}
	vm.bootstrapped.Set(true)

	if err := vm.fx.Bootstrapped(); err != nil {
		return err
	}

	primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
	if err := vm.uptimeManager.StartTracking(primaryVdrIDs, constants.PrimaryNetworkID); err != nil {
		return err
	}

	vl := validators.NewLogger(vm.ctx.Log, constants.PrimaryNetworkID, vm.ctx.NodeID)
	vm.Validators.RegisterSetCallbackListener(constants.PrimaryNetworkID, vl)

	for subnetID := range vm.TrackedSubnets {
		vdrIDs := vm.Validators.GetValidatorIDs(subnetID)
		if err := vm.uptimeManager.StartTracking(vdrIDs, subnetID); err != nil {
			return err
		}

		vl := validators.NewLogger(vm.ctx.Log, subnetID, vm.ctx.NodeID)
		vm.Validators.RegisterSetCallbackListener(subnetID, vl)
	}

	if err := vm.state.Commit(); err != nil {
		return err
	}

	// Start the block builder
	vm.Builder.StartBlockTimer()
	return nil
}

func (vm *VM) SetState(_ context.Context, state consensus.State) error {
	switch state {
	case consensus.Bootstrapping:
		return vm.onBootstrapStarted()
	case consensus.NormalOp:
		return vm.onNormalOperationsStarted()
	default:
		return consensus.ErrUnknownState
	}
}

// Shutdown this blockchain
func (vm *VM) Shutdown(context.Context) error {
	if vm.db == nil {
		return nil
	}

	vm.onShutdownCtxCancel()
	vm.Builder.ShutdownBlockTimer()

	if vm.bootstrapped.Get() {
		primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
		if err := vm.uptimeManager.StopTracking(primaryVdrIDs, constants.PrimaryNetworkID); err != nil {
			return err
		}

		for subnetID := range vm.TrackedSubnets {
			vdrIDs := vm.Validators.GetValidatorIDs(subnetID)
			if err := vm.uptimeManager.StopTracking(vdrIDs, subnetID); err != nil {
				return err
			}
		}

		if err := vm.state.Commit(); err != nil {
			return err
		}
	}

	return errors.Join(
		vm.state.Close(),
		vm.db.Close(),
	)
}

func (vm *VM) ParseBlock(_ context.Context, b []byte) (linear.Block, error) {
	// Note: blocks to be parsed are not verified, so we must used blocks.Codec
	// rather than blocks.GenesisCodec
	statelessBlk, err := block.Parse(block.Codec, b)
	if err != nil {
		return nil, err
	}
	return vm.manager.NewBlock(statelessBlk), nil
}

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (linear.Block, error) {
	return vm.manager.GetBlock(blkID)
}

// LastAccepted returns the block most recently accepted
func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.manager.LastAccepted(), nil
}

// SetPreference sets the preferred block to be the one with ID [blkID]
func (vm *VM) SetPreference(_ context.Context, blkID ids.ID) error {
	if vm.manager.SetPreference(blkID) {
		vm.Builder.ResetBlockTimer()
	}
	return nil
}

func (*VM) Version(context.Context) (string, error) {
	return version.Current.String(), nil
}

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	server.RegisterInterceptFunc(vm.metrics.InterceptRequest)
	server.RegisterAfterFunc(vm.metrics.AfterRequest)
	service := &Service{
		vm:          vm,
		addrManager: lux.NewAddressManager(vm.ctx),
		stakerAttributesCache: &cache.LRU[ids.ID, *stakerAttributes]{
			Size: stakerAttributesCacheSize,
		},
	}
	err := server.RegisterService(service, "platform")
	return map[string]http.Handler{
		"": server,
	}, err
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	if err := vm.uptimeManager.Connect(nodeID, constants.PrimaryNetworkID); err != nil {
		return err
	}
	return vm.Network.Connected(ctx, nodeID, version)
}

func (vm *VM) ConnectedSubnet(_ context.Context, nodeID ids.NodeID, subnetID ids.ID) error {
	return vm.uptimeManager.Connect(nodeID, subnetID)
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := vm.uptimeManager.Disconnect(nodeID); err != nil {
		return err
	}
	if err := vm.state.Commit(); err != nil {
		return err
	}
	return vm.Network.Disconnected(ctx, nodeID)
}

func (vm *VM) CodecRegistry() codec.Registry {
	return vm.codecRegistry
}

func (vm *VM) Clock() *mockable.Clock {
	return &vm.clock
}

func (vm *VM) Logger() logging.Logger {
	return vm.ctx.Log
}

func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	return vm.state.GetBlockIDAtHeight(height)
}

func (vm *VM) issueTxFromRPC(tx *txs.Tx) error {
	err := vm.Network.IssueTxFromRPC(tx)
	if err != nil && !errors.Is(err, mempool.ErrDuplicateTx) {
		vm.ctx.Log.Debug("failed to add tx to mempool",
			zap.Stringer("txID", tx.ID()),
			zap.Error(err),
		)
		return err
	}

	return nil
}
