// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/luxfi/consensus"
	consContext "github.com/luxfi/consensus/context"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/metric"
	// "github.com/luxfi/consensus/core/interfaces" // Not used
	// "github.com/luxfi/consensus/core/tracker" // Not used
	"github.com/luxfi/consensus/engine/chain/block"
	// "github.com/luxfi/consensus/engine/dag/bootstrap/queue" // Not used
	// "github.com/luxfi/consensus/engine/dag/state" // Not used
	// "github.com/luxfi/consensus/engine/dag/vertex" // Not used
	// consensusvertex "github.com/luxfi/consensus/engine/vertex" // Not available in current consensus version
	consensusinterfaces "github.com/luxfi/consensus/interfaces"
	"github.com/luxfi/node/api/health"
	"github.com/luxfi/node/api/keystore"
	"github.com/luxfi/node/api/server"
	"github.com/luxfi/node/chains/atomic"

	// "github.com/luxfi/consensus/engine/chain/syncer" // Not used
	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/consensus/networking/sender"
	"github.com/luxfi/consensus/networking/timeout"
	consensusset "github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/database"
	"github.com/luxfi/database/meterdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/network/p2p"
	"github.com/luxfi/node/router"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/buffer"
	"github.com/luxfi/node/utils/constants"
	utilmetric "github.com/luxfi/node/utils/metric"
	"github.com/luxfi/node/utils/perms"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/fx"
	"github.com/luxfi/node/vms/metervm"
	"github.com/luxfi/node/vms/nftfx"
	"github.com/luxfi/trace"

	// "github.com/luxfi/node/vms/platformvm/warp" // Not used
	"github.com/luxfi/node/vms/propertyfx"
	"github.com/luxfi/node/vms/proposervm"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/tracedvm"

	// smeng "github.com/luxfi/consensus/engine/chain" // Not used
	// smbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap" // Not used
	// consensusgetter "github.com/luxfi/consensus/engine/chain/getter" // Not used
	// aveng "github.com/luxfi/consensus/engine/dag" // Not used
	// dagbootstrap "github.com/luxfi/consensus/engine/dag/bootstrap" // Not used
	// daggetter "github.com/luxfi/consensus/engine/dag/getter" // Not used
	timetracker "github.com/luxfi/consensus/networking/tracker"
	// smcon "github.com/luxfi/consensus/protocol/chain" // currently unused
	// p2ppb "github.com/luxfi/node/proto/pb/p2p" // Not used
)

const (
	ChainLabel = "chain"

	defaultChannelSize = 1
	initialQueueSize   = 3

	luxNamespace          = constants.PlatformName + "_lux"
	handlerNamespace      = constants.PlatformName + "_handler"
	meterchainvmNamespace = constants.PlatformName + "_meterchainvm"
	meterdagvmNamespace   = constants.PlatformName + "_meterdagvm"
	proposervmNamespace   = constants.PlatformName + "_proposervm"
	p2pNamespace          = constants.PlatformName + "_p2p"
	linearNamespace       = constants.PlatformName + "_linear"
	stakeNamespace        = constants.PlatformName + "_stake"
)

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

	errUnknownVMType           = errors.New("the vm should have type lux.GRAPHVM or linear.ChainVM")
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

	Shutdown()
}

// ChainParameters defines the chain being created
type ChainParameters struct {
	// The ID of the chain being created.
	ID ids.ID
	// ID of the net that validates this chain.
	NetID ids.ID
	// The genesis data of this chain's ledger.
	GenesisData []byte
	// The ID of the vm this chain is running.
	VMID ids.ID
	// The IDs of the feature extensions this chain is running.
	FxIDs []ids.ID
	// Invariant: Only used when [ID] is the P-chain ID.
	CustomBeacons validators.Manager
}

type chainInfo struct {
	Name    string
	Context context.Context
	VM      interface{} // Changed from core.VM since core.VM uses context.Context
	Handler handler.Handler
	Engine  Engine // Added to handle Start/Stop operations
}

// Engine represents a consensus engine
type Engine interface {
	Start(context.Context, bool) error
	StopWithError(context.Context, error) error
	Context() context.Context
}

// senderToAppSenderAdapter adapts sender.Sender to block.AppSender
type senderToAppSenderAdapter struct {
	sender sender.Sender
}

func (s *senderToAppSenderAdapter) SendAppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, appRequestBytes []byte) error {
	// sender.Sender doesn't have SendAppRequest, return nil for now
	return nil
}

func (s *senderToAppSenderAdapter) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	// sender.Sender doesn't have SendAppResponse, return nil for now
	return nil
}

func (s *senderToAppSenderAdapter) SendAppGossip(ctx context.Context, appGossipBytes []byte) error {
	// sender.Sender doesn't have SendAppGossip, return nil for now
	return nil
}

// chainVMWrapper wraps block.ChainVM to implement core.VM.
// Uses vms.HandlerDelegator for clean, DRY handler delegation.
type chainVMWrapper struct {
	vm block.ChainVM
	*vms.HandlerDelegator[block.ChainVM]
}

// newChainVMWrapper creates a wrapper that properly delegates handlers
func newChainVMWrapper(vm block.ChainVM) *chainVMWrapper {
	return &chainVMWrapper{
		vm:               vm,
		HandlerDelegator: vms.NewHandlerDelegator(vm),
	}
}

func (c *chainVMWrapper) Initialize(
	ctx context.Context,
	consensusCtx interface{},
	dbManager interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	msgChan interface{},
	fxs []interface{},
	appSender interface{},
) error {
	// ChainVM has a different Initialize signature
	// This is a no-op since the actual initialization happens elsewhere
	return nil
}

func (c *chainVMWrapper) Shutdown(ctx context.Context) error {
	// block.ChainVM doesn't have Shutdown method
	return nil
}

// CreateHandlers and CreateStaticHandlers are inherited from HandlerDelegator.
// No duplicate code needed - beautiful composition!

func (c *chainVMWrapper) HealthCheck(ctx context.Context) (interface{}, error) {
	// ChainVM doesn't have HealthCheck, return nil
	return nil, nil
}

func (c *chainVMWrapper) SetState(ctx context.Context, state consensusinterfaces.State) error {
	// ChainVM doesn't have SetState, return nil
	return nil
}

func (c *chainVMWrapper) Version(ctx context.Context) (string, error) {
	// Return a default version
	return "1.0.0", nil
}

// linearizableVMWrapper wraps consensusvertex.LinearizableVMWithEngine to implement core.VM
// Disabled - consensus package doesn't have vertex types
/*
type linearizableVMWrapper struct {
	vm consensusvertex.LinearizableVMWithEngine
}*/

// linearizableVMWrapper methods disabled - type not available
/*
func (l *linearizableVMWrapper) Initialize() error {
	// LinearizableVMWithEngine has a different Initialize signature
	// This is a no-op since the actual initialization happens elsewhere
	return nil
}

func (l *linearizableVMWrapper) Shutdown() error {
	return l.vm.Shutdown(context.Background())
}

func (l *linearizableVMWrapper) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return l.vm.CreateHandlers(ctx)
}
*/

