// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
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
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	metrics "github.com/luxfi/metric"

	"github.com/luxfi/codec"
	"github.com/luxfi/constants"
	"github.com/luxfi/address"
	chain "github.com/luxfi/vm/chain"
	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/consensus/engine/dag"
	dagvertex "github.com/luxfi/consensus/engine/dag/vertex"
	"github.com/luxfi/container/linked"
	"github.com/luxfi/database"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/pubsub"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/components/index"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/xvm/block"
	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/network"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/vms/txs/auth"
	"github.com/luxfi/node/vms/xvm/utxo"
	"github.com/luxfi/node/vms/txs/mempool"
	"github.com/luxfi/runtime"
	"github.com/luxfi/timer/mockable"
	"github.com/luxfi/utxo/secp256k1fx"
	validators "github.com/luxfi/validators"
	consensusversion "github.com/luxfi/version"
	vmcore "github.com/luxfi/vm"
	"github.com/luxfi/warp"

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
)

// BCLookup provides blockchain alias lookup
type BCLookup interface {
	Lookup(string) (ids.ID, error)
	PrimaryAlias(ids.ID) (string, error)
}

// SharedMemory provides cross-chain shared memory
type SharedMemory interface {
	Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error)
	Apply(map[ids.ID]interface{}, ...interface{}) error
}

