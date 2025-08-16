// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/luxfi/node/cache"
	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/core/interfaces"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/engine/dag/vertex"
	"github.com/luxfi/consensus/engine/dag"
	"github.com/luxfi/consensus/protocol/chain"
	"github.com/luxfi/database"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/pubsub"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/utils/linked"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/components/index"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/txs/mempool"
	"github.com/luxfi/node/vms/xvm/block"
	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/network"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/vms/xvm/utxo"

	blockbuilder "github.com/luxfi/node/vms/xvm/block/builder"
	blockexecutor "github.com/luxfi/node/vms/xvm/block/executor"
	extensions "github.com/luxfi/node/vms/xvm/fxs"
	xvmmetrics "github.com/luxfi/node/vms/xvm/metrics"
	txexecutor "github.com/luxfi/node/vms/xvm/txs/executor"
	xmempool "github.com/luxfi/node/vms/xvm/txs/mempool"
)

const assetToFxCacheSize = 1024

var (
	errIncompatibleFx            = errors.New("incompatible feature extension")
	errUnknownFx                 = errors.New("unknown feature extension")
	errGenesisAssetMustHaveState = errors.New("genesis asset must have non-empty state")
	errUnknownState              = errors.New("unknown state")

	_ vertex.LinearizableVMWithEngine = (*VM)(nil)
)

type VM struct {
	network.Atomic

	config.Config

	metrics xvmmetrics.Metrics

	lux.AddressManager
	ids.Aliaser
	utxo.Spender

	// Contains information of where this VM is executing
	ctx context.Context
	
	// Logger for this VM
	log log.Logger
	
	// Lock for thread safety
	lock sync.RWMutex
	
	// BCLookup provides blockchain alias lookup
	bcLookup interfaces.BCLookup
	
	// SharedMemory for cross-chain operations
	sharedMemory interfaces.SharedMemory

	// Used to check local time
	clock mockable.Clock

	registerer prometheus.Registerer

	connectedPeers map[ids.NodeID]*version.Application

	parser block.Parser

	pubsub *pubsub.Server

	appSender core.AppSender

	// State management
	state state.State

	// Set to true once this VM is marked as `Bootstrapped` by the engine
	bootstrapped bool

	// asset id that will be used for fees
	feeAssetID ids.ID

	// Asset ID --> Bit set with fx IDs the asset supports
	assetToFxCache *cache.LRU[ids.ID, set.Bits64]

	baseDB database.Database
	db     *versiondb.Database

	typeToFxIndex map[reflect.Type]int
	fxs           []*extensions.ParsedFx

	walletService WalletService

	addressTxsIndexer index.AddressTxsIndexer

	txBackend *txexecutor.Backend

	// Cancelled on shutdown
	onShutdownCtx context.Context
	// Call [onShutdownCtxCancel] to cancel [onShutdownCtx] during Shutdown()
	onShutdownCtxCancel context.CancelFunc
	awaitShutdown       sync.WaitGroup

	networkConfig network.Config
	// These values are only initialized after the chain has been linearized.
	blockbuilder.Builder
	chainManager blockexecutor.Manager
	network      *network.Network
	
	// Channel for receiving messages from mempool
	toEngine chan core.Message
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	// If the chain isn't linearized yet, we must track the peers externally
	// until the network is initialized.
	if vm.network == nil {
		vm.connectedPeers[nodeID] = version
		return nil
	}
	return vm.network.Connected(ctx, nodeID, version)
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	// If the chain isn't linearized yet, we must track the peers externally
	// until the network is initialized.
	if vm.network == nil {
		delete(vm.connectedPeers, nodeID)
		return nil
	}
	return vm.network.Disconnected(ctx, nodeID)
}

/*
 ******************************************************************************
 ********************************* Core VM **********************************
 ******************************************************************************
 */