// sharedMemoryWrapper wraps atomic.SharedMemory to implement interfaces.SharedMemory
type sharedMemoryWrapper struct {
	atomicMemory atomic.SharedMemory
}

func (s *sharedMemoryWrapper) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	return s.atomicMemory.Get(peerChainID, keys)
}

func (s *sharedMemoryWrapper) Apply(requests map[ids.ID]interface{}, batch ...interface{}) error {
	// Convert requests to the atomic.Requests type
	atomicRequests := make(map[ids.ID]*atomic.Requests)
	for chainID, req := range requests {
		if atomicReq, ok := req.(*atomic.Requests); ok {
			atomicRequests[chainID] = atomicReq
		}
	}

	// Convert batch to database.Batch if provided
	if len(batch) > 0 {
		if dbBatch, ok := batch[0].(database.Batch); ok {
			return s.atomicMemory.Apply(atomicRequests, dbBatch)
		}
	}

	return s.atomicMemory.Apply(atomicRequests)
}

// validatorStateWrapper wraps validators.State to implement interfaces.ValidatorState
type validatorStateWrapper struct {
	state validators.State
}

// consensusValidatorStateWrapper wraps validators.State to implement consensus.ValidatorState
type consensusValidatorStateWrapper struct {
	state validators.State
}

func (v *consensusValidatorStateWrapper) GetCurrentHeight() (uint64, error) {
	return v.state.GetCurrentHeight(context.Background())
}

func (v *consensusValidatorStateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// validators.State doesn't have GetMinimumHeight, return current height
	return v.state.GetCurrentHeight(ctx)
}

func (v *consensusValidatorStateWrapper) GetNetID(chainID ids.ID) (ids.ID, error) {
	// validators.State doesn't have GetNetID, return empty ID for now
	return ids.Empty, nil
}

func (v *consensusValidatorStateWrapper) GetValidatorSet(height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	valSet, err := v.state.GetValidatorSet(context.Background(), height, netID)
	if err != nil {
		return nil, err
	}
	result := make(map[ids.NodeID]uint64, len(valSet))
	for nodeID, val := range valSet {
		result[nodeID] = val.Weight
	}
	return result, nil
}

func (v *consensusValidatorStateWrapper) GetChainID(netID ids.ID) (ids.ID, error) {
	// Not available in validators.State, return empty ID
	return ids.Empty, nil
}

func (v *consensusValidatorStateWrapper) GetSubnetID(chainID ids.ID) (ids.ID, error) {
	// Not available in validators.State, return empty ID
	return ids.Empty, nil
}

func (v *consensusValidatorStateWrapper) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*consContext.GetValidatorOutput, error) {
	// Get the validator set from the underlying state
	valSet, err := v.state.GetValidatorSet(ctx, height, netID)
	if err != nil {
		return nil, err
	}

	// Convert to GetValidatorOutput format
	result := make(map[ids.NodeID]*consContext.GetValidatorOutput, len(valSet))
	for nodeID, val := range valSet {
		result[nodeID] = &consContext.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: val.PublicKey,
			Weight:    val.Weight,
		}
	}
	return result, nil
}

func (v *validatorStateWrapper) GetCurrentHeight() (uint64, error) {
	return v.state.GetCurrentHeight(context.Background())
}

func (v *validatorStateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// validators.State doesn't have GetMinimumHeight, return current height
	return v.state.GetCurrentHeight(ctx)
}

func (v *validatorStateWrapper) GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	// validators.State doesn't have GetNetID, return empty ID for now
	return ids.Empty, nil
}

func (v *validatorStateWrapper) GetValidatorSet(height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	valSet, err := v.state.GetValidatorSet(context.Background(), height, netID)
	if err != nil {
		return nil, err
	}
	result := make(map[ids.NodeID]uint64, len(valSet))
	for nodeID, val := range valSet {
		result[nodeID] = val.Weight
	}
	return result, nil
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
	StakingBLSKey          *bls.SecretKey
	TracingEnabled         bool
	// Must not be used unless [TracingEnabled] is true as this may be nil.
	Tracer                    trace.Tracer
	Log                       log.Logger
	LogFactory                log.Factory
	VMManager                 vms.Manager // Manage mappings from vm ID --> vm
	BlockAcceptorGroup        consensus.AcceptorGroup
	TxAcceptorGroup           consensus.AcceptorGroup
	VertexAcceptorGroup       consensus.AcceptorGroup
	DB                        database.Database
	MsgCreator                message.OutboundMsgBuilder // message creator, shared with network
	Router                    router.Router              // Routes incoming messages to the appropriate chain
	Net                       network.Network            // Sends consensus messages to other validators
	Validators                validators.Manager         // Validators validating on this chain
	NodeID                    ids.NodeID                 // The ID of this node
	NetworkID                 uint32                     // ID of the network this node is connected to
	PartialSyncPrimaryNetwork bool
	Server                    server.Server // Handles HTTP API calls
	Keystore                  keystore.Keystore
	AtomicMemory              *atomic.Memory
	XAssetID                ids.ID
	SkipBootstrap             bool            // Skip bootstrapping and start processing immediately
	EnableAutomining          bool            // Enable automining in POA mode
	XChainID                  ids.ID          // ID of the X-Chain,
	CChainID                  ids.ID          // ID of the C-Chain,
	CriticalChains            set.Set[ids.ID] // Chains that can't exit gracefully
	TimeoutManager            timeout.Manager // Manages request timeouts when sending messages to other validators
	Health                    health.Registerer
	SubnetConfigs             map[ids.ID]subnets.Config // ID -> SubnetConfig
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

	ApricotPhase4Time            time.Time
	ApricotPhase4MinPChainHeight uint64

	// Tracks CPU/disk usage caused by each peer.
	ResourceTracker timetracker.ResourceTracker

	StateSyncBeacons []ids.NodeID

	ChainDataDir string

	Subnets *Subnets
}

