// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	nodeconsensus "github.com/luxfi/node/consensus"
	// xvm "github.com/luxfi/node/vms/exchangevm" // Unused
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/node/api/health"
	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/node/api/server"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/database"
	// "github.com/luxfi/database/badgerdb" // Unused
	dbmanager "github.com/luxfi/database/manager"
	consensusctx "github.com/luxfi/consensus/context"
	// "github.com/luxfi/database/meterdb" // Unused
	// "github.com/luxfi/database/prefixdb" // Unused
	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/engine"
	"github.com/luxfi/ids"
	"github.com/luxfi/warp"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
	// "github.com/luxfi/p2p" // Unused
	// "github.com/luxfi/consensus/engine/dag/bootstrap/queue" // Unused
	// "github.com/luxfi/consensus/engine/dag/state" // Unused
	// "github.com/luxfi/consensus/engine/vertex" // Unused
	"github.com/luxfi/consensus/engine/interfaces"
	// "github.com/luxfi/consensus/core/tracker"
	consensuschain "github.com/luxfi/consensus/engine/chain"
	consensusdag "github.com/luxfi/consensus/engine/dag"
	"github.com/luxfi/consensus/engine/chain/block"
	// "github.com/luxfi/consensus/engine/chain/syncer"
	"github.com/luxfi/consensus/networking/handler"
	// "github.com/luxfi/consensus/core/router" // Deprecated - using local ChainRouter interface instead
	// "github.com/luxfi/consensus/networking/sender" // Unused after dead code cleanup
	"github.com/luxfi/consensus/networking/timeout"
	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/trace"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/utils/buffer"
	"github.com/luxfi/const"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/log"
	utilmetric "github.com/luxfi/node/utils/metric"
	"github.com/luxfi/node/utils/perms"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/fx"
	// "github.com/luxfi/node/vms/metervm" // Temporarily disabled - needs consensus package updates
	"github.com/luxfi/node/vms/nftfx"

	"github.com/luxfi/node/vms/propertyfx"
	// "github.com/luxfi/node/vms/proposervm"
	"github.com/luxfi/node/vms/secp256k1fx"
	// "github.com/luxfi/node/vms/tracedvm" // Temporarily disabled - needs consensus package updates

	// p2ppb "github.com/luxfi/node/proto/pb/p2p"
	// smcon "github.com/luxfi/consensus/engine/chain"
	// aveng "github.com/luxfi/consensus/engine/dag"
	// avbootstrap "github.com/luxfi/consensus/engine/dag/bootstrap"
	// avagetter "github.com/luxfi/consensus/engine/dag/getter"
	// smeng "github.com/luxfi/consensus/engine/chain"
	// smbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	// consensusgetter "github.com/luxfi/consensus/engine/chain/getter"
	timetracker "github.com/luxfi/node/network/tracker"
)

const (
	ChainLabel = "chain"

	defaultChannelSize = 1
	initialQueueSize   = 3

	luxNamespace    = constants.PlatformName + utilmetric.NamespaceSeparator + "lux"
	handlerNamespace      = constants.PlatformName + utilmetric.NamespaceSeparator + "handler"
	meterchainvmNamespace = constants.PlatformName + utilmetric.NamespaceSeparator + "meterchainvm"
	meterdagvmNamespace   = constants.PlatformName + utilmetric.NamespaceSeparator + "meterdagvm"
	proposervmNamespace   = constants.PlatformName + utilmetric.NamespaceSeparator + "proposervm"
	p2pNamespace          = constants.PlatformName + utilmetric.NamespaceSeparator + "p2p"
	chainNamespace      = constants.PlatformName + utilmetric.NamespaceSeparator + "consensusman"
	stakeNamespace        = constants.PlatformName + utilmetric.NamespaceSeparator + "stake"
)

// ChainRouter is the interface for routing messages to chains.
// This is defined here to avoid circular imports with the node package.
type ChainRouter interface {
	AddChain(ctx context.Context, chainID ids.ID, handler handler.Handler)
}

var (
	// corely shared VM DB prefix
	VMDBPrefix = []byte("vm")

	// Bootstrapping prefixes for LinearizableVMs
	VertexDBPrefix              = []byte("vertex")
	VertexBootstrappingDBPrefix = []byte("vertex_bs")
	TxBootstrappingDBPrefix     = []byte("tx_bs")
	BlockBootstrappingDBPrefix  = []byte("interval_block_bs")

	// Bootstrapping prefixes for ChainVMs
	ChainBootstrappingDBPrefix = []byte("interval_bs")

	errUnknownVMType           = errors.New("the vm should have type lux.DAGVM or chain.ChainVM")
	errCreatePlatformVM        = errors.New("attempted to create a chain running the PlatformVM")
	errNotBootstrapped         = errors.New("subnets not bootstrapped")
	errPartialSyncAsAValidator = errors.New("partial sync should not be configured for a validator")

	fxs = map[ids.ID]fx.Factory{
		secp256k1fx.ID: &secp256k1fx.Factory{},
		nftfx.ID:       &nftfx.Factory{},
		propertyfx.ID:  &propertyfx.Factory{},
	}

	_ Manager = (*manager)(nil)
)

// Manager manages the chains running on this node.
// It can:
//   - Create a chain
//   - Add a registrant. When a chain is created, each registrant calls
//     RegisterChain with the new chain as the argument.
//   - Manage the aliases of chains
type Manager interface {
	ids.Aliaser

	// Queues a chain to be created in the future after chain creator is unblocked.
	// This is only called from the P-chain thread to create other chains
	// Queued chains are created only after P-chain is bootstrapped.
	// This assumes only chains in tracked subnets are queued.
	QueueChainCreation(ChainParameters)

	// Add a registrant [r]. Every time a chain is
	// created, [r].RegisterChain([new chain]) is called.
	AddRegistrant(Registrant)

	// Given an alias, return the ID of the chain associated with that alias
	Lookup(string) (ids.ID, error)

	// Given an alias, return the ID of the VM associated with that alias
	LookupVM(string) (ids.ID, error)

	// Returns true iff the chain with the given ID exists and is finished bootstrapping
	IsBootstrapped(ids.ID) bool

	// Starts the chain creator with the initial platform chain parameters, must
	// be called once.
	StartChainCreator(platformChain ChainParameters) error

	// RetryPendingChains re-queues chains that were waiting for the specified VM.
	// This is called when a VM is hot-loaded via admin.loadVMs.
	RetryPendingChains(vmID ids.ID) int

	// GetPendingChains returns the chain parameters waiting for a VM to be loaded.
	GetPendingChains(vmID ids.ID) []ChainParameters

	Shutdown()
}

// ChainParameters defines the chain being created
type ChainParameters struct {
	// The ID of the blockchain being created.
	ID ids.ID
	// ID of the chain that validates this blockchain.
	ChainID ids.ID
	// The genesis data of this blockchain's ledger.
	GenesisData []byte
	// The ID of the vm this blockchain is running.
	VMID ids.ID
	// The IDs of the feature extensions this blockchain is running.
	FxIDs []ids.ID
	// Invariant: Only used when [ID] is the P-chain ID.
	CustomBeacons validators.Manager
	// Name of the chain (used for HTTP routing alias, e.g., /ext/bc/zoo/rpc)
	Name string
}

type chainInfo struct {
	Name    string
	Context *consensusctx.Context
	VM      interface{} // Use interface{} since VM implementations vary
	Handler handler.Handler
	Engine  Engine // Added to handle Start/Stop operations
}

// Engine represents a consensus engine
type Engine interface {
	Start(context.Context, bool) error
	StopWithError(context.Context, error) error
	Context() context.Context
}


// validatorStateWrapper wraps validators.State to implement interfaces.ValidatorState

// noopValidatorState provides a no-op implementation of validators.State for non-staking nodes
type noopValidatorState struct{}

func (n *noopValidatorState) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

func (n *noopValidatorState) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