type VM struct {
	network.Atomic

	config.Config

	metrics xvmmetrics.Metrics

	lux.AddressManager
	ids.Aliaser
	utxo.Spender

	// Consensus context
	consensusRuntime *runtime.Runtime

	// Logger for this VM
	log log.Logger

	// Lock for thread safety (exposed for tests)
	Lock sync.RWMutex

	// Chain information
	ChainID  ids.ID
	XChainID ids.ID

	// BCLookup provides blockchain alias lookup
	bcLookup BCLookup

	// SharedMemory for cross-chain operations
	SharedMemory SharedMemory

	// Used to check local time
	clock mockable.Clock

	registerer metrics.Registerer

	connectedPeers map[ids.NodeID]*version.Application

	parser block.Parser

	pubsub *pubsub.Server

	sender warp.Sender

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
	toEngine chan vmcore.Message

	// F102 close-out: chain-wide credential-admission policy resolved
	// from genesis at node bootstrap. The mempool builder installs this
	// via SetAuthPolicy so strict-PQ chains refuse classical secp256k1
	// credentials at gossip time.
	securityProfile         *consensusconfig.ChainSecurityProfile
	classicalCompatRegistry auth.ClassicalCompatRegistry
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	// If the chain isn't linearized yet, we must track the peers externally
	// until the network is initialized.
	if vm.network == nil {
		vm.connectedPeers[nodeID] = version
		return nil
	}
	// Convert to consensus version type
	consensusVer := &consensusversion.Application{
		Name:  version.Name,
		Major: version.Major,
		Minor: version.Minor,
		Patch: version.Patch,
	}
	return vm.network.Connected(ctx, nodeID, consensusVer)
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

func (vm *VM) Initialize(
	ctx context.Context,
	init vmcore.Init,
) error {
	_ = ctx
	// Try to get Runtime for chain info
	if init.Runtime != nil {
		// Store chain-specific info from Runtime
		vm.consensusRuntime = init.Runtime
		vm.ChainID = init.Runtime.ChainID
		vm.XChainID = init.Runtime.ChainID // For XVM, this is the same

		// SharedMemory will be set by the chains manager when the VM is created
	}

	db := init.DB
	if db == nil {
		return errors.New("invalid database: nil")
	}

	// Fx types are canonical: require *vmcore.Fx.
	fxs := init.Fx
	coreFxs := make([]*vmcore.Fx, len(fxs))
	for i, fx := range fxs {
		if fx == nil {
			continue
		}
		fxTyped, ok := fx.(*vmcore.Fx)
		if !ok {
			return fmt.Errorf("unexpected fx type %T", fx)
		}
		coreFxs[i] = fxTyped
	}

	// Check sender type
	appSender := init.Sender
	if appSender == nil {
		// In single-node mode, we can work without a Sender
		// Create a no-op Sender
		appSender = &noOpSender{}
	}

	return vm.initialize(ctx, ctx, db, init.Genesis, init.Upgrade, init.Config, coreFxs, appSender)
}

// Original Initialize method renamed to initialize
func (vm *VM) initialize(
	_ context.Context,
	ctx context.Context,
	db database.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	fxs []*vmcore.Fx,
	sender warp.Sender,
) error {
	// Initialize logger first
	vm.log = log.Noop()

	// Create a simple no-op handler for warp.Handler
	noopMessageHandler := &noOpHandler{}
	vm.Atomic = network.NewAtomic(noopMessageHandler)

	xvmConfig, err := ParseConfig(configBytes)
	if err != nil {
		return err
	}

	// Assign parsed config to VM
	vm.Config = xvmConfig.Config

	vm.log.Info("VM config initialized",
		log.Reflect("config", xvmConfig),
	)

	// Get metrics from a global registry or create new one
	vm.registerer = metric.NewRegistry()

	vm.connectedPeers = make(map[ids.NodeID]*version.Application)

	// Initialize metrics as soon as possible
	vm.metrics, err = xvmmetrics.New(vm.registerer)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	vm.AddressManager = lux.NewAddressManager(vm.consensusRuntime)
	vm.Aliaser = ids.NewAliaser()

	vm.sender = sender
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

		// Type assert to extensions.Fx
		fx, ok := fxContainer.Fx.(extensions.Fx)
		if !ok {
			return errIncompatibleFx
		}

		typedFxs[i] = fx
		vm.fxs[i] = &extensions.ParsedFx{
			ID: fxContainer.ID,
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

	// Initialize transaction indexer based on config
	// Note: The indexer uses baseDB directly to avoid versiondb batching issues.
	// Indexer writes need to be immediately visible and not subject to versiondb rollback.
	if vm.Config.IndexTransactions {
		vm.log.Info("address transaction indexing is enabled")
		vm.addressTxsIndexer, err = index.NewIndexer(vm.baseDB, vm.log, "", vm.registerer, true)
		if err != nil {
			return fmt.Errorf("failed to initialize indexer: %w", err)
		}
	} else {
		vm.log.Info("address transaction indexing is disabled")
		vm.addressTxsIndexer, err = index.NewNoIndexer(vm.baseDB, false)
		if err != nil {
			return fmt.Errorf("failed to initialize disabled indexer: %w", err)
		}
	}

	vm.txBackend = &txexecutor.Backend{
		Ctx:           ctx,
		Runtime:       vm.consensusRuntime,
		Config:        &vm.Config,
		Fxs:           vm.fxs,
		TypeToFxIndex: vm.typeToFxIndex,
		Codec:         vm.parser.Codec(),
		FeeAssetID:    vm.feeAssetID,
		Bootstrapped:  false,
		SharedMemory:  vm.SharedMemory,
		Log:           vm.log,
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

func (vm *VM) onReady() error {
	vm.txBackend.Bootstrapped = true
	for _, fx := range vm.fxs {
		if err := fx.Fx.Bootstrapped(); err != nil {
			return err
		}
	}

	vm.bootstrapped = true
	return nil
}

func (vm *VM) SetState(_ context.Context, stateNum uint32) error {
	switch vmcore.State(stateNum) {
	case vmcore.Bootstrapping:
		return vm.onBootstrapStarted()
	case vmcore.Ready:
		return vm.onReady()
	default:
		return nil
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

func (vm *VM) CreateStaticHandlers(context.Context) (map[string]http.Handler, error) {
	// Return static handlers (if any)
	return nil, nil
}

func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	codec := json.NewCodec()

	rpcServer := rpc.NewServer()
	rpcServer.RegisterCodec(codec, "application/json")
	rpcServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if vm.metrics != nil {
		rpcServer.RegisterInterceptFunc(vm.metrics.InterceptRequest)
		rpcServer.RegisterAfterFunc(vm.metrics.AfterRequest)
	}
	// name this service "xvm"
	if err := rpcServer.RegisterService(&Service{vm: vm}, "xvm"); err != nil {
		return nil, err
	}

	walletServer := rpc.NewServer()
	walletServer.RegisterCodec(codec, "application/json")
	walletServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if vm.metrics != nil {
		walletServer.RegisterInterceptFunc(vm.metrics.InterceptRequest)
		walletServer.RegisterAfterFunc(vm.metrics.AfterRequest)
	}
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
	if vm.chainManager != nil {
		vm.chainManager.SetPreference(blkID)
	}
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

// ParseAddress resolves chain aliases (like "X") via BCLookup before parsing.
// This overrides the embedded AddressManager which can't resolve aliases.
func (vm *VM) ParseAddress(addrStr string) (ids.ID, ids.ShortID, error) {
	chainAlias, hrp, addrBytes, err := address.Parse(addrStr)
	if err != nil {
		return ids.Empty, ids.ShortID{}, err
	}

	// Try BCLookup first (resolves "X" → actual blockchain ID)
	var chainID ids.ID
	if vm.consensusRuntime != nil && vm.consensusRuntime.BCLookup != nil {
		chainID, err = vm.consensusRuntime.BCLookup.Lookup(chainAlias)
	}
	if err != nil || chainID == ids.Empty {
		// Fallback: try parsing as raw ID
		chainID, err = ids.FromString(chainAlias)
		if err != nil {
			return ids.Empty, ids.ShortID{}, fmt.Errorf("unknown chain alias %q: %w", chainAlias, err)
		}
	}

	expectedHRP := constants.GetHRP(vm.consensusRuntime.NetworkID)
	if hrp != expectedHRP {
		return ids.Empty, ids.ShortID{}, fmt.Errorf("expected hrp %q but got %q", expectedHRP, hrp)
	}

	addr, err := ids.ToShortID(addrBytes)
	return chainID, addr, err
}

func (vm *VM) Linearize(ctx context.Context, stopVertexID ids.ID, toEngine chan<- vmcore.Message) error {
	// Chain state initialization timestamp under activate-all-implicitly is
	// the canonical Lux InitiallyActiveTime (Dec 5, 2020).
	err := vm.state.InitializeChainState(stopVertexID, upgrade.InitiallyActiveTime)
	if err != nil {
		return err
	}

	// Note: toEngine parameter is for compatibility with LinearizableVMWithEngine interface
	// The XVM uses its own internal channel for mempool communication
	_ = toEngine

	// Create a channel for mempool to engine communication
	vm.toEngine = make(chan vmcore.Message, 1)
	mempool, err := xmempool.New("mempool", vm.registerer)
	if err != nil {
		return fmt.Errorf("failed to create mempool: %w", err)
	}

	// F102 close-out: install the chain-wide credential-admission
	// policy. Nil profile is a no-op gate (legacy/classical-compat
	// networks); strict-PQ chains receive a non-nil profile from
	// the node bootstrap via xvm.Factory.
	mempool.SetAuthPolicy(vm.securityProfile, vm.classicalCompatRegistry)

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
	// Get ValidatorState from Runtime
	if vm.consensusRuntime.ValidatorState == nil {
		return fmt.Errorf("validator state not available in Runtime")
	}
	vs, ok := vm.consensusRuntime.ValidatorState.(runtime.ValidatorState)
	if !ok {
		return fmt.Errorf("validator state has incorrect type")
	}
	validatorStateWrapper := &validatorStateWrapper{vs: vs}

	vm.network, err = network.New(
		vm.log,
		vm.consensusRuntime.NodeID,
		vm.consensusRuntime.ChainID,
		validatorStateWrapper,
		vm.parser,
		network.NewLockedTxVerifier(
			&vm.Lock,
			vm.chainManager,
		),
		mempool,
		vm.sender,
		vm.registerer,
		vm.networkConfig,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize network: %w", err)
	}

	// Notify the network of our current peers
	for nodeID, version := range vm.connectedPeers {
		// Convert to consensus version type
		consensusVer := &consensusversion.Application{
			Name:  version.Name,
			Major: version.Major,
			Minor: version.Minor,
			Patch: version.Patch,
		}
		if err := vm.network.Connected(ctx, nodeID, consensusVer); err != nil {
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
			log.Stringer("txID", txID),
			log.String("error", err.Error()),
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

	// secure this by defaulting to xAsset
	// Use empty ID as default, will be set by first genesis asset
	vm.feeAssetID = ids.Empty

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
				log.String("alias", genesisTx.Alias),
				log.Stringer("assetID", txID),
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
		log.Stringer("txID", txID),
	)
	vm.state.AddTx(tx)
	for _, utxo := range tx.UTXOs() {
		vm.state.AddUTXO(utxo)
	}
}

// LoadUser retrieves user keys from external storage
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
// Note: errors are logged but not returned as this callback must not fail.
func (vm *VM) onAccept(tx *txs.Tx) {
	// Fetch the input UTXOs
	txID := tx.ID()
	vm.log.Info("onAccept called", log.Stringer("txID", txID))
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
				log.Stringer("txID", txID),
				log.Stringer("utxoTxID", utxoID.TxID),
				log.Uint32("utxoOutputIndex", utxoID.OutputIndex),
			)
			continue
		}
		if err != nil {
			// should never happen because the UTXO was previously verified to exist
			vm.log.Error("error finding UTXO on accept",
				log.Stringer("utxoID", utxoID),
				log.Err(err),
			)
			continue
		}
		inputUTXOs = append(inputUTXOs, utxo)
	}

	outputUTXOs := tx.UTXOs()
	// index input and output UTXOs
	if err := vm.addressTxsIndexer.Accept(txID, inputUTXOs, outputUTXOs); err != nil {
		vm.log.Error("error indexing tx",
			log.Stringer("txID", txID),
			log.Err(err),
		)
	} else {
		vm.log.Debug("indexed tx successfully",
			log.Stringer("txID", txID),
			log.Int("inputs", len(inputUTXOs)),
			log.Int("outputs", len(outputUTXOs)),
		)
	}

	vm.pubsub.Publish(NewPubSubFilterer(tx))
	vm.walletService.decided(txID)
}

// WaitForEvent implements the engine.VM interface
func (vm *VM) WaitForEvent(ctx context.Context) (interface{}, error) {
	if vm.toEngine == nil {
		// Before linearization, no events to wait for
		<-ctx.Done()
		return vmcore.PendingTxs, ctx.Err()
	}

	select {
	case msgType := <-vm.toEngine:
		return msgType, nil
	case <-ctx.Done():
		return vmcore.PendingTxs, ctx.Err()
	}
}

// NewHTTPHandler implements the engine.VM interface
func (vm *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	// XVM doesn't provide a single HTTP handler, it uses CreateHandlers instead
	return nil, nil
}

// BuildVertex builds a new vertex - required for LinearizableVMWithEngine
func (vm *VM) BuildVertex(ctx context.Context) (dagvertex.Vertex, error) {
	// XVM doesn't use vertices, it uses blocks
	return nil, errors.New("XVM does not support vertex building")
}

// GetVertex gets a vertex by ID - required for LinearizableVMWithEngine
func (vm *VM) GetVertex(ctx context.Context, vtxID ids.ID) (dagvertex.Vertex, error) {
	// XVM doesn't use vertices, it uses blocks
	return nil, errors.New("XVM does not support vertex operations")
}

// ParseVertex parses vertex bytes - required for LinearizableVMWithEngine
func (vm *VM) ParseVertex(ctx context.Context, vtxBytes []byte) (dagvertex.Vertex, error) {
	// XVM doesn't use vertices, it uses blocks
	return nil, errors.New("XVM does not support vertex parsing")
}

// GetEngine returns the consensus engine - required for LinearizableVMWithEngine
func (vm *VM) GetEngine() dag.Engine {
	// XVM doesn't have a separate engine, return a new DAG engine
	return dag.New()
}

// SetEngine sets the consensus engine - required for LinearizableVMWithEngine
func (vm *VM) SetEngine(engine interface{}) {
	// XVM doesn't use a separate engine
}

// GetTx returns a transaction by ID - required for LinearizableVMWithEngine
func (vm *VM) GetTx(ctx context.Context, txID ids.ID) (dag.Transaction, error) {
	tx, err := vm.state.GetTx(txID)
	if err != nil {
		return nil, err
	}
	return &Tx{
		vm: vm,
		tx: tx,
	}, nil
}

// noOpHandler is a simple no-op implementation of warp.Handler
type noOpHandler struct{}

var _ warp.Handler = (*noOpHandler)(nil)

func (n *noOpHandler) Request(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) ([]byte, *warp.Error) {
	return nil, nil
}

func (n *noOpHandler) Response(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return nil
}

func (n *noOpHandler) Gossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return nil
}

func (n *noOpHandler) RequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, err *warp.Error) error {
	return nil
}

// GetCurrentValidatorOutput represents current validator info
type GetCurrentValidatorOutput struct {
	NodeID    ids.NodeID
	PublicKey interface{}
	Weight    uint64
}

// validatorStateWrapper wraps validator state
type validatorStateWrapper struct {
	vs runtime.ValidatorState
}

func (v *validatorStateWrapper) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return v.vs.GetCurrentHeight(ctx)
}

func (v *validatorStateWrapper) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Get the validator set from consensus ValidatorState
	return v.vs.GetValidatorSet(ctx, height, netID)
}