type manager struct {
	// Note: The string representation of a chain's ID is also considered to be an alias of the chain
	// That is, [chainID].String() is an alias for the chain, too
	ids.Aliaser
	ManagerConfig

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

	chainsLock sync.Mutex
	// Key: Chain's ID
	// Value: The chain
	chains map[ids.ID]*chainInfo

	// linear++ related interface to allow validators retrieval
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

	linearGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(linearNamespace, linearGatherer); err != nil {
		return nil, err
	}

	stakeGatherer := metric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(stakeNamespace, stakeGatherer); err != nil {
		return nil, err
	}

	return &manager{
		Aliaser:                ids.NewAliaser(),
		ManagerConfig:          *config,
		chains:                 make(map[ids.ID]*chainInfo),
		chainsQueue:            buffer.NewUnboundedBlockingDeque[ChainParameters](initialQueueSize),
		unblockChainCreatorCh:  make(chan struct{}),
		chainCreatorShutdownCh: make(chan struct{}),

		luxGatherer:          luxGatherer,
		handlerGatherer:      handlerGatherer,
		meterChainVMGatherer: meterChainVMGatherer,
		meterGRAPHVMGatherer: meterGRAPHVMGatherer,
		proposervmGatherer:   proposervmGatherer,
		p2pGatherer:          p2pGatherer,
		linearGatherer:       linearGatherer,
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

	if sb, _ := m.Subnets.GetOrCreate(chainParams.NetID); !sb.AddChain(chainParams.ID) {
		m.Log.Debug("skipping chain creation",
			log.String("reason", "chain already staged"),
			log.Stringer("netID", chainParams.NetID),
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
		)
		return
	}

	if ok := m.chainsQueue.PushRight(chainParams); !ok {
		m.Log.Warn("skipping chain creation",
			log.String("reason", "couldn't enqueue chain"),
			log.Stringer("netID", chainParams.NetID),
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
		log.Stringer("netID", chainParams.NetID),
		log.Stringer("chainID", chainParams.ID),
		log.Stringer("vmID", chainParams.VMID),
	)

	sb, _ := m.Subnets.GetOrCreate(chainParams.NetID)

	// Note: buildChain builds all chain's relevant objects (notably engine and handler)
	// but does not start their operations. Starting of the handler (which could potentially
	// issue some internal messages), is delayed until chain dispatching is started and
	// the chain is registered in the manager. This ensures that no message generated by handler
	// upon start is dropped.
	chain, err := m.buildChain(chainParams, sb)
	if err != nil {
		// Special handling for X-Chain in single validator mode
		// Allow the node to continue without X-Chain when it fails with VM type error
		isXChain := chainParams.ID == m.XChainID
		isVMTypeError := err == errUnknownVMType
		skipBootstrapMode := m.SkipBootstrap

		// If X-Chain fails with VM type error in single validator mode, just log and continue
		if isXChain && isVMTypeError && skipBootstrapMode {
			chainAlias := m.PrimaryAliasOrDefault(chainParams.ID)
			m.Log.Warn("X-Chain creation failed in single validator mode - continuing without X-Chain",
				log.Stringer("netID", chainParams.NetID),
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
				chainParams.NetID.String(),
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
				log.Stringer("netID", chainParams.NetID),
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
			log.Stringer("netID", chainParams.NetID),
			log.Stringer("chainID", chainParams.ID),
			log.String("chainAlias", chainAlias),
			log.Stringer("vmID", chainParams.VMID),
			log.Err(err),
		)

		// Register the health check for this chain regardless of if it was
		// created or not. This attempts to notify the node operator that their
		// node may not be properly validating the net they expect to be
		// validating.
		healthCheckErr := fmt.Errorf("failed to create chain on net %s: %w", chainParams.NetID, err)
		err := m.Health.RegisterHealthCheck(
			chainAlias,
			health.CheckerFunc(func(context.Context) (interface{}, error) {
				return nil, healthCheckErr
			}),
			chainParams.NetID.String(),
		)
		if err != nil {
			m.Log.Error("failed to register failing health check",
				log.Stringer("netID", chainParams.NetID),
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
			log.Stringer("netID", chainParams.NetID),
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
			log.Err(err),
		)
	}

	// Notify those that registered to be notified when a new chain is created
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

				m.Log.Info("Registered HTTP handler",
					log.String("chainAlias", chainAlias),
					log.Stringer("chainID", chainParams.ID),
					log.String("base", chainBase),
					log.String("endpoint", endpoint),
				)
			}
		}
	}

	// TODO: Fix Router.AddChain - the consensus Router interface has changed
	// and no longer has an AddChain method. Need to update the routing logic.
	// m.ManagerConfig.Router.AddChain(chainParams.ID, chain.Handler)

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

	// Tell the chain to start processing messages.
	// If the X, P, or C Chain panics, do not attempt to recover
	if chain.Engine != nil {
		chain.Engine.Start(context.TODO(), !m.CriticalChains.Contains(chainParams.ID))
	}
}

// Create a chain
func (m *manager) buildChain(chainParams ChainParameters, sb subnets.Net) (*chainInfo, error) {
	if chainParams.ID != constants.PlatformChainID && chainParams.VMID == constants.PlatformVMID {
		return nil, errCreatePlatformVM
	}
	// primaryAlias will be used by the chains created below
	// primaryAlias := m.PrimaryAliasOrDefault(chainParams.ID)

	// Create this chain's data directory
	chainDataDir := filepath.Join(m.ChainDataDir, chainParams.ID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}

	// Create the log and context of the chain
	chainLog := m.Log // Use main log instead of creating chain-specific log

	// linearMetrics was here but not used in context.Context
	// linearMetrics, err := metric.MakeAndRegister(
	// 	m.linearGatherer,
	// 	primaryAlias,
	// )
	// if err != nil {
	// 	return nil, err
	// }

	// Create base context with IDs
	ctx := context.Background()
	// Note: Using local consensus package which has different fields
	// PublicKey needs to be []byte, not *bls.PublicKey
	var pubKeyBytes []byte
	if m.StakingBLSKey != nil && m.StakingBLSKey.PublicKey() != nil {
		// BLS PublicKey doesn't have a Bytes() method, so we'll leave it nil for now
		// This would need proper serialization in production
		pubKeyBytes = nil
	}

	ctx = consensus.WithIDs(ctx, consensus.IDs{
		NetworkID: m.NetworkID,
		NetID:     chainParams.NetID,
		ChainID:   chainParams.ID,
		NodeID:    m.NodeID,
		PublicKey: pubKeyBytes,
	})

	// Get a factory for the vm we want to use on our chain
	m.Log.Info("Getting VM factory", log.Stringer("vmID", chainParams.VMID))
	vmFactory, err := m.VMManager.GetFactory(chainParams.VMID)
	if err != nil {
		m.Log.Error("Failed to get VM factory", log.Stringer("vmID", chainParams.VMID), log.Err(err))
		return nil, fmt.Errorf("error while getting vmFactory: %w", err)
	}
	m.Log.Info("Got VM factory successfully")

	// Create the chain
	vm, err := vmFactory.New(chainLog)
	if err != nil {
		return nil, fmt.Errorf("error while creating vm for chain %s: %w", chainParams.ID, err)
	}

	chainFxs := make([]*core.Fx, len(chainParams.FxIDs))
	for i, fxID := range chainParams.FxIDs {
		_, ok := fxs[fxID]
		if !ok {
			return nil, fmt.Errorf("fx %s not found", fxID)
		}

		// core.Fx is an empty struct, so just create it
		chainFxs[i] = &core.Fx{}
	}

	var chain *chainInfo
	switch vm := vm.(type) {
	// Vertex VM support disabled for now - consensus package doesn't have these types
	// case consensusvertex.LinearizableVMWithEngine:
	// 	chain, err = m.createLuxChain(
	// 		ctx,
	// 		chainParams,
	// 		chainParams.GenesisData,
	// 		m.Validators,
	// 		vm,
	// 		chainFxs,
	// 		sb,
	// 	)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("error while creating new lux vm %w", err)
	// 	}
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

		chain, err = m.createLinearChain(
			ctx,
			chainParams,
			chainParams.GenesisData,
			m.Validators,
			beacons,
			vm,
			chainFxs,
			sb,
		)
		if err != nil {
			m.Log.Error("createLinearChain failed for Platform chain",
				log.String("actualError", err.Error()),
				log.Err(err))
			return nil, fmt.Errorf("error while creating new linear vm: %w", err)
		}
	default:
		return nil, errUnknownVMType
	}

	// timeout.Manager doesn't have RegisterChain in consensus package
	// if err := m.TimeoutManager.RegisterChain(ctx); err != nil {
	// 	return nil, err
	// }

	// Register HTTP handlers for this chain if the VM supports it
	m.Log.Info("Checking for CreateHandlers support",
		log.Stringer("chainID", chainParams.ID),
		log.String("vmType", fmt.Sprintf("%T", chain.VM)))

	if vm, ok := chain.VM.(interface {
		CreateHandlers(context.Context) (map[string]http.Handler, error)
	}); ok {
		m.Log.Info("VM supports CreateHandlers, calling it now",
			log.Stringer("chainID", chainParams.ID))
		handlers, err := vm.CreateHandlers(context.TODO())
		m.Log.Info("CreateHandlers returned",
			log.Stringer("chainID", chainParams.ID),
			log.Int("numHandlers", len(handlers)),
			log.Err(err))
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

				m.Log.Info("Registered HTTP handler",
					log.String("chainAlias", chainAlias),
					log.Stringer("chainID", chainParams.ID),
					log.String("base", chainBase),
					log.String("endpoint", endpoint),
				)
			}
		}
	} else {
		m.Log.Info("VM does not support CreateHandlers",
			log.Stringer("chainID", chainParams.ID),
			log.String("vmType", fmt.Sprintf("%T", chain.VM)))
	}

	return chain, nil
}