func (n *noopValidatorState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (n *noopValidatorState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (n *noopValidatorState) GetWarpValidatorSets(ctx context.Context, heights []uint64, netIDs []ids.ID) (map[ids.ID]map[uint64]*validators.WarpSet, error) {
	result := make(map[ids.ID]map[uint64]*validators.WarpSet)
	for _, netID := range netIDs {
		result[netID] = make(map[uint64]*validators.WarpSet)
		for _, height := range heights {
			result[netID][height] = &validators.WarpSet{
				Height:     height,
				Validators: make(map[ids.NodeID]*validators.WarpValidator),
			}
		}
	}
	return result, nil
}

func (n *noopValidatorState) GetWarpValidatorSet(ctx context.Context, height uint64, netID ids.ID) (*validators.WarpSet, error) {
	return &validators.WarpSet{
		Height:     height,
		Validators: make(map[ids.NodeID]*validators.WarpValidator),
	}, nil
}

// getValidatorState returns the validator state or a no-op implementation if nil
func getValidatorState(state validators.State) validators.State {
	if state != nil {
		return state
	}
	return &noopValidatorState{}
}

// createWarpSigner creates a warp.Signer from a bls.Signer
func createWarpSigner(sk bls.Signer, networkID uint32, chainID ids.ID) warp.Signer {
	if sk == nil {
		return nil
	}
	return warp.NewSigner(sk, networkID, chainID)
}


// ChainConfig is configuration settings for the current execution.
// [Config] is the user-provided config blob for the chain.
// [Upgrade] is a chain-specific blob for coordinating upgrades.
type ChainConfig struct {
	Config  []byte
	Upgrade []byte
}

type ManagerConfig struct {
	SybilProtectionEnabled bool
	StakingTLSSigner       crypto.Signer
	StakingTLSCert         *staking.Certificate
	StakingBLSKey          bls.Signer
	TracingEnabled         bool
	// Must not be used unless [TracingEnabled] is true as this may be nil.
	Tracer                    trace.Tracer
	Log                       log.Logger
	LogFactory                log.Factory
	VMManager                 vms.Manager // Manage mappings from vm ID --> vm
	BlockAcceptorGroup        nodeconsensus.AcceptorGroup
	TxAcceptorGroup           nodeconsensus.AcceptorGroup
	VertexAcceptorGroup       nodeconsensus.AcceptorGroup
	DB                        database.Database
	MsgCreator                message.OutboundMsgBuilder // message creator, shared with network
	Router                    ChainRouter                // Routes incoming messages to the appropriate chain
	Net                       network.Network            // Sends consensus messages to other validators
	Validators                validators.Manager         // Validators validating on this chain
	NodeID                    ids.NodeID                 // The ID of this node
	NetworkID                 uint32                     // ID of the network this node is connected to
	PartialSyncPrimaryNetwork bool
	Server                    server.Server // Handles HTTP API calls
	AtomicMemory              *atomic.Memory
	XAssetID                ids.ID
	SkipBootstrap             bool            // Skip bootstrapping and start processing immediately
	EnableAutomining          bool            // Enable automining in POA mode
	XChainID                  ids.ID          // ID of the X-Chain,
	CChainID                  ids.ID          // ID of the C-Chain,
	DChainID                  ids.ID          // ID of the D-Chain (DEX),
	CriticalChains            set.Set[ids.ID] // Chains that can't exit gracefully
	TimeoutManager            timeout.Manager // Manages request timeouts when sending messages to other validators
	Health                    health.Registerer
	NetConfigs             map[ids.ID]nets.Config // ID -> NetConfig
	ChainConfigs              map[string]ChainConfig    // alias -> ChainConfig
	// ShutdownNodeFunc allows the chain manager to issue a request to shutdown the node
	ShutdownNodeFunc func(exitCode int)
	MeterVMEnabled   bool // Should each VM be wrapped with a MeterVM

	Metrics        metric.MultiGatherer
	MeterDBMetrics metric.MultiGatherer

	FrontierPollFrequency   time.Duration
	ConsensusAppConcurrency int

	// Max Time to spend fetching a container and its
	// ancestors when responding to a GetAncestors
	BootstrapMaxTimeGetAncestors time.Duration
	// Max number of containers in an ancestors message sent by this node.
	BootstrapAncestorsMaxContainersSent int
	// This node will only consider the first [AncestorsMaxContainersReceived]
	// containers in an ancestors message it receives.
	BootstrapAncestorsMaxContainersReceived int

	Upgrades upgrade.Config

	// Tracks CPU/disk usage caused by each peer.
	ResourceTracker timetracker.ResourceTracker

	StateSyncBeacons []ids.NodeID

	ChainDataDir string

	Nets *Nets
}

type manager struct {
	// Note: The string representation of a chain's ID is also considered to be an alias of the chain
	// That is, [chainID].String() is an alias for the chain, too
	ids.Aliaser
	ManagerConfig

	// ChainDBManager handles per-chain database instances
	chainDBManager *ChainDBManager

	// Those notified when a chain is created
	registrants []Registrant

	// queue that holds chain create requests
	chainsQueue buffer.BlockingDeque[ChainParameters]
	// unblocks chain creator to start processing the queue
	unblockChainCreatorCh chan struct{}
	// shutdown the chain creator goroutine if the queue hasn't started to be
	// processed.
	chainCreatorShutdownCh chan struct{}
	chainCreatorExited     sync.WaitGroup

	// pendingVMChains tracks chains waiting for VMs to be loaded (for hot-loading).
	// Key: VM ID that the chain needs
	// Value: List of chain parameters waiting for this VM
	pendingVMChainsLock sync.RWMutex
	pendingVMChains     map[ids.ID][]ChainParameters

	chainsLock sync.Mutex
	// Key: Chain's ID
	// Value: The chain
	chains map[ids.ID]*chainInfo

	// chain++ related interface to allow validators retrieval
	validatorState validators.State

	luxGatherer          metric.MultiGatherer            // chainID
	handlerGatherer      metric.MultiGatherer            // chainID
	meterChainVMGatherer metric.MultiGatherer            // chainID
	meterGRAPHVMGatherer metric.MultiGatherer            // chainID
	proposervmGatherer   metric.MultiGatherer            // chainID
	p2pGatherer          metric.MultiGatherer            // chainID
	linearGatherer       metric.MultiGatherer            // chainID
	stakeGatherer        metric.MultiGatherer            // chainID
	vmGatherer           map[ids.ID]metric.MultiGatherer // vmID -> chainID
}

// New returns a new Manager
func New(config *ManagerConfig) (Manager, error) {
	luxGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(luxNamespace, luxGatherer); err != nil {
		return nil, err
	}

	handlerGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(handlerNamespace, handlerGatherer); err != nil {
		return nil, err
	}

	meterChainVMGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(meterchainvmNamespace, meterChainVMGatherer); err != nil {
		return nil, err
	}

	meterGRAPHVMGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(meterdagvmNamespace, meterGRAPHVMGatherer); err != nil {
		return nil, err
	}

	proposervmGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(proposervmNamespace, proposervmGatherer); err != nil {
		return nil, err
	}

	p2pGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(p2pNamespace, p2pGatherer); err != nil {
		return nil, err
	}

	consensusmanGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(chainNamespace, consensusmanGatherer); err != nil {
		return nil, err
	}

	stakeGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(stakeNamespace, stakeGatherer); err != nil {
		return nil, err
	}

	// Initialize chain database manager using single global BadgerDB with prefix isolation
	// All chains share one database - G-Chain (dgraph) can index the entire database for GraphQL queries
	chainDBManager := NewChainDBManager(ChainDBManagerConfig{
		DB:  config.DB,
		Log: config.Log,
	})

	return &manager{
		Aliaser:                ids.NewAliaser(),
		ManagerConfig:          *config,
		chainDBManager:         chainDBManager,
		chains:                 make(map[ids.ID]*chainInfo),
		chainsQueue:            buffer.NewUnboundedBlockingDeque[ChainParameters](initialQueueSize),
		unblockChainCreatorCh:  make(chan struct{}),
		chainCreatorShutdownCh: make(chan struct{}),
		pendingVMChains:        make(map[ids.ID][]ChainParameters),

		luxGatherer:          luxGatherer,
		handlerGatherer:      handlerGatherer,
		meterChainVMGatherer: meterChainVMGatherer,
		meterGRAPHVMGatherer: meterGRAPHVMGatherer,
		proposervmGatherer:   proposervmGatherer,
		p2pGatherer:          p2pGatherer,
		linearGatherer:       consensusmanGatherer,
		stakeGatherer:        stakeGatherer,
		vmGatherer:           make(map[ids.ID]metric.MultiGatherer),
	}, nil
}

// QueueChainCreation queues a chain creation request
// Invariant: Tracked Net must be checked before calling this function
func (m *manager) QueueChainCreation(chainParams ChainParameters) {
	// Check for chain ID mapping override for C-Chain
	m.Log.Info("QueueChainCreation called",
		log.String("vmID", chainParams.VMID.String()),
		log.String("EVMID", constants.EVMID.String()),
		log.Bool("vmIDEqualsEVMID", chainParams.VMID == constants.EVMID),
		log.String("envVar", os.Getenv("LUX_CHAIN_ID_MAPPING_C")),
	)

	if chainParams.VMID == constants.EVMID && os.Getenv("LUX_CHAIN_ID_MAPPING_C") != "" {
		mappedID := os.Getenv("LUX_CHAIN_ID_MAPPING_C")
		parsedID, err := ids.FromString(mappedID)
		if err == nil {
			m.Log.Info("Using mapped blockchain ID for C-Chain",
				log.String("original", chainParams.ID.String()),
				log.String("mapped", parsedID.String()),
			)
			chainParams.ID = parsedID
		} else {
			m.Log.Warn("Invalid chain ID mapping",
				log.String("mapping", mappedID),
				log.Err(err),
			)
		}
	}

	if sb, _ := m.Nets.GetOrCreate(chainParams.ChainID); !sb.AddChain(chainParams.ID) {
		m.Log.Debug("skipping chain creation",
			log.String("reason", "chain already staged"),
			log.Stringer("netID", chainParams.ChainID),
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
		)
		return
	}

	if ok := m.chainsQueue.PushRight(chainParams); !ok {
		m.Log.Warn("skipping chain creation",
			log.String("reason", "couldn't enqueue chain"),
			log.Stringer("netID", chainParams.ChainID),
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
		)
	}
}

// createChain creates and starts the chain
//
// Note: it is expected for the net to already have the chain registered as
// bootstrapping before this function is called
func (m *manager) createChain(chainParams ChainParameters) {
	m.Log.Info("creating chain",
		log.Stringer("netID", chainParams.ChainID),
		log.Stringer("chainID", chainParams.ID),
		log.Stringer("vmID", chainParams.VMID),
	)

	sb, _ := m.Nets.GetOrCreate(chainParams.ChainID)

	// Note: buildChain builds all chain's relevant objects (notably engine and handler)
	// but does not start their operations. Starting of the handler (which could potentially
	// issue some internal messages), is delayed until chain dispatching is started and
	// the chain is registered in the manager. This ensures that no message generated by handler
	// upon start is dropped.
	chain, err := m.buildChain(chainParams, sb)
	if chain == nil && err == nil { m.Log.Info("chain skipped", log.Stringer("chainID", chainParams.ID)); return }

	if err != nil {
		// Special handling for X-Chain in single validator mode
		// Allow the node to continue without X-Chain when it fails with VM type error
		// X-Chain ID: w68fJWq2nmQYuEKvbKRrKvDXB8xGnzuVGpoosXF3YV2N3G6nY
		xChainID, _ := ids.FromString("w68fJWq2nmQYuEKvbKRrKvDXB8xGnzuVGpoosXF3YV2N3G6nY")
		isXChain := chainParams.ID == xChainID
		isVMTypeError := err == errUnknownVMType
		skipBootstrapMode := m.SkipBootstrap

		// If X-Chain fails with VM type error in single validator mode, just log and continue
		if isXChain && isVMTypeError && skipBootstrapMode {
			chainAlias := m.PrimaryAliasOrDefault(chainParams.ID)
			m.Log.Warn("X-Chain creation failed in single validator mode - continuing without X-Chain",
				log.Stringer("netID", chainParams.ChainID),
				log.Stringer("chainID", chainParams.ID),
				log.String("chainAlias", chainAlias),
				log.Stringer("vmID", chainParams.VMID),
				log.String("errorString", fmt.Sprintf("%v", err)),
				log.Err(err),
			)

			// Register a health check that indicates X-Chain is not running
			healthCheckErr := fmt.Errorf("X-Chain not running in single validator mode: %w", err)
			err := m.Health.RegisterHealthCheck(
				chainAlias,
				health.CheckerFunc(func(context.Context) (interface{}, error) {
					return nil, healthCheckErr
				}),
				chainParams.ChainID.String(),
			)
			if err != nil {
				m.Log.Error("failed to register X-Chain health check",
					log.Stringer("chainID", chainParams.ID),
					log.String("chainAlias", chainAlias),
					log.Err(err),
				)
			}
			return
		}

		if m.CriticalChains.Contains(chainParams.ID) {
			// Shut down if we fail to create a required chain (i.e. X, P or C)
			// unless it's X-Chain with VM type error in single validator mode (handled above)
			m.Log.Error("error creating required chain",
				log.Stringer("netID", chainParams.ChainID),
				log.Stringer("chainID", chainParams.ID),
				log.Stringer("vmID", chainParams.VMID),
				log.String("errorString", fmt.Sprintf("%v", err)),
				log.String("errorType", fmt.Sprintf("%T", err)),
				log.Err(err),
			)
			go m.ShutdownNodeFunc(1)
			return
		}

		chainAlias := m.PrimaryAliasOrDefault(chainParams.ID)
		m.Log.Error("error creating chain",
			log.Stringer("netID", chainParams.ChainID),
			log.Stringer("chainID", chainParams.ID),
			log.String("chainAlias", chainAlias),
			log.Stringer("vmID", chainParams.VMID),
			log.Err(err),
		)

		// Register the health check for this chain regardless of if it was
		// created or not. This attempts to notify the node operator that their
		// node may not be properly validating the net they expect to be
		// validating.
		healthCheckErr := fmt.Errorf("failed to create chain on net %s: %w", chainParams.ChainID, err)
		err := m.Health.RegisterHealthCheck(
			chainAlias,
			health.CheckerFunc(func(context.Context) (interface{}, error) {
				return nil, healthCheckErr
			}),
			chainParams.ChainID.String(),
		)
		if err != nil {
			m.Log.Error("failed to register failing health check",
				log.Stringer("netID", chainParams.ChainID),
				log.Stringer("chainID", chainParams.ID),
				log.String("chainAlias", chainAlias),
				log.Stringer("vmID", chainParams.VMID),
				log.Err(err),
			)
		}
		return
	}

	m.chainsLock.Lock()
	m.chains[chainParams.ID] = chain
	m.chainsLock.Unlock()

	// Associate the newly created chain with its default alias
	if err := m.Alias(chainParams.ID, chainParams.ID.String()); err != nil {
		m.Log.Error("failed to alias the new chain with itself",
			log.Stringer("netID", chainParams.ChainID),
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
			log.Err(err),
		)
	}

	// Notify those who registered to be notified when a new chain is created
	m.notifyRegistrants(chain.Name, chain.Context, chain.VM)

	// Register HTTP handlers for this chain if the VM supports it
	if vm, ok := chain.VM.(interface {
		CreateHandlers(context.Context) (map[string]http.Handler, error)
	}); ok {
		handlers, err := vm.CreateHandlers(context.TODO())
		if err != nil {
			m.Log.Error("failed to create HTTP handlers",
				log.Stringer("chainID", chainParams.ID),
				log.Err(err),
			)
		} else {
			// Register each handler with the HTTP server
			for endpoint, handler := range handlers {
				chainAlias := chainParams.ID.String()
				// For C-Chain, also register under the "C" alias
				if chainParams.ID == m.CChainID {
					chainAlias = "C"
				}

				// The base is just "bc/<chainID>" and endpoint is "/rpc" or "/"
				chainBase := fmt.Sprintf("bc/%s", chainAlias)
				chainIDBase := fmt.Sprintf("bc/%s", chainParams.ID.String())

				// AddRoute will build the full path as /ext/<base><endpoint>
				m.Server.AddRoute(handler, chainBase, endpoint)
				if chainAlias != chainParams.ID.String() {
					m.Server.AddRoute(handler, chainIDBase, endpoint)
				}

				// Also register with chain name alias for user-friendly routing (e.g., /ext/bc/zoo/rpc)
				if chainParams.Name != "" {
					nameLower := strings.ToLower(chainParams.Name)
					nameBase := fmt.Sprintf("bc/%s", nameLower)
					m.Server.AddRoute(handler, nameBase, endpoint)
					m.Log.Info("Registered HTTP handler with chain name",
						log.String("chainName", nameLower),
						log.Stringer("chainID", chainParams.ID),
						log.String("base", nameBase),
						log.String("endpoint", endpoint),
					)

					// For C-Chain, also register under the "C" alias (uppercase)
					if strings.EqualFold(chainParams.Name, "C-Chain") {
						cBase := "bc/C"
						m.Server.AddRoute(handler, cBase, endpoint)
						m.Log.Info("Registered HTTP handler with C alias",
							log.Stringer("chainID", chainParams.ID),
							log.String("base", cBase),
							log.String("endpoint", endpoint),
						)
					}
				}

				m.Log.Info("Registered HTTP handler",
					log.String("chainAlias", chainAlias),
					log.Stringer("chainID", chainParams.ID),
					log.String("base", chainBase),
					log.String("endpoint", endpoint),
				)
			}
		}
	}

	// Register chain with the router for message routing
	if m.ManagerConfig.Router != nil {
		m.ManagerConfig.Router.AddChain(context.TODO(), chainParams.ID, chain.Handler)
	}

	// Register bootstrapped health checks after P chain has been added to
	// chains.
	//
	// Note: Registering this after the chain has been tracked prevents a race
	//       condition between the health check and adding the first chain to
	//       the manager.
	if chainParams.ID == constants.PlatformChainID {
		if err := m.registerBootstrappedHealthChecks(); err != nil {
			if chain.Engine != nil {
				chain.Engine.StopWithError(context.TODO(), err)
			}
		}
	}

	// Log prominent chain creation success message with endpoints
	vmName := constants.VMName(chainParams.VMID)
	chainAlias := m.PrimaryAliasOrDefault(chainParams.ID)
	m.Log.Info("╔══════════════════════════════════════════════════════════════════╗")
	m.Log.Info("║ CHAIN CREATED SUCCESSFULLY                                       ║",
		log.String("vmName", vmName),
		log.String("chainAlias", chainAlias),
	)
	m.Log.Info("║ Chain ID:", log.Stringer("chainID", chainParams.ID))
	m.Log.Info("║ VM ID:", log.Stringer("vmID", chainParams.VMID))
	m.Log.Info("║ Network ID:", log.Stringer("netID", chainParams.ChainID))
	m.Log.Info("║ Endpoints available at:")
	m.Log.Info("║   → /ext/bc/" + chainParams.ID.String())
	if chainAlias != chainParams.ID.String() {
		m.Log.Info("║   → /ext/bc/" + chainAlias)
	}
	m.Log.Info("╚══════════════════════════════════════════════════════════════════╝")

	// Tell the chain to start processing messages.
	// If the X, P, or C Chain panics, do not attempt to recover
	if chain.Engine != nil {
		chain.Engine.Start(context.TODO(), !m.CriticalChains.Contains(chainParams.ID))

		// Start a goroutine to monitor bootstrap completion and notify the subnet
		// This is required because the health check (m.Nets.Bootstrapping()) reports
		// subnets as not bootstrapped until sb.Bootstrapped(chainID) is called
		go m.monitorBootstrap(chain.Engine, sb, chainParams.ID)
	} else {
		// DAG chains (X-Chain, Q-Chain) manage their own consensus and don't have
		// a standard Engine. Mark them as bootstrapped immediately since the DAG
		// engine was already started in createDAG.
		m.Log.Info("DAG chain has no standard engine, marking as bootstrapped immediately",
			log.Stringer("chainID", chainParams.ID))
		sb.Bootstrapped(chainParams.ID)
	}
}

// Create a chain
func (m *manager) buildChain(chainParams ChainParameters, sb nets.Net) (*chainInfo, error) {
	if chainParams.ID != constants.PlatformChainID && chainParams.VMID == constants.PlatformVMID {
		return nil, errCreatePlatformVM
	}
	// primaryAlias will be used by the chains created below
	primaryAlias := m.PrimaryAliasOrDefault(chainParams.ID)

	// Create this chain's data directory
	chainDataDir := filepath.Join(m.ChainDataDir, chainParams.ID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}

	// Create the log and context of the chain
	chainLog := m.Log // Use main log instead of creating chain-specific log

	// Create metrics gatherer for this chain
	// The coreth EVM expects luxmetric.MultiGatherer, not *prometheus.Registry
	m.Log.Info("Creating metrics gatherer", log.String("primaryAlias", primaryAlias))
	chainMetricsGatherer := metric.NewMultiGatherer()

	// Create a registry and register it with the gatherer
	chainMetricsReg, err := metric.MakeAndRegister(chainMetricsGatherer, primaryAlias)
	if err != nil {
		return nil, fmt.Errorf("failed to create chain metrics: %w", err)
	}

	// Also register with the global gatherer for metrics collection
	if err := m.linearGatherer.Register(primaryAlias, chainMetricsReg); err != nil {
		m.Log.Warn("Failed to register chain metrics with global gatherer",
			log.String("primaryAlias", primaryAlias),
			log.Err(err),
		)
	}
	m.Log.Info("Metrics gatherer created",
		log.String("primaryAlias", primaryAlias),
		log.Bool("isNil", chainMetricsGatherer == nil),
	)

	// Note: Using local consensus package which has different fields
	// PublicKey needs to be []byte, not *bls.PublicKey
	var pubKeyBytes []byte
	if m.StakingBLSKey != nil && m.StakingBLSKey.PublicKey() != nil {
		// BLS PublicKey doesn't have a Bytes() method, so we'll leave it nil for now
		// This would need proper serialization in production
		pubKeyBytes = nil
	}

	// Create warp signer for this chain using the node's BLS key
	warpSigner := createWarpSigner(m.StakingBLSKey, m.NetworkID, chainParams.ID)

	chainCtx := &consensusctx.Context{
		NetworkID:    m.NetworkID,
		ChainID:      chainParams.ID,
		NodeID:       m.NodeID,
		PublicKey:    pubKeyBytes,

		XChainID:     m.XChainID,
		CChainID:     m.CChainID,
		XAssetID:     m.XAssetID,
		ChainDataDir: chainDataDir,

		BCLookup:        m,
		ValidatorState:  getValidatorState(m.validatorState),
		Metrics:         chainMetricsGatherer,
		Log:             chainLog,
		WarpSigner:      warpSigner,
		NetworkUpgrades: m.Upgrades,
	}

	// Get a factory for the vm we want to use on our chain
	m.Log.Info("Getting VM factory", log.Stringer("vmID", chainParams.VMID))
	vmFactory, err := m.VMManager.GetFactory(chainParams.VMID)
	if err != nil {
		// Check if this is a VM not found error - if so, add to pending chains for hot-loading
		if errors.Is(err, vms.ErrNotFound) {
			m.pendingVMChainsLock.Lock()
			m.pendingVMChains[chainParams.VMID] = append(m.pendingVMChains[chainParams.VMID], chainParams)
			m.pendingVMChainsLock.Unlock()
			m.Log.Warn("VM not found - chain queued for hot-loading",
				log.Stringer("vmID", chainParams.VMID),
				log.Stringer("chainID", chainParams.ID),
			)
			return nil, fmt.Errorf("VM %s not found (chain queued for hot-loading): %w", chainParams.VMID, err)
		}
		m.Log.Error("Failed to get VM factory", log.Stringer("vmID", chainParams.VMID), log.Err(err))
		return nil, fmt.Errorf("error while getting vmFactory: %w", err)
	}
	m.Log.Info("Got VM factory successfully")

	// Create the chain
	vm, err := vmFactory.New(chainLog)
	if err != nil {
		return nil, fmt.Errorf("error while creating vm for chain %s: %w", chainParams.ID, err)
	}

	chainFxs := make([]*engine.Fx, len(chainParams.FxIDs))
	for i, fxID := range chainParams.FxIDs {
		fxFactory, ok := fxs[fxID]
		if !ok {
			return nil, fmt.Errorf("fx %s not found", fxID)
		}

		chainFxs[i] = &engine.Fx{
			ID: fxID,
			Fx: fxFactory.New(),
		}
	}

	m.Log.Info("DEBUG: About to check VM type", log.Stringer("chainID", chainParams.ID), log.String("vmType", fmt.Sprintf("%T", vm)))
	var chain *chainInfo
	switch vm := vm.(type) {
	// DAG VM support - for X-Chain and Q-Chain
	case interface{ GetEngine() consensusdag.Engine }:
		m.Log.Info("detected DAG VM with GetEngine()",
			log.Stringer("chainID", chainParams.ID),
		)
		chain, err = m.createDAG(chainCtx, chainParams, vm, chainFxs)
		if err != nil {
			return nil, fmt.Errorf("error creating DAG chain: %w", err)
		}
	case block.ChainVM:
		beacons := m.Validators
		if chainParams.ID == constants.PlatformChainID {
			beacons = chainParams.CustomBeacons
		}

		// In skip-bootstrap mode, use empty beacons for all chains
		// This enables single-node development mode
		if m.SkipBootstrap {
			beacons = &emptyValidatorManager{}
			m.Log.Info("skip-bootstrap enabled - using empty beacons for single-node mode")
		}
		// Note: For linear chains, the consensus engine uses networkGossiper
		// which samples validators from m.Net (network's validator manager).
		// The validator manager (n.vdrs) is populated by PlatformVM during
		// its initialization via state.initValidatorSets(). Beacons are not
		// directly used here but are available for future beacon-based bootstrap.
		_ = beacons

		// Create simple linear chain with basic consensus engine
		m.Log.Info("creating linear chain", log.Stringer("chainID", chainCtx.ChainID))
		
		// Initialize the VM before creating the chain
		// Get chain configuration
		chainConfig, err := m.getChainConfig(chainParams.ID)
		if err != nil {
			m.Log.Warn("failed to get chain config, using empty config",
				log.Stringer("chainID", chainParams.ID),
				log.Err(err))
			chainConfig = ChainConfig{}
		}

		// Get chain alias for database directory naming
		linearChainAlias := chainParams.ID.String()
		if aliases, _ := m.Aliases(chainParams.ID); len(aliases) > 0 {
			linearChainAlias = aliases[0] // Use first alias (e.g., "P", "C")
		}

		// Get VM database from chain database manager
		// Get VM database from chain database manager
		vmDB, err := m.chainDBManager.GetVMDatabase(chainParams.ID, linearChainAlias)
		if err != nil {
			return nil, fmt.Errorf("failed to get database for chain %s: %w", chainParams.ID, err)
		}

		// Create message channel for VM-to-Engine communication
		toEngine := make(chan block.Message, 1)

		// Convert []*engine.Fx to []interface{}
		fxsInterface := make([]interface{}, len(chainFxs))
		for i, fx := range chainFxs {
			fxsInterface[i] = fx
		}

		// Initialize the VM if it supports the Initialize interface
		// Inject automining config for dev mode (applies to C-Chain/coreth)
		vmConfigBytes := m.injectAutominingConfig(chainConfig.Config)
		m.Log.Info("initializing VM", log.Stringer("chainID", chainParams.ID))
		err = vm.Initialize(
			context.TODO(),
			chainCtx,
			vmDB,
			chainParams.GenesisData,
			chainConfig.Upgrade,
			vmConfigBytes,
			toEngine,
			fxsInterface,
			nil, // appSender - not needed for simple VMs
		)
		if err != nil {
			m.Log.Error("VM initialization failed",
				log.Stringer("chainID", chainParams.ID),
				log.Err(err))
			return nil, fmt.Errorf("failed to initialize VM: %w", err)
		}
		m.Log.Info("VM initialized successfully", log.Stringer("chainID", chainParams.ID))

		// Transition VM to normal operation after initialization
		// For genesis-based networks with pre-configured validators, this is required
		// to make the VM APIs available immediately
		if stateVM, ok := vm.(interface {
			SetState(context.Context, uint32) error
		}); ok {
			m.Log.Info("transitioning VM to normal operation",
				log.Stringer("chainID", chainParams.ID))
			if err := stateVM.SetState(context.TODO(), uint32(consensus.Ready)); err != nil {
				m.Log.Error("failed to transition VM to normal operation",
					log.Stringer("chainID", chainParams.ID),
					log.Err(err))
				return nil, fmt.Errorf("failed to transition VM to normal operation: %w", err)
			}
		}

		// Create integrated consensus engine - the ONE right way to set up chain consensus
		// This consolidates: engine creation, emitter wiring, VM registration
		var blockBuilder consensuschain.BlockBuilder
		if bb, ok := vm.(consensuschain.BlockBuilder); ok {
			blockBuilder = bb
			m.Log.Info("registered VM with consensus engine for block building",
				log.Stringer("chainID", chainParams.ID))
		} else {
			m.Log.Warn("VM does not implement BlockBuilder interface, block building disabled",
				log.Stringer("chainID", chainParams.ID))
		}

		// For native/primary network chains (P/C/X/Q/A/B/T/Z etc.), use PrimaryNetworkID for validator lookups.
		// Native chains all have IDs with first 31 bytes zero, last byte is the chain letter (e.g., 'P', 'C').
		// Validators are registered under constants.PrimaryNetworkID (ids.Empty), not individual chain IDs.
		// For L1/subnet chains, use the subnet's validator set ID (chainParams.ChainID).
		networkID := chainParams.ChainID
		isNative := ids.IsNativeChain(chainParams.ID)
		if isNative {
			// Native chains (P, C, X, Q, A, B, T, Z, G, I, K) use PrimaryNetworkID for validator lookups
			networkID = constants.PrimaryNetworkID
		}
		m.Log.Info("[CONSENSUS DEBUG] Creating consensus engine for chain",
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("chainParams.ChainID", chainParams.ChainID),
			log.Bool("isNativeChain", isNative),
			log.Stringer("networkIDForValidators", networkID),
			log.Stringer("PrimaryNetworkID", constants.PrimaryNetworkID),
		)

		consensusEngine := consensuschain.NewIntegratedEngine(consensuschain.NetworkConfig{
			ChainID:   chainParams.ID,
			NetworkID: networkID,
			Logger:    m.Log,
			Gossiper:  &networkGossiper{net: m.Net, msgCreator: m.MsgCreator},
			VM:        blockBuilder,
		})

		// Start the consensus engine
		if err := consensusEngine.Start(context.TODO(), true); err != nil {
			m.Log.Error("failed to start consensus engine",
				log.Stringer("chainID", chainParams.ID),
				log.Err(err))
			return nil, fmt.Errorf("failed to start consensus engine: %w", err)
		}
		m.Log.Info("consensus engine started with Lux consensus (Photon → Wave → Focus)",
			log.Stringer("chainID", chainParams.ID))

		// Forward VM notifications to consensus (single goroutine)
		go consensusEngine.ForwardVMNotifications(toEngine)

		chain = &chainInfo{
			Name:    chainCtx.ChainID.String(),
			Context: chainCtx,
			VM:      vm, // Use the real VM directly
			Engine:  consensusEngine, // Use real consensus engine directly
			Handler: newBlockHandler(vm, m.Log, consensusEngine, m.Net, m.MsgCreator, chainParams.ID),
		}
	default:
		return nil, fmt.Errorf("unsupported VM type: %T", vm)
	}

	vmGatherer, err := m.getOrMakeVMGatherer(chainParams.VMID)
	if err != nil {
		return nil, err
	}
	_ = vmGatherer

	return chain, nil
}

func (m *manager) AddRegistrant(r Registrant) {
	m.registrants = append(m.registrants, r)
}

// dagVMAdapter adapts a DAG VM to consensus.VM for HTTP handler registration
type dagVMAdapter struct {
	underlying interface{}
}

func (v *dagVMAdapter) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	if h, ok := v.underlying.(interface {
		CreateHandlers(context.Context) (map[string]http.Handler, error)
	}); ok {
		return h.CreateHandlers(ctx)
	}
	return map[string]http.Handler{}, nil
}

func (v *dagVMAdapter) CreateStaticHandlers(ctx context.Context) (map[string]http.Handler, error) {
	if h, ok := v.underlying.(interface {
		CreateStaticHandlers(context.Context) (map[string]http.Handler, error)
	}); ok {
		return h.CreateStaticHandlers(ctx)
	}
	return map[string]http.Handler{}, nil
}

func (v *dagVMAdapter) HealthCheck(ctx context.Context) (interface{}, error) {
	return map[string]interface{}{"healthy": true}, nil
}

func (v *dagVMAdapter) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	return nil, nil
}