// Initialize with new signature for LinearizableVMWithEngine compatibility
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx interface{},
	dbManager interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- interface{},
	fxs []interface{},
	appSender interface{},
) error {
	// Convert types to what we expect
	consensusCtx, ok := chainCtx.(context.Context)
	if !ok {
		return errors.New("invalid chain context type")
	}
	
	db, ok := dbManager.(database.Database)
	if !ok {
		return errors.New("invalid database type")
	}
	
	coreFxs := make([]*core.Fx, len(fxs))
	for i, fx := range fxs {
		if fx != nil {
			coreFxs[i] = fx.(*core.Fx)
		}
	}
	
	coreAppSender, ok := appSender.(core.AppSender)
	if !ok {
		return errors.New("invalid app sender type")
	}
	
	// Ignore toEngine channel as XVM doesn't use it
	_ = toEngine
	
	return vm.initialize(ctx, consensusCtx, db, genesisBytes, upgradeBytes, configBytes, coreFxs, coreAppSender)
}

// Original Initialize method renamed to initialize
func (vm *VM) initialize(
	_ context.Context,
	ctx context.Context,
	db database.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	fxs []*core.Fx,
	appSender core.AppSender,
) error {
	// Create a simple no-op handler since core.NewNoOpAppHandler doesn't exist in consensus
	noopMessageHandler := &noOpAppHandler{}
	vm.Atomic = network.NewAtomic(noopMessageHandler)

	xvmConfig, err := ParseConfig(configBytes)
	if err != nil {
		return err
	}
	vm.log.Info("VM config initialized",
		zap.Reflect("config", xvmConfig),
	)

	// Get metrics from a global registry or create new one
	vm.registerer = prometheus.NewRegistry()

	vm.connectedPeers = make(map[ids.NodeID]*version.Application)

	// Initialize metrics as soon as possible
	vm.metrics, err = xvmmetrics.New(vm.registerer)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	vm.AddressManager = lux.NewAddressManager(ctx)
	vm.Aliaser = ids.NewAliaser()

	vm.ctx = ctx
	vm.appSender = appSender
	vm.baseDB = db
	vm.db = versiondb.New(db)
	vm.assetToFxCache = &cache.LRU[ids.ID, set.Bits64]{Size: assetToFxCacheSize}

	vm.pubsub = pubsub.New(vm.log)

	typedFxs := make([]extensions.Fx, len(fxs))
	vm.fxs = make([]*extensions.ParsedFx, len(fxs))
	for i, fxContainer := range fxs {
		if fxContainer == nil {
			return errIncompatibleFx
		}
		// Since core.Fx is now empty, we need to handle this differently
		// For now, use a placeholder secp256k1fx
		fx := &secp256k1fx.Fx{}
		typedFxs[i] = fx
		vm.fxs[i] = &extensions.ParsedFx{
			ID: ids.Empty, // Use empty ID as placeholder
			Fx: fx,
		}
	}

	vm.typeToFxIndex = map[reflect.Type]int{}
	vm.parser, err = block.NewCustomParser(
		vm.typeToFxIndex,
		&vm.clock,
		vm.log,
		typedFxs,
	)
	if err != nil {
		return err
	}

	codec := vm.parser.Codec()
	vm.Spender = utxo.NewSpender(&vm.clock, codec)

	state, err := state.New(
		vm.db,
		vm.parser,
		vm.registerer,
		xvmConfig.ChecksumsEnabled,
	)
	if err != nil {
		return err
	}

	vm.state = state

	if err := vm.initGenesis(genesisBytes); err != nil {
		return err
	}

	vm.walletService.vm = vm
	vm.walletService.pendingTxs = linked.NewHashmap[ids.ID, *txs.Tx]()

	// use no op impl when disabled in config
	if xvmConfig.IndexTransactions {
		vm.log.Warn("deprecated address transaction indexing is enabled")
		vm.addressTxsIndexer, err = index.NewIndexer(vm.db, vm.log, "", vm.registerer, xvmConfig.IndexAllowIncomplete)
		if err != nil {
			return fmt.Errorf("failed to initialize address transaction indexer: %w", err)
		}
	} else {
		vm.log.Info("address transaction indexing is disabled")
		vm.addressTxsIndexer, err = index.NewNoIndexer(vm.db, xvmConfig.IndexAllowIncomplete)
		if err != nil {
			return fmt.Errorf("failed to initialize disabled indexer: %w", err)
		}
	}

	vm.txBackend = &txexecutor.Backend{
		Ctx:           ctx,
		Config:        &vm.Config,
		Fxs:           vm.fxs,
		TypeToFxIndex: vm.typeToFxIndex,
		Codec:         vm.parser.Codec(),
		FeeAssetID:    vm.feeAssetID,
		Bootstrapped:  false,
	}

	vm.onShutdownCtx, vm.onShutdownCtxCancel = context.WithCancel(context.Background())
	vm.networkConfig = xvmConfig.Network
	return vm.state.Commit()
}