func (v *validatorStateWrapper) GetCurrentValidatorSet(ctx context.Context, netID ids.ID) (map[ids.ID]*GetCurrentValidatorOutput, uint64, error) {
	// Get current height
	height, err := v.vs.GetCurrentHeight(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Get validators at current height
	valSet, err := v.vs.GetValidatorSet(ctx, height, netID)
	if err != nil {
		return nil, 0, err
	}

	// Convert to GetCurrentValidatorOutput format
	result := make(map[ids.ID]*GetCurrentValidatorOutput, len(valSet))
	for nodeID, validator := range valSet {
		// Convert NodeID to ID by copying the bytes
		var id ids.ID
		copy(id[:], nodeID[:])
		result[id] = &GetCurrentValidatorOutput{
			NodeID: nodeID,
			Weight: validator.Weight,
		}
	}

	return result, height, nil
}

func (v *validatorStateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return v.vs.GetMinimumHeight(ctx)
}

func (v *validatorStateWrapper) GetChainID(netID ids.ID) (ids.ID, error) {
	return v.vs.GetChainID(netID)
}

func (v *validatorStateWrapper) GetNetworkID(chainID ids.ID) (ids.ID, error) {
	return v.vs.GetNetworkID(chainID)
}

func (v *validatorStateWrapper) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Get validators at specified height - now directly returns *validators.GetValidatorOutput
	return v.vs.GetValidatorSet(ctx, height, netID)
}