func (v *dagVMAdapter) SetState(ctx context.Context, state consensus.State) error {
	if s, ok := v.underlying.(interface {
		SetState(context.Context, uint32) error
	}); ok {
		return s.SetState(ctx, uint32(state))
	}
	return nil
}

func (v *dagVMAdapter) Shutdown(ctx context.Context) error {
	if s, ok := v.underlying.(interface {
		Shutdown(context.Context) error
	}); ok {
		return s.Shutdown(ctx)
	}
	return nil
}

func (v *dagVMAdapter) Version(ctx context.Context) (string, error) {
	return "1.0.0", nil
}

func (v *dagVMAdapter) Initialize(
	ctx context.Context,
	chainCtx *consensusctx.Context,
	dbMgr dbmanager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- engine.Message,
	fxs []*engine.Fx,
	appSender interface{},
) error {
	return nil // DAG VMs are pre-initialized
}

// createDAG creates a DAG chain (X-Chain, Q-Chain) using the VM's DAG engine
func (m *manager) createDAG(
	ctx *consensusctx.Context,
	chainParams ChainParameters,
	vm interface{},
	fxs []*engine.Fx,
) (*chainInfo, error) {
	// Type assert to get GetEngine() method from exchangevm/qvm
	dagVM, ok := vm.(interface{ GetEngine() consensusdag.Engine })
	if !ok {
		return nil, fmt.Errorf("VM does not implement GetEngine() for DAG consensus")
	}

	m.Log.Info("creating DAG chain",
		log.Stringer("chainID", chainParams.ID),
		log.String("vmID", chainParams.VMID.String()),
	)

	// Get chain configuration
	chainConfig, err := m.getChainConfig(chainParams.ID)
	if err != nil {
		m.Log.Warn("failed to get chain config, using empty config",
			log.Stringer("chainID", chainParams.ID),
			log.Err(err))
		chainConfig = ChainConfig{}
	}

	// Inject automining config for dev mode (applies to C-Chain/coreth)
	chainConfig.Config = m.injectAutominingConfig(chainConfig.Config)

	// Get chain alias for database directory naming
	chainAlias := chainParams.ID.String()
	if aliases, _ := m.Aliases(chainParams.ID); len(aliases) > 0 {
		chainAlias = aliases[0] // Use first alias (e.g., "X", "Q")
	}

	// Get VM database from chain database manager
	// In isolated mode, each chain gets its own BadgerDB
	// In legacy mode, uses prefixdb on shared database
	vmDB, err := m.chainDBManager.GetVMDatabase(chainParams.ID, chainAlias)
	if err != nil {
		return nil, fmt.Errorf("failed to get database for chain %s: %w", chainParams.ID, err)
	}

	// Create a proper context for VM initialization with cancellation support
	// This replaces context.TODO() which the user flagged as confusing and error-prone
	initCtx, cancelInit := context.WithCancel(context.Background())
	defer cancelInit() // Ensure cleanup on function exit

	// Initialize VM if it supports Initialize
	// Try multiple Initialize signatures since VMs may have different interfaces
	vmInitialized := false

	// Try QVM Initialize signature (uses consensus/core types)
	if initVM, ok := vm.(interface {
		Initialize(
			ctx context.Context,
			chainCtx interface{},
			db database.Database,
			genesisBytes []byte,
			upgradeBytes []byte,
			configBytes []byte,
			toEngine chan<- engine.Message,
			fxs []*engine.Fx,
			appSender warp.Sender,
		) error
	}); ok {
		toEngine := make(chan engine.Message, 1)
		err := initVM.Initialize(
			initCtx,
			ctx,
			vmDB,
			chainParams.GenesisData,
			chainConfig.Upgrade,
			chainConfig.Config,
			toEngine,
			fxs,
			&noopWarpSender{}, // Simple no-op for non-warp VMs
		)
		if err != nil {
			m.Log.Warn("QVM-style initialization failed", log.Stringer("chainID", chainParams.ID), log.Err(err))
		} else {
			m.Log.Info("QVM initialized successfully", log.Stringer("chainID", chainParams.ID))
			vmInitialized = true
		}
	}

	// Try ExchangeVM Initialize signature (uses interface{} types for flexibility)
	if !vmInitialized {
		if initVM, ok := vm.(interface {
			Initialize(
				ctx context.Context,
				chainCtx interface{},
				dbManager interface{},
				genesisBytes []byte,
				upgradeBytes []byte,
				configBytes []byte,
				toEngine chan<- interface{},
				fxs []interface{},
				appSender interface{},
			) error
		}); ok {
			toEngine := make(chan interface{}, 1)
			// Convert fxs to []interface{}
			fxsInterface := make([]interface{}, len(fxs))
			for i, fx := range fxs {
				fxsInterface[i] = fx
			}
			err := initVM.Initialize(
				initCtx,
				ctx,
				vmDB,
				chainParams.GenesisData,
				chainConfig.Upgrade,
				chainConfig.Config,
				toEngine,
				fxsInterface,
				&noopWarpSender{}, // Implements AppSender interface
			)
			if err != nil {
				m.Log.Warn("ExchangeVM-style initialization failed", log.Stringer("chainID", chainParams.ID), log.Err(err))
			} else {
				m.Log.Info("ExchangeVM initialized successfully", log.Stringer("chainID", chainParams.ID))
				vmInitialized = true
			}
		}
	}

	// Only transition VM to normal operation if initialization succeeded
	if vmInitialized {
		if stateVM, ok := vm.(interface {
			SetState(context.Context, uint32) error
		}); ok {
			if err := stateVM.SetState(initCtx, uint32(consensus.Ready)); err != nil {
				m.Log.Warn("failed to transition VM to normal op", log.Stringer("chainID", chainParams.ID), log.Err(err))
			}
		}
	}

	// Get and start the DAG engine
	dagEngine := dagVM.GetEngine()
	if starter, ok := dagEngine.(interface{ Start(context.Context, uint32) error }); ok {
		if err := starter.Start(context.Background(), 0); err != nil {
			return nil, fmt.Errorf("failed to start DAG engine: %w", err)
		}
	}

	m.Log.Info("DAG chain created successfully",
		log.Stringer("chainID", chainParams.ID),
		log.String("status", "using native DAG consensus"),
	)

	return &chainInfo{
		Name:    chainParams.ID.String(),
		Context: ctx,
		VM:      &dagVMAdapter{underlying: vm},
		Handler: &placeholderHandler{},
	}, nil
}