// onBootstrapStarted is called by the consensus engine when it starts bootstrapping this chain
func (vm *VM) onBootstrapStarted() error {
	vm.txBackend.Bootstrapped = false
	for _, fx := range vm.fxs {
		if err := fx.Fx.Bootstrapping(); err != nil {
			return err
		}
	}
	return nil
}

func (vm *VM) onNormalOperationsStarted() error {
	vm.txBackend.Bootstrapped = true
	for _, fx := range vm.fxs {
		if err := fx.Fx.Bootstrapped(); err != nil {
			return err
		}
	}

	vm.bootstrapped = true
	return nil
}

func (vm *VM) SetState(_ context.Context, state consensus.State) error {
	switch state {
	case consensus.Bootstrapping:
		return vm.onBootstrapStarted()
	case consensus.NormalOp:
		return vm.onNormalOperationsStarted()
	default:
		return errUnknownState
	}
}

func (vm *VM) Shutdown() error {
	if vm.state == nil {
		return nil
	}

	vm.onShutdownCtxCancel()
	vm.awaitShutdown.Wait()

	return errors.Join(
		vm.state.Close(),
		vm.baseDB.Close(),
	)
}

func (*VM) Version(context.Context) (string, error) {
	return version.Current.String(), nil
}

func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	codec := json.NewCodec()

	rpcServer := rpc.NewServer()
	rpcServer.RegisterCodec(codec, "application/json")
	rpcServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	rpcServer.RegisterInterceptFunc(vm.metrics.InterceptRequest)
	rpcServer.RegisterAfterFunc(vm.metrics.AfterRequest)
	// name this service "xvm"
	if err := rpcServer.RegisterService(&Service{vm: vm}, "xvm"); err != nil {
		return nil, err
	}

	walletServer := rpc.NewServer()
	walletServer.RegisterCodec(codec, "application/json")
	walletServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	walletServer.RegisterInterceptFunc(vm.metrics.InterceptRequest)
	walletServer.RegisterAfterFunc(vm.metrics.AfterRequest)
	// name this service "wallet"
	err := walletServer.RegisterService(&vm.walletService, "wallet")

	return map[string]http.Handler{
		"":        rpcServer,
		"/wallet": walletServer,
		"/events": vm.pubsub,
	}, err
}

/*
 ******************************************************************************
 ********************************** Chain VM **********************************
 ******************************************************************************
 */

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (chain.Block, error) {
	return vm.chainManager.GetBlock(blkID)
}

func (vm *VM) ParseBlock(_ context.Context, blkBytes []byte) (chain.Block, error) {
	blk, err := vm.parser.ParseBlock(blkBytes)
	if err != nil {
		return nil, err
	}
	return vm.chainManager.NewBlock(blk), nil
}

func (vm *VM) SetPreference(_ context.Context, blkID ids.ID) error {
	vm.chainManager.SetPreference(blkID)
	return nil
}

func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.chainManager.LastAccepted(), nil
}

func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	return vm.state.GetBlockIDAtHeight(height)
}

/*
 ******************************************************************************
 *********************************** DAG VM ***********************************
 ******************************************************************************
 */