func (v *validatorStateWrapper) GetWarpValidatorSet(ctx context.Context, height uint64, netID ids.ID) (*validators.WarpSet, error) {
	// Get the validator set at the requested height
	vdrSet, err := v.GetValidatorSet(ctx, height, netID)
	if err != nil {
		return nil, err
	}

	// Convert to WarpSet format (Height + Validators map)
	warpValidators := make(map[ids.NodeID]*validators.WarpValidator, len(vdrSet))
	for nodeID, vdr := range vdrSet {
		// Only include validators with BLS public keys
		if len(vdr.PublicKey) > 0 {
			warpValidators[nodeID] = &validators.WarpValidator{
				NodeID:    nodeID,
				PublicKey: vdr.PublicKey,
				Weight:    vdr.Weight,
			}
		}
	}

	return &validators.WarpSet{
		Height:     height,
		Validators: warpValidators,
	}, nil
}

func (v *validatorStateWrapper) GetWarpValidatorSets(ctx context.Context, heights []uint64, netIDs []ids.ID) (map[ids.ID]map[uint64]*validators.WarpSet, error) {
	result := make(map[ids.ID]map[uint64]*validators.WarpSet)

	// For each netID, get validator sets for all requested heights
	for _, netID := range netIDs {
		heightMap := make(map[uint64]*validators.WarpSet)
		for _, height := range heights {
			warpSet, err := v.GetWarpValidatorSet(ctx, height, netID)
			if err != nil {
				return nil, err
			}
			heightMap[height] = warpSet
		}
		result[netID] = heightMap
	}

	return result, nil
}

// Clock returns the VM's clock for time-related operations
func (vm *VM) Clock() *mockable.Clock {
	return &vm.clock
}

// CodecRegistry returns the codec registry for marshalling/unmarshalling
func (vm *VM) CodecRegistry() codec.Registry {
	if vm.parser == nil {
		return nil
	}
	return vm.parser.CodecRegistry()
}

// Logger returns the VM's logger
func (vm *VM) Logger() log.Logger {
	return vm.log
}

// noOpSender is a minimal implementation of warp.Sender for single-node mode
type noOpSender struct{}

var _ warp.Sender = (*noOpSender)(nil)

func (n *noOpSender) SendRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
	return nil
}

func (n *noOpSender) SendResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, responseBytes []byte) error {
	return nil
}

func (n *noOpSender) SendError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	return nil
}

func (n *noOpSender) SendGossip(ctx context.Context, config warp.SendConfig, gossipBytes []byte) error {
	return nil
}