// errBootstrapTimeout is returned when a chain fails to bootstrap within the timeout period
var errBootstrapTimeout = errors.New("chain failed to bootstrap within timeout")

// monitorBootstrap monitors when a chain finishes bootstrapping and notifies the subnet.
// This is critical for health checks because the health check queries m.Nets.Bootstrapping()
// which returns subnets that have chains still in bootstrapping state. Without this notification,
// the health check would permanently report "subnets not bootstrapped".
//
// IMPORTANT: If bootstrap times out, the chain is NOT marked as bootstrapped. This ensures
// real bootstrap failures are surfaced rather than masked by forcing a "ready" state.
func (m *manager) monitorBootstrap(engine Engine, sb nets.Net, chainID ids.ID) {
	// Check if the engine supports IsBootstrapped
	type bootstrapChecker interface {
		IsBootstrapped() bool
	}
	checker, ok := engine.(bootstrapChecker)
	if !ok {
		// Engine doesn't support IsBootstrapped, immediately mark as bootstrapped
		// This is safe because if we can't check, we assume the chain is ready
		m.Log.Info("engine does not support IsBootstrapped, marking chain as bootstrapped",
			log.Stringer("chainID", chainID))
		sb.Bootstrapped(chainID)
		return
	}

	// Poll the engine until it reports bootstrapped
	// Use a short initial delay to let the engine start up, then poll regularly
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Set a reasonable timeout (5 minutes for local networks)
	// After timeout, we do NOT mark as bootstrapped - this is a real failure
	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	// Track polling count for diagnostics
	pollCount := 0

	for {
		select {
		case <-ticker.C:
			pollCount++
			if checker.IsBootstrapped() {
				m.Log.Info("chain finished bootstrapping, notifying subnet",
					log.Stringer("chainID", chainID),
					log.Int("pollCount", pollCount))
				sb.Bootstrapped(chainID)
				return
			}
		case <-timeout.C:
			// Timeout reached - this is a real bootstrap failure
			// DO NOT mark as bootstrapped - this masks real failures and causes unpredictable behavior
			m.Log.Error("chain bootstrap timeout - chain NOT marked as bootstrapped",
				log.Stringer("chainID", chainID),
				log.Int("pollCount", pollCount),
				log.String("lastState", "still bootstrapping after 5 minutes"),
				log.Err(errBootstrapTimeout))

			// Stop the engine with the bootstrap timeout error
			// This ensures the chain is properly marked as failed
			if err := engine.StopWithError(context.Background(), errBootstrapTimeout); err != nil {
				m.Log.Error("failed to stop engine after bootstrap timeout",
					log.Stringer("chainID", chainID),
					log.Err(err))
			}

			// Register a health check that reports the bootstrap failure
			chainAlias := m.PrimaryAliasOrDefault(chainID)
			healthErr := m.Health.RegisterHealthCheck(
				chainAlias+"-bootstrap",
				health.CheckerFunc(func(context.Context) (interface{}, error) {
					return map[string]interface{}{
						"chainID":   chainID.String(),
						"error":     "bootstrap timeout",
						"pollCount": pollCount,
					}, errBootstrapTimeout
				}),
				health.ApplicationTag,
			)
			if healthErr != nil {
				m.Log.Error("failed to register bootstrap timeout health check",
					log.Stringer("chainID", chainID),
					log.Err(healthErr))
			}
			return
		case <-m.chainCreatorShutdownCh:
			// Manager is shutting down
			return
		}
	}
}

