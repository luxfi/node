// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/luxfi/metric"

	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/version"
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
	lux "github.com/luxfi/utxo"
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
	// Convert the internal message to the external ZAP message
	extMsg, err := extwarp.NewMessage(msg.NetworkID, msg.SourceChainID, msg.Payload)
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
	nodeClock mockable.Clock

	uptimeManager uptime.Calculator
	tracker       *uptimeTracker

	// The runtime wiring of this vm
	rt *runtime.Runtime
	db database.Database

	// Additional fields needed for platformvm
	log         log.Logger
	nodeID      ids.NodeID
	lock        sync.RWMutex
	utxoAssetID ids.ID
	chainID     ids.ID
	state       state.State

	// stateLock serializes every commit of the shared platform state (state.write)
	// that originates OUTSIDE the consensus engine's serialized accept path:
	//   - a block DECISION (block/executor Block.Accept/Reject → state.CommitBatch);
	//     the executor manager holds THIS lock (passed as &vm.stateLock),
	//   - a peer Disconnect (tracker.Disconnect → state.SetUptime, then state.Commit),
	//   - normal-ops Start/StopTracking uptime flushes (state.SetUptime + state.Commit).
	// The engine invokes VM.Accept as a lock-free call-out (its t.mu is released
	// before the call-out per its lock discipline), and peer connect/disconnect run
	// on the node's peer-lifecycle goroutine, so nothing else serializes these
	// writers against one another. Without this lock a disconnect's state.Commit
	// races an accept's state.CommitBatch inside state.write() → Go "concurrent map
	// writes" fatal. This is the avalanchego ctx.Lock invariant (accept serialized
	// with engine.Connected/Disconnected), scoped to the state the platform VM owns.
	// It is DISTINCT from vm.lock (which guards API/service reads): a separate lock
	// keeps the accept path off the API lock and avoids any ordering coupling.
	stateLock sync.Mutex

	fx fx.Fx

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
	// chainRuntime is passed through to sub-components that need it;
	// PlatformVM configures itself from the VMInit struct directly.
	_ = chainRuntime

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

	// Initialize utxo.UTXOAssetID from the context
	utxo.UTXOAssetID = init.Runtime.UTXOAssetID

	// Initialize vm.utxoAssetID for the GetStakingAssetID API.
	vm.utxoAssetID = init.Runtime.UTXOAssetID

	// Get the current database from the DBManager
	// Since DBManager is now an interface{}, we need to handle it differently
	// logic simplified as we now just trust init.DB or fallback to memdb if nil above

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

	// Create the real uptime tracker for the primary network NOW, at Initialize,
	// so peer Connect events delivered during bootstrap are captured — the
	// connected map must already be populated by the time StartTracking runs at
	// normal-operations start. Register it with the thread-safe LockedCalculator,
	// through which both the API (service.go getCurrentValidators) and the reward
	// gate (block/executor/options.go prefersCommit) read uptime.
	vm.tracker = newUptimeTracker(vm.state, constants.PrimaryNetworkID, vm.nodeClock.Time)
	if err := vm.UptimeLockedCalculator.SetCalculator(constants.PrimaryNetworkID, vm.tracker); err != nil {
		return fmt.Errorf("failed to register uptime tracker: %w", err)
	}
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

	// Install the chain-wide credential-admission policy (F102 close-out).
	// SecurityProfile is nil for legacy/classical-compat networks; the
	// mempool gate is a no-op in that case. For strict-PQ networks the
	// gate refuses any tx whose credentials carry an unwrapped classical
	// secp256k1 entry not named in the registry allow-list.
	mempool.SetAuthPolicy(vm.SecurityProfile, vm.ClassicalCompatRegistry)

	vm.manager = blockexecutor.NewManager(
		mempool,
		vm.metrics,
		vm.state,
		txExecutorBackend,
		validatorManager,
		&vm.stateLock,
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

// Create all chains that exist that this node validates.
func (vm *VM) initBlockchains() error {
	if vm.Internal.PartialSyncPrimaryNetwork {
		vm.log.Info("skipping primary network chain creation")
	} else if err := vm.createNet(constants.PrimaryNetworkID); err != nil {
		return err
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
	} else if vm.SybilProtectionEnabled && len(vm.TrackedChains) > 0 {
		// TrackedChains may contain either chain IDs or blockchain IDs.
		// Try each as a chain ID first; if no chains found, scan all
		// chains for blockchains matching the tracked ID.
		resolved := make(map[ids.ID]bool) // chain IDs to create
		unresolved := make(map[ids.ID]bool)

		for chainID := range vm.TrackedChains {
			chains, err := vm.state.GetChains(chainID)
			if err != nil {
				return err
			}
			if len(chains) > 0 {
				// It's a chain ID
				resolved[chainID] = true
			} else {
				// May be a blockchain ID - need reverse lookup
				unresolved[chainID] = true
			}
		}

		// Resolve blockchain IDs by scanning all chains
		if len(unresolved) > 0 {
			netIDs, err := vm.state.GetChainIDs()
			if err != nil {
				return err
			}
			for _, netID := range netIDs {
				chains, err := vm.state.GetChains(netID)
				if err != nil {
					return err
				}
				for _, chain := range chains {
					if unresolved[chain.ID()] {
						resolved[netID] = true
						delete(unresolved, chain.ID())
					}
				}
				if len(unresolved) == 0 {
					break
				}
			}
		}

		for netID := range resolved {
			if err := vm.createNet(netID); err != nil {
				return err
			}
		}
	}
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

	// On a normal-ops → re-bootstrap transition, flush and stop uptime tracking
	// so connected sessions are persisted before we stop measuring. This is a
	// no-op on the first bootstrap (tracking hasn't started yet). StopTracking
	// flushes uptime into shared state (state.SetUptime → state.write), so hold
	// stateLock to serialize it with any block accept still in flight.
	if err := func() error {
		vm.stateLock.Lock()
		defer vm.stateLock.Unlock()
		if vm.tracker != nil && vm.tracker.StartedTracking() {
			primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
			if err := vm.tracker.StopTracking(primaryVdrIDs); err != nil {
				return err
			}
		}
		return nil
	}(); err != nil {
		return err
	}
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

	// Begin tracking validator uptime for the primary network. The tracker was
	// created and registered at Initialize (so bootstrap-time peer connections
	// were already captured); StartTracking baselines every current validator's
	// uptime record and switches the tracker into live-tracking mode. Mirrors
	// avalanchego's onNormalOperationsStarted.
	//
	// StartTracking (state.SetUptime) and the trailing state.Commit both write
	// shared state, so hold stateLock across them to serialize with any block
	// accept (Block.Accept holds the same lock) — the same guard as the peer
	// Disconnect path.
	if err := func() error {
		vm.stateLock.Lock()
		defer vm.stateLock.Unlock()
		if vm.tracker != nil && !vm.tracker.StartedTracking() {
			primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
			if err := vm.tracker.StartTracking(primaryVdrIDs); err != nil {
				return err
			}
			vm.log.Info("uptime tracking started for primary network",
				log.Int("validators", len(primaryVdrIDs)))
		}

		// Commit state BEFORE starting background goroutines to avoid race conditions
		// between state readers (forwardNotifications) and state writers (Commit)
		return vm.state.Commit()
	}(); err != nil {
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
	switch vmcore.State(stateNum) {
	case vmcore.Bootstrapping:
		return vm.onBootstrapStarted()
	case vmcore.Ready:
		return vm.onReady()
	default:
		return nil
	}
}

// Shutdown this blockchain
func (vm *VM) Shutdown(context.Context) error {
	if vm.db == nil {
		return nil
	}

	vm.onShutdownCtxCancel()

	// Flush uptime for all primary-network validators before closing state, so
	// connected sessions are durably persisted across the restart. StopTracking
	// (state.SetUptime) + state.Commit write shared state, so hold stateLock to
	// serialize with any block accept still draining as the chain stops.
	if err := func() error {
		vm.stateLock.Lock()
		defer vm.stateLock.Unlock()
		if vm.tracker != nil && vm.tracker.StartedTracking() {
			primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
			if err := vm.tracker.StopTracking(primaryVdrIDs); err != nil {
				return err
			}
			if err := vm.state.Commit(); err != nil {
				return err
			}
		}
		return nil
	}(); err != nil {
		return err
	}

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
	// Blocks are native struct-is-wire (zap): Parse re-wraps the self-delimiting
	// buffer zero-copy — no codec, no version prefix.
	statelessBlk, err := block.Parse(b)
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
	if vm.tracker != nil {
		vm.tracker.Connect(nodeID)
	}
	return vm.Network.Connected(ctx, nodeID, nodeVersion)
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	// This runs on the node's peer-lifecycle goroutine, which the consensus
	// engine does NOT serialize against block accept. tracker.Disconnect flushes
	// the peer's uptime into shared state (state.SetUptime) and state.Commit
	// persists the whole diff (state.write). Hold stateLock so both are atomic
	// w.r.t. a concurrent block accept (Block.Accept holds the same lock);
	// otherwise state.write races the acceptor's state.write → concurrent map
	// writes fatal. The p2p Network.Disconnected below touches only the p2p peer
	// set (its own lock), so it stays OUTSIDE stateLock to keep the critical
	// section to the state commit.
	//
	// Commit ONLY when the flush actually wrote uptime state. Before StartTracking
	// (bootstrap churn) and for non-validator peers, Disconnect is a no-op, and an
	// unconditional Commit would be an empty full-write+fsync on every such
	// disconnect. A no-write disconnect leaves the state clean, so skipping Commit
	// is correct — every writer under stateLock (block accept, Start/StopTracking)
	// commits its own diff, so there is never an orphaned write relying on us.
	vm.stateLock.Lock()
	if vm.tracker != nil {
		mutated, err := vm.tracker.Disconnect(nodeID)
		if err != nil {
			vm.stateLock.Unlock()
			return err
		}
		if mutated {
			if err := vm.state.Commit(); err != nil {
				vm.stateLock.Unlock()
				return err
			}
		}
	}
	vm.stateLock.Unlock()
	return vm.Network.Disconnected(ctx, nodeID)
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