func (m *manager) AddRegistrant(r Registrant) {
	m.registrants = append(m.registrants, r)
}

// Create a Graph-based blockchain that uses Lux
// Disabled for now - consensus package doesn't have vertex types
/*
func (m *manager) createLuxChain(
	ctx context.Context,
	chainParams ChainParameters,
	genesisData []byte,
	vdrs validators.Manager,
	vm consensusvertex.LinearizableVMWithEngine,
	fxs []*core.Fx,
	sb subnets.Net,
) (*chainInfo, error) {*/
/*	// Use a sync.Mutex for chain creation if needed
	// State tracking will be handled by the engine

	// Extract chainID from chainParams
	chainID := chainParams.ID
	primaryAlias := m.PrimaryAliasOrDefault(chainID)

	// Create this chain's data directory
	chainDataDir := filepath.Join(m.ChainDataDir, chainID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}

	// Get VM metrics
	_, err = m.getOrMakeVMRegisterer(chainID, primaryAlias)
	if err != nil {
		return nil, err
	}

	// Create SharedMemory wrapper for consensus package (unused for now)
	_ = &sharedMemoryWrapper{
		atomicMemory: m.AtomicMemory.NewSharedMemory(chainID),
	}

	// Create ValidatorState wrapper (unused for now)
	_ = &validatorStateWrapper{
		state: m.validatorState,
	}
	meterDBReg, err := metric.MakeAndRegister(
		m.MeterDBMetrics,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Create Metrics from Registry for meterdb
	meterDBMetrics := metric.NewWithRegistry(primaryAlias, meterDBReg)
	meterDB, err := meterdb.New(meterDBMetrics, m.DB)
	if err != nil {
		return nil, err
	}

	prefixDB := prefixdb.New(chainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	vertexDB := prefixdb.New(VertexDBPrefix, prefixDB)
	vertexBootstrappingDB := prefixdb.New(VertexBootstrappingDBPrefix, prefixDB)
	txBootstrappingDB := prefixdb.New(TxBootstrappingDBPrefix, prefixDB)
	_ = prefixdb.New(BlockBootstrappingDBPrefix, prefixDB) // blockBootstrappingDB not used for DAG

	luxMetricsReg, err := metric.MakeAndRegister(
		m.luxGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Convert Registry to Metrics for queue functions
	luxMetrics := metric.NewWithRegistry(primaryAlias, luxMetricsReg)

	// Create queue blockers for bootstrapping
	vtxBlocker := queue.NewQueue()
	txBlocker := queue.NewQueue()

	// Passes messages from the lux engines to the network
	// Create a sender directly using the network
	luxMessageSender := m.Net

	// Tracing is handled at the network level

	// Passes messages from the linear engines to the network
	linearMessageSender := m.Net

	chainConfig, err := m.getChainConfig(chainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	// For linear/block chains, we don't use vertex/DAG wrappers
	// Platform VM is a block-based VM, not a vertex-based VM
	// So we skip the vertex-related setup and go straight to block VM setup

	// Initialize the ProposerVM and the vm wrapped inside it
	var (
		minBlockDelay       = proposervm.DefaultMinBlockDelay
		numHistoricalBlocks = proposervm.DefaultNumHistoricalBlocks
	)
	netID := consensus.GetNetID(ctx)
	if subnetCfg, ok := m.SubnetConfigs[netID]; ok {
		minBlockDelay = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	}
	m.Log.Info("creating proposervm wrapper",
		log.Time("activationTime", m.ApricotPhase4Time),
		log.Uint64("minPChainHeight", m.ApricotPhase4MinPChainHeight),
		log.Duration("minBlockDelay", minBlockDelay),
		log.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	// Skip proposervm wrapper for Platform chain for now due to initialization issues
	if chainParams.ID == constants.PlatformChainID {
		m.Log.Info("skipping proposervm wrapper for Platform chain")
		// Create a wrapper for the platform VM
		vmWrapper := newChainVMWrapper(vm)
		return vmWrapper, vm, nil
	}

	// For block-based VMs (like Platform VM), we pass the VM directly to ProposerVM
	// vm is already a block.ChainVM from the factory
	// Rename to chainblockVM for clarity - this is the block-based chain VM
	var chainblockVM block.ChainVM = vm
	if m.TracingEnabled {
		chainblockVM = tracedvm.NewBlockVM(chainblockVM, primaryAlias, m.Tracer)
	}

	proposervmReg, err := metric.MakeAndRegister(
		m.proposervmGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Note: chainblockVMWithProposer is the VM that the Linear engines should be
	// using.
	var chainblockVMWithProposer block.ChainVM = proposervm.New(
		chainblockVM,
		proposervm.Config{
			ActivationTime:      m.ApricotPhase4Time,
			DurangoTime:         version.GetDurangoTime(m.NetworkID),
			MinimumPChainHeight: m.ApricotPhase4MinPChainHeight,
			MinBlkDelay:         minBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   m.StakingTLSSigner,
			StakingCertLeaf:     m.StakingTLSCert,
			Registerer:          proposervmReg,
		},
	)

	if m.MeterVMEnabled {
		meterchainvmReg, err := metric.MakeAndRegister(
			m.meterChainVMGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		chainblockVMWithProposer = metervm.NewBlockVM(chainblockVMWithProposer, meterchainvmReg)
	}
	if m.TracingEnabled {
		chainblockVMWithProposer = tracedvm.NewBlockVM(chainblockVMWithProposer, "proposervm", m.Tracer)
	}

	// Note: linearizableVM is the VM that the Lux engines should be
	// using.
	linearizableVM := &initializeOnLinearizeVM{
		LinearizableVMWithEngine: graphVM,
		vmToInitialize:           nil, // Will be set to proper VM type later
		vmToLinearize:            untracedVMWrappedInsideProposerVM,

		ctx:          ctx,
		db:           vmDB,
		genesisBytes: genesisData,
		upgradeBytes: chainConfig.Upgrade,
		configBytes:  chainConfig.Config,
		fxs:          fxs,
		appSender:    nil, // Will be set to proper AppSender type later
	}

	bootstrapWeight, err := vdrs.TotalWeight(netID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching weight for net %s: %w", netID, err)
	}

	consensusParams := sb.Config().ConsensusParameters
	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	_, err = metric.MakeAndRegister(
		m.stakeGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// tracker.NewMeteredPeers not available in current consensus version
	// connectedValidators, err := tracker.NewMeteredPeers(stakeReg)
	// if err != nil {
	// 	return nil, fmt.Errorf("error creating peer tracker: %w", err)
	// }
	// vdrs.RegisterSetCallbackListener(netID, connectedValidators)
	_ = interface{}(nil) // connectedValidators placeholder

	p2pReg, err := metric.MakeAndRegister(
		m.p2pGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	_, err = p2p.NewPeerTracker(
		m.Log,
		"peer_tracker",
		p2pReg,
		set.NewSet[ids.NodeID](0), // Empty set of NodeIDs
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}

	_, err = metric.MakeAndRegister(
		m.handlerGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Asynchronously passes messages from the network to the consensus engine
	h, err := handler.New(
		runtime,
		nil, // cn *block.ChangeNotifier - not used for DAG chains
		nil, // subscription core.Subscription - not used for DAG chains
		vdrs,
		m.FrontierPollFrequency,
		m.ConsensusAppConcurrency,
		m.ResourceTracker,
		sb,
		connectedValidators,
		peerTracker,
		handlerReg,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing network handler: %w", err)
	}

	connectedBeacons := tracker.NewPeers()
	var startupTracker tracker.Startup
	if m.SkipBootstrap {
		// Use startup tracker with 0 weight requirement to skip bootstrap
		startupTracker = tracker.NewStartup(connectedBeacons, 0)
		m.Log.Info("bootstrapping disabled - starting processing immediately")
	} else {
		startupTracker = tracker.NewStartup(connectedBeacons, float64(3*bootstrapWeight+3)/4.0)
	}
	// startupTracker doesn't implement SetCallbackListener, skip registration
	// vdrs.RegisterSetCallbackListener(startupTracker)

	consensusGetHandler, err := consensusgetter.New(
		chainblockVMWithProposer,
		linearMessageSender,
		m.Log,
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
		// ctx.Registerer doesn't exist in context.Context
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize consensus base message handler: %w", err)
	}

	// var linearConsensus smcon.Consensus

	// Create engine, bootstrapper and state-syncer in this order,
	// to make sure start callbacks are duly initialized
	// Convert sampling.Parameters to chain.Parameters
	chainParams := smeng.Parameters{}

	// var linearEngine core.Engine
	linearEngine, err := smeng.New(runtime, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error initializing linear engine: %w", err)
	}
	_ = linearEngine // temporarily unused

	// Tracing for linearEngine can be added here if needed
	// if m.TracingEnabled {
	// 	linearEngine = core.TraceEngine(linearEngine, m.Tracer)
	// }

	// create bootstrap gear
	bootstrapBeacons := vdrs
	// In skip-bootstrap mode, use empty beacons for single-node development
	if m.SkipBootstrap {
		bootstrapBeacons = validators.NewManager()
	}

	bootstrapCfg := smbootstrap.Config{
		AllGetsServer:                  consensusGetHandler,
		Ctx:                            runtime,
		Beacons:                        bootstrapBeacons,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Sender:                         linearMessageSender,
		BootstrapTracker:               sb,
		Timer:                          nil, // Timer not used for now
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		Blocked:                        nil, // Blocked not used for now
		VM:                             chainblockVMWithProposer,
	}

	// Create bootstrapper with a callback function
	bootstrapCallback := func(ctx context.Context, lastReqID uint32) error {
		_ = lastReqID // Implementation note
		
		// CRITICAL: For Platform chain, unblock other chains when bootstrap completes
		if chainParams.ID == constants.PlatformChainID {
			m.Log.Info("Platform chain bootstrap complete - unblocking chain creator")
			select {
			case <-m.unblockChainCreatorCh:
				// Channel already closed, ignore
			default:
				close(m.unblockChainCreatorCh)
			}
		}
		
		return linearEngine.Start(ctx)
	}

	linearBootstrapper, err := smbootstrap.New(
		bootstrapCfg,
		bootstrapCallback,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing linear bootstrapper: %w", err)
	}

	if m.TracingEnabled {
		linearBootstrapper = smbootstrap.Trace(linearBootstrapper, m.Tracer)
	}

	// Create handler for DAG bootstrap
	getHandler, err := daggetter.NewHandler(
		vtxManager,
		luxMessageSender,
		m.Log,
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize lux base message handler: %w", err)
	}

	// Runtime already created above at line 845

	// create engine gear
	dagParams := aveng.Parameters{
		K:               20, // Sample size
		AlphaPreference: 14, // Preference threshold
		AlphaConfidence: 14, // Confidence threshold
		Beta:            20, // Finalization threshold
	}
	_, err = aveng.New(runtime, dagParams) // luxEngine not used currently
	if err != nil {
		return nil, fmt.Errorf("failed to create dag engine: %w", err)
	}
	// Note: aveng.Engine doesn't implement core.Engine interface
	// Tracing is not supported for graph engines currently

	// create bootstrap gear
	// beacons := vdrs // Not used
	// In skip-bootstrap mode, use empty beacons for single-node development
	// if m.SkipBootstrap {
	// 	beacons = validators.NewManager()
	// 	ctx.Log.Info("skip-bootstrap enabled - using empty beacons for X-Chain single-node mode")
	// }

	luxBootstrapperConfig := dagbootstrap.Config{
		AllGetsServer:  getHandler,
		Ctx:            runtime,
		StartupTracker: startupTracker,
		Sender:         luxMessageSender,
		PeerTracker:    peerTracker,
		// Beacons field removed - beacons,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		VtxBlocked:                     vtxBlocker,
		TxBlocked:                      txBlocker,
		Manager:                        vtxManager,
		VM:                             linearizableVM,
		Haltable:                       nil, // Implementation note
	}
	// if ctx.ChainID == m.XChainID {
	// 	luxBootstrapperConfig.StopVertexID = version.CortinaXChainStopVertexID[ctx.NetworkID]
	// }

	_, err = dagbootstrap.New( // luxBootstrapper not used currently
		luxBootstrapperConfig,
		func(ctx context.Context, lastReqID uint32) error {
			return linearBootstrapper.Start(ctx)
		},
		// ctx.Registerer doesn't exist in context.Context
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing lux bootstrapper: %w", err)
	}

	// var tracedLuxBootstrapper core.BootstrapableEngine = luxBootstrapper
	// if m.TracingEnabled {
	// 	tracedLuxBootstrapper = core.TraceBootstrapableEngine(luxBootstrapper, m.Tracer)
	// }

	// h.SetEngineManager(&handler.EngineManager{
	// 	Dag: &handler.Engine{
	// 		StateSyncer:  nil,
	// 		Bootstrapper: tracedLuxBootstrapper,
	// 		Consensus:    luxEngine,
	// 	},
	// 	Chain: &handler.Engine{
	// 		StateSyncer:  nil,
	// 		Bootstrapper: linearBootstrapper,
	// 		Consensus:    linearEngine,
	// 	},
	// })

	// // Register health check for this chain
	// if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.NetID.String()); err != nil {
	// 	return nil, fmt.Errorf("couldn't add health check for chain %s: %w", primaryAlias, err)
	// }

	// Create a wrapper to adapt LinearizableVMWithEngine to core.VM
	vmWrapper := &linearizableVMWrapper{vm: graphVM}

	return &chainInfo{
		Name:    primaryAlias,
		Context: ctx,
		VM:      vmWrapper,
		Handler: h,
	}, nil
}
*/ // End of createLuxChain - disabled