func (m *manager) IsBootstrapped(id ids.ID) bool {
	m.chainsLock.Lock()
	_, exists := m.chains[id]
	m.chainsLock.Unlock()
	if !exists {
		return false
	}

	// For now, assume bootstrapped chains are in NormalOp
	return true // chain.Context.State.Get() == consensus.NormalOp
}

func (m *manager) registerBootstrappedHealthChecks() error {
	bootstrappedCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		if netIDs := m.Nets.Bootstrapping(); len(netIDs) != 0 {
			return netIDs, errNotBootstrapped
		}
		return []ids.ID{}, nil
	})
	if err := m.Health.RegisterReadinessCheck("bootstrapped", bootstrappedCheck, health.ApplicationTag); err != nil {
		return fmt.Errorf("couldn't register bootstrapped readiness check: %w", err)
	}
	if err := m.Health.RegisterHealthCheck("bootstrapped", bootstrappedCheck, health.ApplicationTag); err != nil {
		return fmt.Errorf("couldn't register bootstrapped health check: %w", err)
	}

	// We should only report unhealthy if the node is partially syncing the
	// primary network and is a validator.
	if !m.PartialSyncPrimaryNetwork {
		return nil
	}

	partialSyncCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		// Note: The health check is skipped during bootstrapping to allow a
		// node to sync the network even if it was previously a validator.
		if !m.IsBootstrapped(constants.PlatformChainID) {
			return "node is currently bootstrapping", nil
		}
		if _, ok := m.Validators.GetValidator(constants.PrimaryNetworkID, m.NodeID); !ok {
			return "node is not a primary network validator", nil
		}

		m.Log.Warn("node is a primary network validator",
			log.Err(errPartialSyncAsAValidator),
		)
		return "node is a primary network validator", errPartialSyncAsAValidator
	})

	if err := m.Health.RegisterHealthCheck("validation", partialSyncCheck, health.ApplicationTag); err != nil {
		return fmt.Errorf("couldn't register validation health check: %w", err)
	}
	return nil
}