func (vm *VM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	time := version.GetCortinaTime(consensus.GetNetworkID(vm.ctx))
	err := vm.state.InitializeChainState(stopVertexID, time)
	if err != nil {
		return err
	}

	// Create a channel for mempool to engine communication
	vm.toEngine = make(chan core.Message, 1)
	mempool, err := xmempool.New("mempool", vm.registerer, vm.toEngine)
	if err != nil {
		return fmt.Errorf("failed to create mempool: %w", err)
	}

	vm.chainManager = blockexecutor.NewManager(
		mempool,
		vm.metrics,
		vm.state,
		vm.txBackend,
		&vm.clock,
		vm.onAccept,
	)

	vm.Builder = blockbuilder.New(
		vm.txBackend,
		vm.chainManager,
		&vm.clock,
		mempool,
	)

	// Invariant: The context lock is not held when calling network.IssueTx.
	// Create a wrapper for ValidatorState to match the expected interface
	// For now, use a nil validator state as we don't have it in context
	validatorStateWrapper := &validatorStateWrapper{vs: nil}
	
	vm.network, err = network.New(
		vm.log,
		consensus.GetNodeID(vm.ctx),
		consensus.GetSubnetID(vm.ctx),
		validatorStateWrapper,
		vm.parser,
		network.NewLockedTxVerifier(
			&vm.lock,
			vm.chainManager,
		),
		mempool,
		vm.appSender,
		vm.registerer,
		vm.networkConfig,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize network: %w", err)
	}

	// Notify the network of our current peers
	for nodeID, version := range vm.connectedPeers {
		if err := vm.network.Connected(ctx, nodeID, version); err != nil {
			return err
		}
	}
	vm.connectedPeers = nil

	// Note: It's important only to switch the networking stack after the full
	// chainVM has been initialized. Traffic will immediately start being
	// handled asynchronously.
	vm.Atomic.Set(vm.network)

	// Only start gossip goroutines if network is properly initialized
	// (avoids panics in test environments)
	if vm.network != nil {
		vm.awaitShutdown.Add(2)
		go func() {
			defer vm.awaitShutdown.Done()

			// Invariant: PushGossip must never grab the context lock.
			vm.network.PushGossip(vm.onShutdownCtx)
		}()
		go func() {
			defer vm.awaitShutdown.Done()

			// Invariant: PullGossip must never grab the context lock.
			vm.network.PullGossip(vm.onShutdownCtx)
		}()
	}

	return nil
}

func (vm *VM) ParseTx(_ context.Context, bytes []byte) (dag.Tx, error) {
	tx, err := vm.parser.ParseTx(bytes)
	if err != nil {
		return nil, err
	}

	err = tx.Unsigned.Visit(&txexecutor.SyntacticVerifier{
		Backend: vm.txBackend,
		Tx:      tx,
	})
	if err != nil {
		return nil, err
	}

	return &Tx{
		vm: vm,
		tx: tx,
	}, nil
}

/*
 ******************************************************************************
 ********************************** JSON API **********************************
 ******************************************************************************
 */

// issueTxFromRPC attempts to send a transaction to consensus.
//
// Invariant: The context lock is not held
// Invariant: This function is only called after Linearize has been called.
func (vm *VM) issueTxFromRPC(tx *txs.Tx) (ids.ID, error) {
	txID := tx.ID()
	err := vm.network.IssueTxFromRPC(tx)
	if err != nil && !errors.Is(err, mempool.ErrDuplicateTx) {
		vm.log.Debug("failed to add tx to mempool",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)
		return txID, err
	}
	return txID, nil
}

/*
 ******************************************************************************
 ********************************** Helpers ***********************************
 ******************************************************************************
 */