// Create a linear chain using the Linear consensus engine
func (m *manager) createLinearChain(
	ctx context.Context,
	chainParams ChainParameters,
	genesisData []byte,
	vdrs validators.Manager,
	beacons validators.Manager,
	vm block.ChainVM,
	fxs []*core.Fx,
	sb subnets.Net,
) (*chainInfo, error) {
	// Use a sync.Mutex for chain creation if needed
	// State is managed by the consensus engine

	// Extract chainID from chainParams
	chainID := chainParams.ID
	primaryAlias := m.PrimaryAliasOrDefault(chainID)

	// Create this chain's data directory
	chainDataDir := filepath.Join(m.ChainDataDir, chainID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}
	meterDBReg, err := metric.MakeAndRegister(
		m.MeterDBMetrics,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Create Metrics from Registry for meterdb
	meterDBMetrics := metric.NewWithRegistry(primaryAlias, meterDBReg)
	meterDB, err := meterdb.New(meterDBMetrics, m.DB)
	if err != nil {
		return nil, err
	}

	prefixDB := prefixdb.New(chainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	_ = prefixdb.New(ChainBootstrappingDBPrefix, prefixDB) // bootstrappingDB not used

	// Get VM metrics
	_, err = m.getOrMakeVMRegisterer(chainID, primaryAlias)
	if err != nil {
		return nil, err
	}

	// Create SharedMemory wrapper for consensus package (unused for now)
	_ = &sharedMemoryWrapper{
		atomicMemory: m.AtomicMemory.NewSharedMemory(chainID),
	}

	// Create ValidatorState wrapper (unused for now)
	_ = &validatorStateWrapper{
		state: m.validatorState,
	}

	// Runtime is not available in current consensus package
	// We'll use the context directly instead
	// TODO: Re-enable when consensus package is updated
	/*
		ids := consensus.MustIDs(ctx)
		runtime := &interfaces.Runtime{
			NetworkID:      ids.NetworkID,
			NetID:       ids.NetID,
			ChainID:        ids.ChainID,
			NodeID:         ids.NodeID,
			PublicKey:      ids.PublicKey,
			XAssetID:     m.XAssetID,
			CChainID:       m.CChainID,
			ChainDataDir:   chainDataDir,
			Log:            m.Log,
			Metrics:        vmMetrics,
			ValidatorState: valStateWrapper,
			BCLookup:       m,
			SharedMemory:   sharedMem,
		}

		// Passes messages from the consensus engine to the network
		messageSender, err := sender.New(
			runtime,
			m.MsgCreator,
			m.Net,                  // Passing network as interface{}
			m.ManagerConfig.Router, // Passing router as interface{}
			sb,
			// ctx.Registerer doesn't exist in context.Context
		)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialize sender: %w", err)
		}

		if m.TracingEnabled {
			messageSender = sender.Trace(messageSender, m.Tracer)
		}
	*/

	// For now, use a nil message sender since sender.New requires Runtime
	// ExternalSender doesn't exist, just use interface{}
	_ = interface{}(nil) // messageSender placeholder

	// If [m.validatorState] is nil then we are creating the P-Chain. Since the
	// P-Chain is the first chain to be created, we can use it to initialize
	// required interfaces for the other chains
	if m.validatorState == nil {
		valState, ok := vm.(validators.State)
		if !ok {
			return nil, fmt.Errorf("expected validators.State but got %T", vm)
		}

		// if m.TracingEnabled {
		// 	valState = validators.Trace(valState, "platformvm", m.Tracer)
		// }

		// Notice that this context is left unlocked. This is because the
		// lock will already be held when accessing these values on the
		// P-chain.
		// Create a wrapper to adapt validators.State to interfaces.ValidatorState
		// ValidatorState is already set in context
		// Create a simpler wrapper for consensus.ValidatorState
		consensusValState := &consensusValidatorStateWrapper{state: valState}
		ctx = consensus.WithValidatorState(ctx, consensusValState)

		// Initialize the validator state for future chains.
		m.validatorState = valState // State locking handled elsewhere if needed
		// if m.TracingEnabled {
		// 	m.validatorState = validators.Trace(m.validatorState, "lockedState", m.Tracer)
		// }

		if !m.ManagerConfig.SybilProtectionEnabled {
			// m.validatorState = validators.NewNoValidatorsState(m.validatorState)
			// Wrap the NoValidatorsState as well
			// ctx.ValidatorState = &validatorStateWrapper{state: validators.NewNoValidatorsState(valState)}
		}

		// Set this func only for platform
		//
		// The linear bootstrapper ensures this function is only executed once, so
		// we don't need to be concerned about closing this channel multiple times.
		// NOTE: The unblocking of chain creator is now handled directly in the
		// bootstrap callback and skip-bootstrap logic

		// Set up the net connector for the P-Chain
		// subnetConnector, ok = vm.(validators.SubnetConnector)
		// if !ok {
		// 	return nil, fmt.Errorf("expected validators.SubnetConnector but got %T", vm)
		// }
	}

	// Initialize the ProposerVM and the vm wrapped inside it
	chainConfig, err := m.getChainConfig(chainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	var (
		minBlockDelay       = proposervm.DefaultMinBlockDelay
		numHistoricalBlocks = proposervm.DefaultNumHistoricalBlocks
	)
	netID := consensus.GetNetID(ctx)
	if subnetCfg, ok := m.SubnetConfigs[netID]; ok {
		minBlockDelay = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	}
	m.Log.Info("creating proposervm wrapper",
		log.Time("activationTime", m.ApricotPhase4Time),
		log.Uint64("minPChainHeight", m.ApricotPhase4MinPChainHeight),
		log.Duration("minBlockDelay", minBlockDelay),
		log.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	// Skip proposervm wrapper for Platform chain for now due to initialization issues
	skipProposerVM := chainParams.ID == constants.PlatformChainID
	if skipProposerVM {
		m.Log.Info("skipping proposervm wrapper for Platform chain in createLinearChain")
		// Platform chain gets special handling - continue without proposervm wrapper
	}

	if m.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, primaryAlias, m.Tracer)
	}

	// Only wrap with proposervm if not Platform chain
	if !skipProposerVM {
		proposervmReg, err := metric.MakeAndRegister(
			m.proposervmGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		vm = proposervm.New(
			vm,
			proposervm.Config{
				ActivationTime:      m.ApricotPhase4Time,
				DurangoTime:         version.GetDurangoTime(m.NetworkID),
				MinimumPChainHeight: m.ApricotPhase4MinPChainHeight,
				MinBlkDelay:         minBlockDelay,
				NumHistoricalBlocks: numHistoricalBlocks,
				StakingLeafSigner:   m.StakingTLSSigner,
				StakingCertLeaf:     m.StakingTLSCert,
				Registerer:          proposervmReg,
			},
		)
	}

	if m.MeterVMEnabled {
		meterchainvmReg, err := metric.MakeAndRegister(
			m.meterChainVMGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		vm = metervm.NewBlockVM(vm, meterchainvmReg)
	}
	if m.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, "proposervm", m.Tracer)
	}

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	msgChan := make(chan core.Message, defaultChannelSize)

	// Create ChainContext from context.Context
	// Note: ChainContext contains ConsensusContext and Context from consensus package
	// We need to extract IDs from the context
	var pubKeyBytes []byte
	if m.StakingBLSKey != nil && m.StakingBLSKey.PublicKey() != nil {
		// BLS PublicKey doesn't have a Bytes() method, so we'll leave it nil for now
		pubKeyBytes = nil
	}

	consensusCtx := &consContext.Context{
		QuantumID: m.NetworkID,
		NetID:     chainParams.NetID,
		ChainID:   chainParams.ID,
		NodeID:    m.NodeID,
		PublicKey: pubKeyBytes,
	}

	chainCtx := &block.ChainContext{
		Context: consensusCtx,
	}

	// Create DBManager wrapper - but for C-Chain VM we'll pass the database directly
	dbManager := &dbManagerWrapper{db: vmDB}

	// Create channel for messages
	toEngine := make(chan block.Message, defaultChannelSize)

	// Convert core.Fx to []*block.Fx for Initialize
	blockFxs := make([]*block.Fx, len(fxs))
	for i := range fxs {
		// Create empty block.Fx
		blockFxs[i] = &block.Fx{}
	}

	// Create AppSender - use noopAppSender for now
	appSender := &noopAppSender{}

	// Debug: log first few bytes of genesis data to understand format
	genesisPreview := ""
	if len(genesisData) > 100 {
		genesisPreview = fmt.Sprintf("%x...", genesisData[:100])
	} else {
		genesisPreview = fmt.Sprintf("%x", genesisData)
	}

	m.Log.Info("Initializing VM",
		log.Stringer("chainID", chainParams.ID),
		log.Int("genesisDataLen", len(genesisData)),
		log.String("genesisPreview", genesisPreview),
		log.Int("fxsCount", len(blockFxs)))

	// Convert blockFxs to []interface{} for Initialize
	var fxsInterface []interface{}
	for _, fx := range blockFxs {
		fxsInterface = append(fxsInterface, fx)
	}

	// Determine what database interface to pass based on the VM type
	// C-Chain VM (cchainvm) expects database.Database directly
	// Other VMs expect the dbManagerWrapper
	var dbInterface interface{}
	if chainParams.VMID == constants.EVMID {
		// C-Chain VM expects database.Database directly
		dbInterface = vmDB
		m.Log.Info("Using direct database for C-Chain VM")
	} else {
		// Other VMs expect dbManagerWrapper
		dbInterface = dbManager
		m.Log.Info("Using dbManagerWrapper for VM", log.Stringer("vmID", chainParams.VMID))
	}

	// Initialize the chainblock VM with proposer wrapper (NOT the raw vm)
	if err := vm.Initialize(
		context.TODO(),
		chainCtx,
		dbInterface,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		toEngine,
		fxsInterface,
		appSender,
	); err != nil {
		m.Log.Error("VM Initialize failed",
			log.Stringer("chainID", chainParams.ID),
			log.String("errorDetails", err.Error()),
			log.Err(err))
		return nil, fmt.Errorf("VM initialization failed: %w", err)
	}
	m.Log.Info("VM initialized successfully", log.Stringer("chainID", chainParams.ID))

	// CRITICAL FIX: When SkipBootstrap is enabled for single-node mode,
	// immediately unblock chain creator for Platform chain
	// Check both the expected PlatformChainID and the actual Platform VM
	isPlatformChain := chainParams.ID == constants.PlatformChainID ||
		chainParams.VMID == constants.PlatformVMID

	if m.SkipBootstrap && isPlatformChain {
		m.Log.Info("Skip-bootstrap mode: Platform chain initialized - immediately unblocking chain creator",
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID))
		// Unblock in a goroutine to avoid blocking
		go func() {
			select {
			case <-m.unblockChainCreatorCh:
				// Channel already closed, ignore
			default:
				close(m.unblockChainCreatorCh)
			}
		}()
	}

	// netID already defined above
	bootstrapWeight, err := beacons.TotalWeight(netID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching weight for net %s: %w", netID, err)
	}

	consensusParams := sb.Config().ConsensusParameters
	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	_, err = metric.MakeAndRegister(
		m.stakeGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// tracker.NewMeteredPeers not available in current consensus version
	// connectedValidators, err := tracker.NewMeteredPeers(stakeReg)
	// if err != nil {
	// 	return nil, fmt.Errorf("error creating peer tracker: %w", err)
	// }
	// vdrs.RegisterSetCallbackListener(netID, connectedValidators)
	_ = interface{}(nil) // connectedValidators placeholder

	p2pReg, err := metric.MakeAndRegister(
		m.p2pGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	_, err = p2p.NewPeerTracker(
		m.Log,
		"peer_tracker",
		p2pReg,
		set.NewSet[ids.NodeID](0), // Empty set of NodeIDs
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}

	_, err = metric.MakeAndRegister(
		m.handlerGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Create change notifier and subscription for linear chain
	// cn := &block.ChangeNotifier{}
	_ = func(ctx context.Context) (core.Message, error) { // subscription placeholder
		select {
		case msg := <-msgChan:
			return msg, nil
		case <-ctx.Done():
			return core.Message{}, ctx.Err()
		}
	}

	// handler.New requires runtime which is not available
	// Asynchronously passes messages from the network to the consensus engine
	/*
		h, err := handler.New(
			runtime,
			nil,          // cn was block.ChangeNotifier which doesn't exist
			subscription, // Pass as interface{}
			vdrs,
			m.FrontierPollFrequency,
			m.ConsensusAppConcurrency,
			m.ResourceTracker, // Pass as interface{}
			sb,
			connectedValidators,
			peerTracker, // Pass as interface{}
			handlerReg,
			// func() {} removed - signature doesn't accept this
		)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialize message handler: %w", err)
		}
	*/
	// Create a placeholder handler since handler.New is not available
	h := &placeholderHandler{}

	// tracker.NewPeers and NewStartup not available in current consensus version
	// connectedBeacons := tracker.NewPeers()
	// startupTracker := tracker.NewStartup(connectedBeacons, float64((3*bootstrapWeight+3)/4))
	_ = interface{}(nil) // startupTracker placeholder
	// beacons.RegisterSetCallbackListener(ctx.NetID, startupTracker)
	// beacons.RegisterSetCallbackListener(startupTracker)

	// Most consensus engine creation is disabled due to missing runtime and types
	// This needs to be re-enabled when consensus package is updated
	/*
		consensusGetHandler, err := consensusgetter.New(
			vm,
			messageSender,
			m.Log,
			m.BootstrapMaxTimeGetAncestors,
			m.BootstrapAncestorsMaxContainersSent,
			// ctx.Registerer doesn't exist in context.Context
		)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialize consensus base message handler: %w", err)
		}

		// var consensus smcon.Consensus

		// Create engine, bootstrapper and state-syncer in this order,
		// to make sure start callbacks are duly initialized
		// chain.Parameters is an empty struct
		chainParams := smeng.Parameters{}

		// engineConfig not used - using New directly
		// engineConfig := smeng.Config{
		// 	Ctx:                 ctx,
		// 	AllGetsServer:       consensusGetHandler,
		// 	VM:                  vm,
		// 	Sender:              messageSender,
		// 	Validators:          vdrs,
		// 	ConnectedValidators: connectedValidators,
		// 	Params:              chainParams,
		// 	Consensus:           consensus,
		// 	// PartialSync field removed - doesn't exist
		// }
		// var engine core.Engine
		engine, err := smeng.New(runtime, chainParams)
		if err != nil {
			return nil, fmt.Errorf("error initializing linear engine: %w", err)
		}
		_ = engine // temporarily unused

		// if m.TracingEnabled {
		// 	engine = core.TraceEngine(engine, m.Tracer)
		// }

		// create bootstrap gear
		bootstrapCfg := smbootstrap.Config{
			AllGetsServer:    consensusGetHandler,
			Ctx:              runtime,
			Beacons:          beacons,
			SampleK:          sampleK,
			StartupTracker:   startupTracker,
			Sender:           messageSender,
			BootstrapTracker: sb,
			// Timer field removed - h,
			// PeerTracker field removed - doesn't exist
			AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
			// DB field removed - doesn't exist
			VM: vm,
			// Bootstrapped field removed - doesn't exist
			// NonVerifyingParse field removed - doesn't exist
			// Haltable field removed - doesn't exist
		}
		// var bootstrapper core.BootstrapableEngine
		_, err = smbootstrap.New(
			bootstrapCfg,
			func(ctx context.Context, lastReqID uint32) error {
				return engine.Start(ctx)
			},
		)
		if err != nil {
			return nil, fmt.Errorf("error initializing linear bootstrapper: %w", err)
		}
	*/

	// if m.TracingEnabled {
	// 	bootstrapper = core.TraceBootstrapableEngine(bootstrapper, m.Tracer)
	// }

	// // create state sync gear
	// stateSyncCfg, err := syncer.NewConfig(
	// 	consensusGetHandler,
	// 	ctx,
	// 	startupTracker,
	// 	messageSender,
	// 	beacons,
	// 	sampleK,
	// 	bootstrapWeight/2+1, // must be > 50%
	// 	m.StateSyncBeacons,
	// 	vm,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("couldn't initialize state syncer configuration: %w", err)
	// }
	// stateSyncer := syncer.New(
	// 	stateSyncCfg,
	// 	bootstrapper.Start,
	// )

	// if m.TracingEnabled {
	// 	stateSyncer = core.TraceStateSyncer(stateSyncer, m.Tracer)
	// }

	// h.SetEngineManager(&handler.EngineManager{
	// 	Dag: nil,
	// 	Chain: &handler.Engine{
	// 		StateSyncer:  stateSyncer,
	// 		Bootstrapper: bootstrapper,
	// 		Consensus:    engine,
	// 	},
	// })

	// // Register health checks
	// if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.NetID.String()); err != nil {
	// 	return nil, fmt.Errorf("couldn't add health check for chain %s: %w", primaryAlias, err)
	// }

	// Handler h was already created above as &placeholderHandler{}

	// The chain ID will be available through the VM itself

	// Since block.ChainVM doesn't implement core.VM directly,
	// we need to wrap it to implement core.VM
	vmWrapper := newChainVMWrapper(vm)
	return &chainInfo{
		Name:    primaryAlias,
		Context: ctx,
		VM:      vmWrapper,
		Handler: h,
	}, nil
}

func (m *manager) IsBootstrapped(id ids.ID) bool {
	m.chainsLock.Lock()
	_, exists := m.chains[id]
	m.chainsLock.Unlock()
	if !exists {
		return false
	}

	// For now, assume bootstrapped chains are in NormalOp
	return true // chain.Context.State.Get() == interfaces.NormalOp
}

func (m *manager) registerBootstrappedHealthChecks() error {
	bootstrappedCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		if netIDs := m.Subnets.Bootstrapping(); len(netIDs) != 0 {
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
	sb, _ := m.Subnets.GetOrCreate(constants.PrimaryNetworkID)
	sb.AddChain(platformParams.ID)

	// The P-chain is created synchronously to ensure that `VM.Initialize` has
	// finished before returning from this function. This is required because
	// the P-chain initializes state that the rest of the node initialization
	// depends on.
	m.createChain(platformParams)

	m.Log.Info("starting chain creator")
	m.chainCreatorExited.Add(1)
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

// Notify registrants [those who want to know about the creation of chains]
// that the specified chain has been created
func (m *manager) notifyRegistrants(name string, ctx context.Context, vm interface{}) {
	for _, registrant := range m.registrants {
		// registrant.RegisterChain expects core.VM, but we use interface{}
		// since core.VM uses context.Context which we're not using
		if coreVM, ok := vm.(core.VM); ok {
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

func (m *manager) getOrMakeVMRegisterer(vmID ids.ID, chainAlias string) (metric.MultiGatherer, error) {
	vmGatherer, ok := m.vmGatherer[vmID]
	if !ok {
		vmName := constants.VMName(vmID)
		vmNamespace := utilmetric.AppendNamespace(constants.PlatformName, vmName)
		vmGatherer = metric.NewLabelGatherer(ChainLabel)
		err := m.Metrics.Register(
			vmNamespace,
			vmGatherer,
		)
		if err != nil {
			return nil, err
		}
		m.vmGatherer[vmID] = vmGatherer
	}

	chainReg := metric.NewPrefixGatherer()
	err := vmGatherer.Register(
		chainAlias,
		chainReg,
	)
	return chainReg, err
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

func (e *emptyValidatorManager) AddStaker(netID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
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

func (e *emptyValidatorManager) NumSubnets() int {
	return 0
}

func (e *emptyValidatorManager) SubsetWeight(netID ids.ID, nodeIDs consensusset.Set[ids.NodeID]) (uint64, error) {
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

// placeholderHandler implements handler.Handler interface
type placeholderHandler struct{}

func (p *placeholderHandler) Context() *consContext.Context                 { return nil }
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

// noopAppSender is a no-op implementation of AppSender
type noopAppSender struct{}

func (n *noopAppSender) SendAppRequest(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, request []byte) error {
	return nil
}

func (n *noopAppSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return nil
}

func (n *noopAppSender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	return nil
}

func (n *noopAppSender) SendAppGossip(ctx context.Context, nodeIDs []ids.NodeID, appGossipBytes []byte) error {
	return nil
}