// Starts chain creation loop to process queued chains
func (m *manager) StartChainCreator(platformParams ChainParameters) error {
	// Add the P-Chain to the Primary Network
	sb, _ := m.Nets.GetOrCreate(constants.PrimaryNetworkID)
	sb.AddChain(platformParams.ID)

	// The P-chain is created synchronously to ensure that `VM.Initialize` has
	// finished before returning from this function. This is required because
	// the P-chain initializes state that the rest of the node initialization
	// depends on.
	m.createChain(platformParams)

	m.Log.Info("starting chain creator")
	m.chainCreatorExited.Add(1)
	go func() { close(m.unblockChainCreatorCh) }()
	go m.dispatchChainCreator()
	return nil
}

func (m *manager) dispatchChainCreator() {
	defer m.chainCreatorExited.Done()

	select {
	// This channel will be closed when Shutdown is called on the manager.
	case <-m.chainCreatorShutdownCh:
		return
	case <-m.unblockChainCreatorCh:
	}

	// Handle chain creations
	for {
		// Get the next chain we should create.
		// Dequeue waits until an element is pushed, so this is not
		// busy-looping.
		chainParams, ok := m.chainsQueue.PopLeft()
		if !ok { // queue is closed, return directly
			return
		}
		m.createChain(chainParams)
	}
}

// PrimaryAliasOrDefault returns the primary alias for a chain, or the chain ID if no alias exists
func (m *manager) PrimaryAliasOrDefault(chainID ids.ID) string {
	alias, err := m.PrimaryAlias(chainID)
	if err != nil {
		// Return chain ID as string if no alias found
		return chainID.String()
	}
	return alias
}

// Shutdown stops all the chains
func (m *manager) Shutdown() {
	m.Log.Info("shutting down chain manager")
	m.chainsQueue.Close()
	close(m.chainCreatorShutdownCh)
	m.chainCreatorExited.Wait()
	// Router doesn't have Shutdown method in consensus package
}

// LookupVM returns the ID of the VM associated with an alias
func (m *manager) LookupVM(alias string) (ids.ID, error) {
	return m.VMManager.Lookup(alias)
}

// RetryPendingChains re-queues chains that were waiting for the specified VM.
// This is called when a VM is hot-loaded via admin.loadVMs.
// Returns the number of chains that were re-queued.
func (m *manager) RetryPendingChains(vmID ids.ID) int {
	m.pendingVMChainsLock.Lock()
	pendingChains, ok := m.pendingVMChains[vmID]
	if ok {
		delete(m.pendingVMChains, vmID)
	}
	m.pendingVMChainsLock.Unlock()

	if !ok || len(pendingChains) == 0 {
		return 0
	}

	// Re-queue all pending chains for this VM
	for _, chainParams := range pendingChains {
		m.Log.Info("Re-queuing chain after VM hot-load",
			log.Stringer("vmID", vmID),
			log.Stringer("chainID", chainParams.ID),
		)
		m.chainsQueue.PushRight(chainParams)
	}

	return len(pendingChains)
}