func (vm *VM) initGenesis(genesisBytes []byte) error {
	genesisCodec := vm.parser.GenesisCodec()
	genesis := Genesis{}
	if _, err := genesisCodec.Unmarshal(genesisBytes, &genesis); err != nil {
		return err
	}

	stateInitialized, err := vm.state.IsInitialized()
	if err != nil {
		return err
	}

	// secure this by defaulting to luxAsset
	vm.feeAssetID = consensus.LuxAssetID(vm.ctx)

	for index, genesisTx := range genesis.Txs {
		if len(genesisTx.Outs) != 0 {
			return errGenesisAssetMustHaveState
		}

		tx := &txs.Tx{
			Unsigned: &genesisTx.CreateAssetTx,
		}
		if err := tx.Initialize(genesisCodec); err != nil {
			return err
		}

		txID := tx.ID()
		if err := vm.Alias(txID, genesisTx.Alias); err != nil {
			return err
		}

		if !stateInitialized {
			vm.initState(tx)
		}
		if index == 0 {
			vm.log.Info("fee asset is established",
				zap.String("alias", genesisTx.Alias),
				zap.Stringer("assetID", txID),
			)
			vm.feeAssetID = txID
		}
	}

	if !stateInitialized {
		return vm.state.SetInitialized()
	}

	return nil
}

func (vm *VM) initState(tx *txs.Tx) {
	txID := tx.ID()
	vm.log.Info("initializing genesis asset",
		zap.Stringer("txID", txID),
	)
	vm.state.AddTx(tx)
	for _, utxo := range tx.UTXOs() {
		vm.state.AddUTXO(utxo)
	}
}

// LoadUser retrieves user keys from external storage
// TODO: Implement proper key management without context.Context keystore
func (vm *VM) LoadUser(
	username string,
	password string,
	addresses set.Set[ids.ShortID],
) ([]*lux.UTXO, *secp256k1fx.Keychain, error) {
	// For now, return empty keychain and UTXOs
	// This needs to be properly implemented with external key management
	kc := secp256k1fx.NewKeychain()
	utxos := []*lux.UTXO{}
	
	// If addresses provided, get their UTXOs
	if addresses.Len() > 0 {
		allUTXOs, err := lux.GetAllUTXOs(vm.state, addresses)
		if err != nil {
			return nil, nil, fmt.Errorf("problem retrieving UTXOs: %w", err)
		}
		utxos = allUTXOs
	}
	
	return utxos, kc, nil
}

// selectChangeAddr returns the change address to be used for [kc] when [changeAddr] is given
// as the optional change address argument
func (vm *VM) selectChangeAddr(defaultAddr ids.ShortID, changeAddr string) (ids.ShortID, error) {
	if changeAddr == "" {
		return defaultAddr, nil
	}
	addr, err := lux.ParseServiceAddress(vm, changeAddr)
	if err != nil {
		return ids.ShortID{}, fmt.Errorf("couldn't parse changeAddr: %w", err)
	}
	return addr, nil
}

// lookupAssetID looks for an ID aliased by [asset] and if it fails
// attempts to parse [asset] into an ID
func (vm *VM) lookupAssetID(asset string) (ids.ID, error) {
	if assetID, err := vm.Lookup(asset); err == nil {
		return assetID, nil
	}
	if assetID, err := ids.FromString(asset); err == nil {
		return assetID, nil
	}
	return ids.Empty, fmt.Errorf("asset '%s' not found", asset)
}

// Invariant: onAccept is called when [tx] is being marked as accepted, but
// before its state changes are applied.
// Invariant: any error returned by onAccept should be considered fatal.
// TODO: Remove [onAccept] once the deprecated APIs this powers are removed.
func (vm *VM) onAccept(tx *txs.Tx) error {
	// Fetch the input UTXOs
	txID := tx.ID()
	inputUTXOIDs := tx.Unsigned.InputUTXOs()
	inputUTXOs := make([]*lux.UTXO, 0, len(inputUTXOIDs))
	for _, utxoID := range inputUTXOIDs {
		// Don't bother fetching the input UTXO if its symbolic
		if utxoID.Symbolic() {
			continue
		}

		utxo, err := vm.state.GetUTXO(utxoID.InputID())
		if err == database.ErrNotFound {
			vm.log.Debug("dropping utxo from index",
				zap.Stringer("txID", txID),
				zap.Stringer("utxoTxID", utxoID.TxID),
				zap.Uint32("utxoOutputIndex", utxoID.OutputIndex),
			)
			continue
		}
		if err != nil {
			// should never happen because the UTXO was previously verified to
			// exist
			return fmt.Errorf("error finding UTXO %s: %w", utxoID, err)
		}
		inputUTXOs = append(inputUTXOs, utxo)
	}

	outputUTXOs := tx.UTXOs()
	// index input and output UTXOs
	if err := vm.addressTxsIndexer.Accept(txID, inputUTXOs, outputUTXOs); err != nil {
		return fmt.Errorf("error indexing tx: %w", err)
	}

	vm.pubsub.Publish(NewPubSubFilterer(tx))
	vm.walletService.decided(txID)
	return nil
}