// GetPendingChains returns the chain parameters waiting for a VM to be loaded.
func (m *manager) GetPendingChains(vmID ids.ID) []ChainParameters {
	m.pendingVMChainsLock.RLock()
	defer m.pendingVMChainsLock.RUnlock()

	pendingChains, ok := m.pendingVMChains[vmID]
	if !ok {
		return nil
	}

	// Return a copy to avoid race conditions
	result := make([]ChainParameters, len(pendingChains))
	copy(result, pendingChains)
	return result
}

// Notify registrants [those who want to know about the creation of chains]
// that the specified chain has been created
func (m *manager) notifyRegistrants(name string, ctx *consensusctx.Context, vm interface{}) {
	for _, registrant := range m.registrants {
		if coreVM, ok := vm.(interfaces.VM); ok {
			registrant.RegisterChain(name, ctx, coreVM)
		}
	}
}

// getChainConfig returns value of a entry by looking at ID key and alias key
// it first searches ID key, then falls back to it's corresponding primary alias
func (m *manager) getChainConfig(id ids.ID) (ChainConfig, error) {
	if val, ok := m.ManagerConfig.ChainConfigs[id.String()]; ok {
		return val, nil
	}
	aliases, err := m.Aliases(id)
	if err != nil {
		return ChainConfig{}, err
	}
	for _, alias := range aliases {
		if val, ok := m.ManagerConfig.ChainConfigs[alias]; ok {
			return val, nil
		}
	}

	return ChainConfig{}, nil
}

// injectAutominingConfig modifies the config bytes to include enable-automining flag
// when dev mode automining is enabled. This is used for C-Chain (coreth) to enable
// anvil-like block production behavior.
func (m *manager) injectAutominingConfig(configBytes []byte) []byte {
	if !m.EnableAutomining {
		return configBytes
	}

	// Parse existing config or create empty object
	var config map[string]interface{}
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &config); err != nil {
			// If we can't parse existing config, create new one with just automining
			m.Log.Warn("failed to parse chain config for automining injection, creating new config",
				log.Err(err))
			config = make(map[string]interface{})
		}
	} else {
		config = make(map[string]interface{})
	}

	// Inject enable-automining flag
	config["enable-automining"] = true
	// Inject skip-block-fee flag to allow block generation without requiring transaction fees
	// This is necessary for dev mode APIs (eth_setBalance, eth_setStorageAt, evm_mine, etc.)
	config["skip-block-fee"] = true

	// Serialize back to JSON
	modifiedBytes, err := json.Marshal(config)
	if err != nil {
		m.Log.Warn("failed to marshal modified chain config", log.Err(err))
		return configBytes
	}

	m.Log.Info("injected enable-automining and skip-block-fee into chain config")
	return modifiedBytes
}

func (m *manager) getOrMakeVMGatherer(vmID ids.ID) (metrics.MultiGatherer, error) {
	vmGatherer, ok := m.vmGatherer[vmID]
	if ok {
		return vmGatherer, nil
	}

	vmName := constants.VMName(vmID)
	// metric.AppendNamespace doesn't exist in current metric package
	vmNamespace := vmName // Simplified - just use vmName directly
	vmGatherer = metrics.NewLabelGatherer(ChainLabel)
	err := m.Metrics.Register(
		vmNamespace,
		vmGatherer,
	)
	if err != nil {
		return nil, err
	}
	m.vmGatherer[vmID] = vmGatherer
	return vmGatherer, nil
}

// emptyValidatorManager implements validators.Manager with no validators
type emptyValidatorManager struct{}

func (e *emptyValidatorManager) GetValidator(netID ids.ID, nodeID ids.NodeID) (*validators.GetValidatorOutput, bool) {
	return nil, false
}

func (e *emptyValidatorManager) GetValidators(netID ids.ID) (validators.Set, error) {
	// Return nil for empty validator set since NewEmpty doesn't exist
	return nil, nil
}

func (e *emptyValidatorManager) GetWeight(netID ids.ID, nodeID ids.NodeID) uint64 {
	return 0
}

func (e *emptyValidatorManager) GetCurrentHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
}

func (e *emptyValidatorManager) GetNetIDHeight(ctx context.Context, netID ids.ID) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) OnAcceptedBlockID(blkID ids.ID) {}

func (e *emptyValidatorManager) String() string {
	return "empty validator manager"
}

func (e *emptyValidatorManager) TotalWeight(netID ids.ID) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) GetLight(netID ids.ID, nodeID ids.NodeID) uint64 {
	return 0
}

func (e *emptyValidatorManager) TotalLight(netID ids.ID) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) AddStaker(netID ids.ID, nodeID ids.NodeID, publicKey []byte, txID ids.ID, light uint64) error {
	return nil
}

func (e *emptyValidatorManager) AddWeight(netID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return nil
}

func (e *emptyValidatorManager) RemoveWeight(netID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return nil
}

func (e *emptyValidatorManager) GetMap(netID ids.ID) map[ids.NodeID]*validators.GetValidatorOutput {
	return nil
}

func (e *emptyValidatorManager) GetValidatorIDs(netID ids.ID) []ids.NodeID {
	return nil
}

func (e *emptyValidatorManager) NumValidators(netID ids.ID) int {
	return 0
}

func (e *emptyValidatorManager) NumNets() int {
	return 0
}

func (e *emptyValidatorManager) SubsetWeight(netID ids.ID, nodeIDs set.Set[ids.NodeID]) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) Sample(netID ids.ID, size int) ([]ids.NodeID, error) {
	return nil, nil
}

func (e *emptyValidatorManager) Count(netID ids.ID) int {
	return 0
}

func (e *emptyValidatorManager) RegisterCallbackListener(listener validators.ManagerCallbackListener) {
}

func (e *emptyValidatorManager) RegisterSetCallbackListener(netID ids.ID, listener validators.SetCallbackListener) {
}

func (e *emptyValidatorManager) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return nil, nil
}

// blockHandler implements handler.Handler interface and processes incoming blocks
// This enables block propagation between validators
type blockHandler struct {
	vm         block.ChainVM
	logger     log.Logger
	engine     *consensuschain.IntegratedEngine // Consensus engine for proper block handling
	net        network.Network                  // Network for sending Chits responses
	msgCreator message.OutboundMsgBuilder       // Message creator for Chits responses
	chainID    ids.ID                           // Chain ID for message routing
}

func newBlockHandler(vm block.ChainVM, logger log.Logger, engine *consensuschain.IntegratedEngine, net network.Network, msgCreator message.OutboundMsgBuilder, chainID ids.ID) *blockHandler {
	return &blockHandler{
		vm:         vm,
		logger:     logger,
		engine:     engine,
		net:        net,
		msgCreator: msgCreator,
		chainID:    chainID,
	}
}

func (b *blockHandler) Context() *consensusctx.Context                 { return nil }
func (b *blockHandler) Start(ctx context.Context, startReqID uint32)  {}
func (b *blockHandler) Push(ctx context.Context, msg handler.Message) {}
func (b *blockHandler) Len() int                                      { return 0 }
func (b *blockHandler) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (b *blockHandler) GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
	return nil
}
func (b *blockHandler) GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time) error {
	return nil
}
func (b *blockHandler) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerIDs []ids.ID) error {
	return nil
}
func (b *blockHandler) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	// Route incoming block through consensus engine instead of auto-accepting
	// This enables proper quorum-based block acceptance
	if b.engine == nil {
		b.logger.Warn("no consensus engine - cannot process incoming block",
			log.Stringer("from", nodeID))
		return nil
	}

	// Use the consensus engine to handle the block properly:
	// 1. Parse and verify the block
	// 2. Add to pending blocks
	// 3. Vote on the block
	// 4. Accept only when quorum is reached
	blk, err := b.engine.HandleIncomingBlock(ctx, container, nodeID)
	if err != nil {
		b.logger.Debug("failed to handle incoming block",
			log.Stringer("from", nodeID),
			log.Err(err))
		return nil
	}

	if blk != nil {
		b.logger.Info("processed incoming block through consensus",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", blk.ID()),
			log.Uint64("height", blk.Height()))
	}

	return nil
}
func (b *blockHandler) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, container []byte) error {
	// Handle PushQuery the same as Put - process the block
	return b.Put(ctx, nodeID, requestID, container)
}
func (b *blockHandler) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
	// PullQuery requests a vote on a block identified by containerID
	// We need to respond with Chits containing our preferred vote

	if b.net == nil || b.msgCreator == nil {
		b.logger.Debug("cannot respond to PullQuery - no network sender",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", containerID))
		return nil
	}

	// Try to get the block from the VM
	blk, err := b.vm.GetBlock(ctx, containerID)
	if err != nil {
		b.logger.Debug("cannot respond to PullQuery - block not found",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", containerID),
			log.Err(err))
		return nil
	}

	// Verify the block before voting for it
	if err := blk.Verify(ctx); err != nil {
		b.logger.Debug("cannot respond to PullQuery - block verification failed",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", containerID),
			log.Err(err))
		return nil
	}

	// Get the accepted block ID (last accepted) for the acceptedID field
	acceptedBlkID := containerID
	acceptedHeight := blk.Height()

	// Try to get the last accepted block for more accurate acceptedID
	if lastAccepted, err := b.vm.LastAccepted(ctx); err == nil && lastAccepted != ids.Empty {
		acceptedBlkID = lastAccepted
		if acceptedBlk, err := b.vm.GetBlock(ctx, lastAccepted); err == nil {
			acceptedHeight = acceptedBlk.Height()
		}
	}

	// Create Chits message
	// preferredID: the block we're voting for
	// preferredIDAtHeight: same as preferredID for now (could be optimized)
	// acceptedID: the last accepted block
	// acceptedHeight: height of the accepted block
	chitsMsg, err := b.msgCreator.Chits(b.chainID, requestID, containerID, containerID, acceptedBlkID, acceptedHeight)
	if err != nil {
		b.logger.Error("failed to create Chits message",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", containerID),
			log.Err(err))
		return nil
	}

	// Send Chits response to the requesting node
	nodeSet := set.NewSet[ids.NodeID](1)
	nodeSet.Add(nodeID)

	sentTo := b.net.Send(chitsMsg, nodeSet, ids.Empty, 0)
	b.logger.Debug("responded to PullQuery with Chits",
		log.Stringer("from", nodeID),
		log.Stringer("blockID", containerID),
		log.Uint64("height", blk.Height()),
		log.Int("sentTo", sentTo.Len()))

	return nil
}
func (b *blockHandler) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}
func (b *blockHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (b *blockHandler) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	return nil
}
func (b *blockHandler) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	return nil
}
func (b *blockHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (b *blockHandler) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}
func (b *blockHandler) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return nil
}
func (b *blockHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	// Handle AppGossip - try to process as block
	return b.Put(ctx, nodeID, 0, msg)
}
func (b *blockHandler) GetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time) error {
	return nil
}
func (b *blockHandler) StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return nil
}
func (b *blockHandler) GetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, heights []uint64) error {
	return nil
}
func (b *blockHandler) AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error {
	return nil
}
func (b *blockHandler) GetStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, height uint64) error {
	return nil
}
func (b *blockHandler) StateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return nil
}
func (b *blockHandler) Connected(ctx context.Context, nodeID ids.NodeID) error    { return nil }
func (b *blockHandler) Disconnected(ctx context.Context, nodeID ids.NodeID) error { return nil }
func (b *blockHandler) HealthCheck(ctx context.Context) (interface{}, error)      { return nil, nil }
func (b *blockHandler) Stop(ctx context.Context)                                  {}
func (b *blockHandler) HandleInbound(ctx context.Context, msg handler.Message) error {
	// Dispatch based on Op type
	switch msg.Op {
	case handler.Put, handler.PushQuery:
		// Put and PushQuery contain block data - process it
		if len(msg.Message) > 0 {
			return b.Put(ctx, msg.NodeID, msg.RequestID, msg.Message)
		}
	case handler.Chits:
		// Chits contain vote for a block (preferredID)
		if len(msg.Message) >= 32 && b.engine != nil {
			var preferredID ids.ID
			copy(preferredID[:], msg.Message[:32])

			// Derive Accept from block verification:
			// Accept=true only if we have the block AND it verifies
			accept := false
			if b.vm != nil {
				if blk, err := b.vm.GetBlock(ctx, preferredID); err == nil {
					if err := blk.Verify(ctx); err == nil {
						accept = true
					}
				}
			}

			vote := consensuschain.Vote{
				BlockID:  preferredID,
				NodeID:   msg.NodeID,
				Accept:   accept,
				SignedAt: time.Now(),
			}

			b.engine.ReceiveVote(vote)

			b.logger.Debug("received vote via Chits",
				log.Stringer("from", msg.NodeID),
				log.Stringer("blockID", preferredID),
				log.Bool("accept", accept))
		}
	}
	return nil
}
func (b *blockHandler) HandleOutbound(ctx context.Context, msg handler.Message) error {
	return nil
}

// placeholderHandler implements handler.Handler interface
type placeholderHandler struct{}

func (p *placeholderHandler) Context() *consensusctx.Context                 { return nil }
func (p *placeholderHandler) Start(ctx context.Context, startReqID uint32)  {}
func (p *placeholderHandler) Push(ctx context.Context, msg handler.Message) {}
func (p *placeholderHandler) Len() int                                      { return 0 }
func (p *placeholderHandler) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (p *placeholderHandler) GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
	return nil
}
func (p *placeholderHandler) GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time) error {
	return nil
}
func (p *placeholderHandler) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerIDs []ids.ID) error {
	return nil
}
func (p *placeholderHandler) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	return nil
}
func (p *placeholderHandler) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, container []byte) error {
	return nil
}
func (p *placeholderHandler) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
	return nil
}
func (p *placeholderHandler) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}
func (p *placeholderHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (p *placeholderHandler) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	return nil
}
func (p *placeholderHandler) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	return nil
}
func (p *placeholderHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (p *placeholderHandler) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}
func (p *placeholderHandler) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return nil
}
func (p *placeholderHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return nil
}
func (p *placeholderHandler) GetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time) error {
	return nil
}
func (p *placeholderHandler) StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return nil
}
func (p *placeholderHandler) GetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, heights []uint64) error {
	return nil
}
func (p *placeholderHandler) AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error {
	return nil
}
func (p *placeholderHandler) GetStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, height uint64) error {
	return nil
}
func (p *placeholderHandler) StateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return nil
}
func (p *placeholderHandler) Connected(ctx context.Context, nodeID ids.NodeID) error    { return nil }
func (p *placeholderHandler) Disconnected(ctx context.Context, nodeID ids.NodeID) error { return nil }
func (p *placeholderHandler) HealthCheck(ctx context.Context) (interface{}, error)      { return nil, nil }
func (p *placeholderHandler) Stop(ctx context.Context)                                  {}
func (p *placeholderHandler) HandleInbound(ctx context.Context, msg handler.Message) error {
	return nil
}
func (p *placeholderHandler) HandleOutbound(ctx context.Context, msg handler.Message) error {
	return nil
}

// noopWarpSender is a no-op implementation of warp.Sender for cross-chain messaging
// Used in single-node mode where cross-chain messaging is not needed
type noopWarpSender struct{}

// Compile-time check that noopWarpSender implements warp.Sender
var _ warp.Sender = (*noopWarpSender)(nil)

func (n *noopWarpSender) SendRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
	return nil
}

func (n *noopWarpSender) SendResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return nil
}

func (n *noopWarpSender) SendError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	return nil
}

func (n *noopWarpSender) SendGossip(ctx context.Context, config warp.SendConfig, gossipBytes []byte) error {
	return nil
}

// networkGossiper implements consensuschain.Gossiper for Lux consensus integration.
// It adapts the node's network layer to the minimal Gossiper interface used by
// the integrated consensus engine.
type networkGossiper struct {
	net        network.Network
	msgCreator message.OutboundMsgBuilder
}

// Compile-time check that networkGossiper implements Gossiper
var _ consensuschain.Gossiper = (*networkGossiper)(nil)

// GossipPut broadcasts a Put message with block data to validators.
func (g *networkGossiper) GossipPut(chainID ids.ID, networkID ids.ID, blockData []byte) int {
	if g.net == nil || g.msgCreator == nil {
		return 0
	}

	putMsg, err := g.msgCreator.Put(chainID, 0, blockData)
	if err != nil {
		return 0
	}

	// Gossip to all validators (-1 = all validators)
	sentTo := g.net.Gossip(putMsg, nil, networkID, -1, 0, 0)
	return sentTo.Len()
}

// SendPullQuery sends a PullQuery to validators requesting votes on a block.
// If validators is nil or empty, broadcasts to all validators (like GossipPut).
func (g *networkGossiper) SendPullQuery(chainID ids.ID, networkID ids.ID, blockID ids.ID, validators []ids.NodeID) int {
	if g.net == nil || g.msgCreator == nil {
		return 0
	}

	pullMsg, err := g.msgCreator.PullQuery(chainID, 0, 5*time.Second, blockID, 0)
	if err != nil {
		return 0
	}

	// If no specific validators provided, broadcast to all validators
	if len(validators) == 0 {
		sentTo := g.net.Gossip(pullMsg, nil, networkID, -1, 0, 0)
		return sentTo.Len()
	}

	// Otherwise, send to specific validators
	validatorSet := set.NewSet[ids.NodeID](len(validators))
	for _, v := range validators {
		validatorSet.Add(v)
	}

	sentTo := g.net.Send(pullMsg, validatorSet, networkID, 0)
	return sentTo.Len()
}

// SendChit sends a vote response (Chit) back to the node that requested our vote.
// This is called after verifying a block received via PullQuery.
func (g *networkGossiper) SendChit(toNodeID ids.NodeID, chainID ids.ID, requestID uint32, preferredID ids.ID) error {
	if g.net == nil || g.msgCreator == nil {
		return nil
	}

	// Create Chits message with the preferred block ID
	// For now, we use the preferredID as both preferred and accepted
	// since we've verified the block before sending the chit
	chitsMsg, err := g.msgCreator.Chits(chainID, requestID, preferredID, preferredID, preferredID, 0)
	if err != nil {
		return err
	}

	// Send to the specific node
	nodeSet := set.Of(toNodeID)
	g.net.Send(chitsMsg, nodeSet, ids.Empty, 0)
	return nil
}