// WaitForEvent implements the core.VM interface
func (vm *VM) WaitForEvent(ctx context.Context) (core.Message, error) {
	if vm.toEngine == nil {
		// Before linearization, no events to wait for
		<-ctx.Done()
		return core.PendingTxs, ctx.Err()
	}
	
	select {
	case msg := <-vm.toEngine:
		return msg, nil
	case <-ctx.Done():
		return core.PendingTxs, ctx.Err()
	}
}

// NewHTTPHandler implements the core.VM interface
func (vm *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	// XVM doesn't provide a single HTTP handler, it uses CreateHandlers instead
	return nil, nil
}

// BuildVertex builds a new vertex - required for LinearizableVMWithEngine
func (vm *VM) BuildVertex(ctx context.Context) (dag.Vertex, error) {
	// XVM doesn't use vertices, it uses blocks
	return nil, errors.New("XVM does not support vertex building")
}

// GetVertex gets a vertex by ID - required for LinearizableVMWithEngine
func (vm *VM) GetVertex(ctx context.Context, vtxID ids.ID) (dag.Vertex, error) {
	// XVM doesn't use vertices, it uses blocks
	return nil, errors.New("XVM does not support vertex operations")
}

// ParseVertex parses vertex bytes - required for LinearizableVMWithEngine
func (vm *VM) ParseVertex(ctx context.Context, vtxBytes []byte) (dag.Vertex, error) {
	// XVM doesn't use vertices, it uses blocks
	return nil, errors.New("XVM does not support vertex parsing")
}

// GetEngine returns the consensus engine - required for LinearizableVMWithEngine
func (vm *VM) GetEngine() interface{} {
	// XVM doesn't have a separate engine
	return nil
}

// SetEngine sets the consensus engine - required for LinearizableVMWithEngine
func (vm *VM) SetEngine(engine interface{}) {
	// XVM doesn't use a separate engine
}



// noOpAppHandler is a simple no-op implementation of core.AppHandler
type noOpAppHandler struct{}

func (n *noOpAppHandler) CrossChainAppRequest(context.Context, ids.ID, uint32, time.Time, []byte) error {
	return nil
}

func (n *noOpAppHandler) CrossChainAppRequestFailed(context.Context, ids.ID, uint32, *core.AppError) error {
	return nil
}

func (n *noOpAppHandler) CrossChainAppResponse(context.Context, ids.ID, uint32, []byte) error {
	return nil
}

func (n *noOpAppHandler) AppRequest(context.Context, ids.NodeID, uint32, time.Time, []byte) error {
	return nil
}

func (n *noOpAppHandler) AppRequestFailed(context.Context, ids.NodeID, uint32, *core.AppError) error {
	return nil
}

func (n *noOpAppHandler) AppResponse(context.Context, ids.NodeID, uint32, []byte) error {
	return nil
}

func (n *noOpAppHandler) AppGossip(context.Context, ids.NodeID, []byte) error {
	return nil
}

// validatorStateWrapper wraps interfaces.ValidatorState to match validators.State
type validatorStateWrapper struct {
	vs interfaces.ValidatorState
}

func (v *validatorStateWrapper) GetCurrentHeight() (uint64, error) {
	// GetCurrentHeight doesn't take context in new interface
	return v.vs.GetCurrentHeight()
}

func (v *validatorStateWrapper) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	// Use the GetValidatorSet from interfaces.ValidatorState
	// It already returns map[ids.NodeID]uint64
	return v.vs.GetValidatorSet(height, subnetID)
}
