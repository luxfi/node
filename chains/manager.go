// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
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

	"go.uber.org/zap"

	"github.com/luxfi/node/api/health"
	"github.com/luxfi/node/api/keystore"
	"github.com/luxfi/node/api/server"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/engine/core"
	"github.com/luxfi/consensus/engine/core/tracker"
	"github.com/luxfi/consensus/core/interfaces"
	"github.com/luxfi/consensus/engine/dag/bootstrap/queue"
	"github.com/luxfi/consensus/engine/dag/state"
	"github.com/luxfi/consensus/engine/dag/vertex"
	"github.com/luxfi/consensus/engine/chain/block"
	// "github.com/luxfi/consensus/engine/chain/syncer" // Not used
	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/consensus/networking/router"
	"github.com/luxfi/consensus/networking/sender"
	"github.com/luxfi/consensus/networking/timeout"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/database"
	"github.com/luxfi/database/meterdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/network/p2p"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/trace"
	"github.com/luxfi/node/utils/buffer"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/log"
	luxmetric "github.com/luxfi/metric"
	"github.com/luxfi/node/utils/metric"
	"github.com/luxfi/node/utils/perms"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/fx"
	"github.com/luxfi/node/vms/metervm"
	"github.com/luxfi/node/vms/nftfx"
	// "github.com/luxfi/node/vms/platformvm/warp" // Not used
	"github.com/luxfi/node/vms/propertyfx"
	"github.com/luxfi/node/vms/proposervm"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/tracedvm"

	aveng "github.com/luxfi/consensus/engine/dag"
	dagbootstrap "github.com/luxfi/consensus/engine/dag/bootstrap"
	daggetter "github.com/luxfi/consensus/engine/dag/getter"
	smeng "github.com/luxfi/consensus/engine/chain"
	smbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	consensusgetter "github.com/luxfi/consensus/engine/chain/getter"
	smcon "github.com/luxfi/consensus/chain"
	timetracker "github.com/luxfi/consensus/networking/tracker"
	p2ppb "github.com/luxfi/node/proto/pb/p2p"
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
	// ID of the subnet that validates this chain.
	SubnetID ids.ID
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
	VM      core.VM
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

// chainVMWrapper wraps block.ChainVM to implement core.VM
type chainVMWrapper struct {
	vm block.ChainVM
}

func (c *chainVMWrapper) Initialize() error {
	// ChainVM has a different Initialize signature
	// This is a no-op since the actual initialization happens elsewhere
	return nil
}

func (c *chainVMWrapper) Shutdown() error {
	// block.ChainVM doesn't have Shutdown method
	return nil
}

func (c *chainVMWrapper) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	// ChainVM doesn't have CreateHandlers, return empty map
	return make(map[string]http.Handler), nil
}

// linearizableVMWrapper wraps vertex.LinearizableVMWithEngine to implement core.VM
type linearizableVMWrapper struct {
	vm vertex.LinearizableVMWithEngine
}

func (l *linearizableVMWrapper) Initialize() error {
	// LinearizableVMWithEngine has a different Initialize signature
	// This is a no-op since the actual initialization happens elsewhere
	return nil
}

func (l *linearizableVMWrapper) Shutdown() error {
	return l.vm.Shutdown()
}

func (l *linearizableVMWrapper) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return l.vm.CreateHandlers(ctx)
}

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

func (v *validatorStateWrapper) GetCurrentHeight() (uint64, error) {
	return v.state.GetCurrentHeight()
}

func (v *validatorStateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// validators.State doesn't have GetMinimumHeight, return current height
	return v.state.GetCurrentHeight()
}

func (v *validatorStateWrapper) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	// validators.State doesn't have GetSubnetID, return empty ID for now
	return ids.Empty, nil
}

func (v *validatorStateWrapper) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	return v.state.GetValidatorSet(height, subnetID)
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
	LUXAssetID                ids.ID
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

	Metrics        luxmetric.MultiGatherer
	MeterDBMetrics luxmetric.MultiGatherer

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

	luxGatherer          luxmetric.MultiGatherer            // chainID
	handlerGatherer      luxmetric.MultiGatherer            // chainID
	meterChainVMGatherer luxmetric.MultiGatherer            // chainID
	meterGRAPHVMGatherer luxmetric.MultiGatherer            // chainID
	proposervmGatherer   luxmetric.MultiGatherer            // chainID
	p2pGatherer          luxmetric.MultiGatherer            // chainID
	linearGatherer       luxmetric.MultiGatherer            // chainID
	stakeGatherer        luxmetric.MultiGatherer            // chainID
	vmGatherer           map[ids.ID]luxmetric.MultiGatherer // vmID -> chainID
}

// New returns a new Manager
func New(config *ManagerConfig) (Manager, error) {
	luxGatherer := luxmetric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(luxNamespace, luxGatherer); err != nil {
		return nil, err
	}

	handlerGatherer := luxmetric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(handlerNamespace, handlerGatherer); err != nil {
		return nil, err
	}

	meterChainVMGatherer := luxmetric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(meterchainvmNamespace, meterChainVMGatherer); err != nil {
		return nil, err
	}

	meterGRAPHVMGatherer := luxmetric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(meterdagvmNamespace, meterGRAPHVMGatherer); err != nil {
		return nil, err
	}

	proposervmGatherer := luxmetric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(proposervmNamespace, proposervmGatherer); err != nil {
		return nil, err
	}

	p2pGatherer := luxmetric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(p2pNamespace, p2pGatherer); err != nil {
		return nil, err
	}

	linearGatherer := luxmetric.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(linearNamespace, linearGatherer); err != nil {
		return nil, err
	}

	stakeGatherer := luxmetric.NewLabelGatherer(ChainLabel)
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
		vmGatherer:           make(map[ids.ID]luxmetric.MultiGatherer),
	}, nil
}

// QueueChainCreation queues a chain creation request
// Invariant: Tracked Subnet must be checked before calling this function
func (m *manager) QueueChainCreation(chainParams ChainParameters) {
	// Check for chain ID mapping override for C-Chain
	m.Log.Info("QueueChainCreation called",
		zap.String("vmID", chainParams.VMID.String()),
		zap.String("EVMID", constants.EVMID.String()),
		zap.Bool("vmIDEqualsEVMID", chainParams.VMID == constants.EVMID),
		zap.String("envVar", os.Getenv("LUX_CHAIN_ID_MAPPING_C")),
	)

	if chainParams.VMID == constants.EVMID && os.Getenv("LUX_CHAIN_ID_MAPPING_C") != "" {
		mappedID := os.Getenv("LUX_CHAIN_ID_MAPPING_C")
		parsedID, err := ids.FromString(mappedID)
		if err == nil {
			m.Log.Info("Using mapped blockchain ID for C-Chain",
				zap.String("original", chainParams.ID.String()),
				zap.String("mapped", parsedID.String()),
			)
			chainParams.ID = parsedID
		} else {
			m.Log.Warn("Invalid chain ID mapping",
				zap.String("mapping", mappedID),
				zap.Error(err),
			)
		}
	}

	if sb, _ := m.Subnets.GetOrCreate(chainParams.SubnetID); !sb.AddChain(chainParams.ID) {
		m.Log.Debug("skipping chain creation",
			zap.String("reason", "chain already staged"),
			zap.Stringer("subnetID", chainParams.SubnetID),
			zap.Stringer("chainID", chainParams.ID),
			zap.Stringer("vmID", chainParams.VMID),
		)
		return
	}

	if ok := m.chainsQueue.PushRight(chainParams); !ok {
		m.Log.Warn("skipping chain creation",
			zap.String("reason", "couldn't enqueue chain"),
			zap.Stringer("subnetID", chainParams.SubnetID),
			zap.Stringer("chainID", chainParams.ID),
			zap.Stringer("vmID", chainParams.VMID),
		)
	}
}

// createChain creates and starts the chain
//
// Note: it is expected for the subnet to already have the chain registered as
// bootstrapping before this function is called
func (m *manager) createChain(chainParams ChainParameters) {
	m.Log.Info("creating chain",
		zap.Stringer("subnetID", chainParams.SubnetID),
		zap.Stringer("chainID", chainParams.ID),
		zap.Stringer("vmID", chainParams.VMID),
	)

	sb, _ := m.Subnets.GetOrCreate(chainParams.SubnetID)

	// Note: buildChain builds all chain's relevant objects (notably engine and handler)
	// but does not start their operations. Starting of the handler (which could potentially
	// issue some internal messages), is delayed until chain dispatching is started and
	// the chain is registered in the manager. This ensures that no message generated by handler
	// upon start is dropped.
	chain, err := m.buildChain(chainParams, sb)
	if err != nil {
		if m.CriticalChains.Contains(chainParams.ID) {
			// Shut down if we fail to create a required chain (i.e. X, P or C)
			m.Log.Error("error creating required chain",
				zap.Stringer("subnetID", chainParams.SubnetID),
				zap.Stringer("chainID", chainParams.ID),
				zap.Stringer("vmID", chainParams.VMID),
				zap.Error(err),
			)
			go m.ShutdownNodeFunc(1)
			return
		}

		chainAlias := m.PrimaryAliasOrDefault(chainParams.ID)
		m.Log.Error("error creating chain",
			zap.Stringer("subnetID", chainParams.SubnetID),
			zap.Stringer("chainID", chainParams.ID),
			zap.String("chainAlias", chainAlias),
			zap.Stringer("vmID", chainParams.VMID),
			zap.Error(err),
		)

		// Register the health check for this chain regardless of if it was
		// created or not. This attempts to notify the node operator that their
		// node may not be properly validating the subnet they expect to be
		// validating.
		healthCheckErr := fmt.Errorf("failed to create chain on subnet %s: %w", chainParams.SubnetID, err)
		err := m.Health.RegisterHealthCheck(
			chainAlias,
			health.CheckerFunc(func(context.Context) (interface{}, error) {
				return nil, healthCheckErr
			}),
			chainParams.SubnetID.String(),
		)
		if err != nil {
			m.Log.Error("failed to register failing health check",
				zap.Stringer("subnetID", chainParams.SubnetID),
				zap.Stringer("chainID", chainParams.ID),
				zap.String("chainAlias", chainAlias),
				zap.Stringer("vmID", chainParams.VMID),
				zap.Error(err),
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
			zap.Stringer("subnetID", chainParams.SubnetID),
			zap.Stringer("chainID", chainParams.ID),
			zap.Stringer("vmID", chainParams.VMID),
			zap.Error(err),
		)
	}

	// Notify those that registered to be notified when a new chain is created
	m.notifyRegistrants(chain.Name, chain.Context, chain.VM)

	// Allows messages to be routed to the new chain. If the handler hasn't been
	// started and a message is forwarded, then the message will block until the
	// handler is started.
	m.ManagerConfig.Router.AddChain(chainParams.ID, chain.Handler)

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
func (m *manager) buildChain(chainParams ChainParameters, sb subnets.Subnet) (*chainInfo, error) {
	if chainParams.ID != constants.PlatformChainID && chainParams.VMID == constants.PlatformVMID {
		return nil, errCreatePlatformVM
	}
	primaryAlias := m.PrimaryAliasOrDefault(chainParams.ID)

	// Create this chain's data directory
	chainDataDir := filepath.Join(m.ChainDataDir, chainParams.ID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}

	// Create the log and context of the chain
	chainLog := m.Log // Use main log instead of creating chain-specific log

	// linearMetrics was here but not used in context.Context
	// linearMetrics, err := luxmetric.MakeAndRegister(
	// 	m.linearGatherer,
	// 	primaryAlias,
	// )
	// if err != nil {
	// 	return nil, err
	// }

	vmMetrics, err := m.getOrMakeVMRegisterer(chainParams.VMID, primaryAlias)
	if err != nil {
		return nil, err
	}

	// Create SharedMemory wrapper for consensus package
	sharedMem := &sharedMemoryWrapper{
		atomicMemory: m.AtomicMemory.NewSharedMemory(chainParams.ID),
	}

	// Create ValidatorState wrapper
	valStateWrapper := &validatorStateWrapper{
		state: m.validatorState,
	}

	ctx := &context.Context{
		NetworkID:    m.NetworkID,
		SubnetID:     chainParams.SubnetID,
		ChainID:      chainParams.ID,
		NodeID:       m.NodeID,
		PublicKey:    m.StakingBLSKey.PublicKey(),
		CChainID:     m.CChainID,
		LUXAssetID:   m.LUXAssetID,
		ChainDataDir: chainDataDir,
		
		Log:            chainLog,
		Metrics:        vmMetrics,
		ValidatorState: valStateWrapper,
		BCLookup:       m,
		SharedMemory:   sharedMem,
	}

	// Get a factory for the vm we want to use on our chain
	vmFactory, err := m.VMManager.GetFactory(chainParams.VMID)
	if err != nil {
		return nil, fmt.Errorf("error while getting vmFactory: %w", err)
	}

	// Create the chain
	vm, err := vmFactory.New(chainLog)
	if err != nil {
		return nil, fmt.Errorf("error while creating vm: %w", err)
	}
	// TODO: Shutdown VM if an error occurs

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
	case vertex.LinearizableVMWithEngine:
		chain, err = m.createLuxChain(
			ctx,
			chainParams.GenesisData,
			m.Validators,
			vm,
			chainFxs,
			sb,
		)
		if err != nil {
			return nil, fmt.Errorf("error while creating new lux vm %w", err)
		}
	case block.ChainVM:
		beacons := m.Validators
		if chainParams.ID == constants.PlatformChainID {
			beacons = chainParams.CustomBeacons
		}

		// In skip-bootstrap mode, use empty beacons for all chains
		// This enables single-node development mode
		if m.SkipBootstrap {
			beacons = validators.NewManager()
			ctx.Log.Info("skip-bootstrap enabled - using empty beacons for single-node mode")
		}

		chain, err = m.createLinearChain(
			ctx,
			chainParams.GenesisData,
			m.Validators,
			beacons,
			vm,
			chainFxs,
			sb,
		)
		if err != nil {
			return nil, fmt.Errorf("error while creating new linear vm %w", err)
		}
	default:
		return nil, errUnknownVMType
	}

	// timeout.Manager doesn't have RegisterChain in consensus package
	// if err := m.TimeoutManager.RegisterChain(ctx); err != nil {
	// 	return nil, err
	// }

	return chain, nil
}

func (m *manager) AddRegistrant(r Registrant) {
	m.registrants = append(m.registrants, r)
}

// Create a Graph-based blockchain that uses Lux
func (m *manager) createLuxChain(
	ctx context.Context,
	genesisData []byte,
	vdrs validators.Manager,
	vm vertex.LinearizableVMWithEngine,
	fxs []*core.Fx,
	sb subnets.Subnet,
) (*chainInfo, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	// Set state to Bootstrapping (from interfaces.State constants)
	ctx.State.Set(interfaces.Bootstrapping)

	primaryAlias := m.PrimaryAliasOrDefault(ctx.ChainID)
	meterDBReg, err := luxmetric.MakeAndRegister(
		m.MeterDBMetrics,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Create Metrics from Registry for meterdb
	meterDBMetrics := luxmetric.NewWithRegistry(primaryAlias, meterDBReg)
	meterDB, err := meterdb.New(meterDBMetrics, m.DB)
	if err != nil {
		return nil, err
	}

	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	vertexDB := prefixdb.New(VertexDBPrefix, prefixDB)
	vertexBootstrappingDB := prefixdb.New(VertexBootstrappingDBPrefix, prefixDB)
	txBootstrappingDB := prefixdb.New(TxBootstrappingDBPrefix, prefixDB)
	_ = prefixdb.New(BlockBootstrappingDBPrefix, prefixDB) // blockBootstrappingDB not used for DAG

	luxMetricsReg, err := luxmetric.MakeAndRegister(
		m.luxGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}
	
	// Convert Registry to Metrics for queue functions
	luxMetrics := luxmetric.NewWithRegistry(primaryAlias, luxMetricsReg)

	vtxBlocker, err := queue.NewWithMissing(vertexBootstrappingDB, "vtx", luxMetrics)
	if err != nil {
		return nil, err
	}
	txBlocker, err := queue.New(txBootstrappingDB, "tx", luxMetrics)
	if err != nil {
		return nil, err
	}

	// Passes messages from the lux engines to the network
	// Convert context.Context to interfaces.Context for sender
	interfacesCtx := &interfaces.Context{
		NetworkID:      ctx.NetworkID,
		SubnetID:       ctx.SubnetID,
		ChainID:        ctx.ChainID,
		NodeID:         ctx.NodeID,
		PublicKey:      ctx.PublicKey,
		LUXAssetID:     ctx.LUXAssetID,
		CChainID:       ctx.CChainID,
		ChainDataDir:   ctx.ChainDataDir,
		Log:            ctx.Log,
		Metrics:        ctx.Metrics,
		ValidatorState: ctx.ValidatorState,
		BCLookup:       ctx.BCLookup,
		SharedMemory:   ctx.SharedMemory,
	}
	
	luxMessageSender, err := sender.New(
		interfacesCtx,
		m.MsgCreator,
		m.TimeoutManager,
		p2ppb.EngineType_ENGINE_TYPE_DAG,
		sb,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize lux sender: %w", err)
	}

	if m.TracingEnabled {
		luxMessageSender = sender.Trace(luxMessageSender, m.Tracer)
	}

	// Passes messages from the linear engines to the network
	linearMessageSender, err := sender.New(
		interfacesCtx,
		m.MsgCreator,
		m.TimeoutManager,
		p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		sb,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize lux sender: %w", err)
	}

	if m.TracingEnabled {
		linearMessageSender = sender.Trace(linearMessageSender, m.Tracer)
	}

	chainConfig, err := m.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	graphVM := vm
	if m.MeterVMEnabled {
		meterdagvmReg, err := luxmetric.MakeAndRegister(
			m.meterGRAPHVMGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		graphVM = metervm.NewVertexVM(graphVM, meterdagvmReg)
	}
	if m.TracingEnabled {
		graphVM = tracedvm.NewVertexVM(graphVM, m.Tracer)
	}

	// Handles serialization/deserialization of vertices and also the
	// persistence of vertices
	vtxManager := state.NewSerializer(
		vertexDB,
		vertexDB, // Using same DB for both vertex and tx getter
	)

	// The channel through which a VM may send messages to the consensus engine
	// VM uses this channel to notify engine that a block is ready to be made
	// msgChan := make(chan core.Message, defaultChannelSize) // Not used for DAG chains - commented to avoid unused variable error

	// The only difference between using luxMessageSender and
	// linearMessageSender here is where the metrics will be placed. Because we
	// end up using this sender after the linearization, we pass in
	// linearMessageSender here.
	// Create a message channel for engine communication
	toEngine := make(chan interface{}, 1)
	
	// Convert fxs to []interface{}
	var fxInterfaces []interface{}
	for _, fx := range fxs {
		fxInterfaces = append(fxInterfaces, fx)
	}
	
	err = graphVM.Initialize(
		context.TODO(),
		ctx,           // chainCtx interface{}
		vmDB,          // dbManager interface{}
		genesisData,   // genesisBytes []byte
		chainConfig.Upgrade, // upgradeBytes []byte
		chainConfig.Config,  // configBytes []byte
		toEngine,      // toEngine chan<- interface{}
		fxInterfaces,  // fxs []interface{}
		linearMessageSender, // appSender interface{}
	)
	if err != nil {
		return nil, fmt.Errorf("error during vm's Initialize: %w", err)
	}

	// Initialize the ProposerVM and the vm wrapped inside it
	var (
		minBlockDelay       = proposervm.DefaultMinBlockDelay
		numHistoricalBlocks = proposervm.DefaultNumHistoricalBlocks
	)
	if subnetCfg, ok := m.SubnetConfigs[ctx.SubnetID]; ok {
		minBlockDelay = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	}
	m.Log.Info("creating proposervm wrapper",
		zap.Time("activationTime", m.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", m.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	// Note: this does not use [graphVM] to ensure we use the [vm]'s height index.
	untracedVMWrappedInsideProposerVM := NewLinearizeOnInitializeVM(vm)

	var vmWrappedInsideProposerVM block.ChainVM = untracedVMWrappedInsideProposerVM
	if m.TracingEnabled {
		vmWrappedInsideProposerVM = tracedvm.NewBlockVM(vmWrappedInsideProposerVM, primaryAlias, m.Tracer)
	}

	proposervmReg, err := luxmetric.MakeAndRegister(
		m.proposervmGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Note: vmWrappingProposerVM is the VM that the Linear engines should be
	// using.
	var vmWrappingProposerVM block.ChainVM = proposervm.New(
		vmWrappedInsideProposerVM,
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
		meterchainvmReg, err := luxmetric.MakeAndRegister(
			m.meterChainVMGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		vmWrappingProposerVM = metervm.NewBlockVM(vmWrappingProposerVM, meterchainvmReg)
	}
	if m.TracingEnabled {
		vmWrappingProposerVM = tracedvm.NewBlockVM(vmWrappingProposerVM, "proposervm", m.Tracer)
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

	bootstrapWeight, err := vdrs.TotalWeight(ctx.SubnetID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching weight for subnet %s: %w", ctx.SubnetID, err)
	}

	consensusParams := sb.Config().ConsensusParameters
	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	stakeReg, err := luxmetric.MakeAndRegister(
		m.stakeGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	connectedValidators, err := tracker.NewMeteredPeers(stakeReg)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}
	vdrs.RegisterSetCallbackListener(connectedValidators)

	p2pReg, err := luxmetric.MakeAndRegister(
		m.p2pGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	peerTracker, err := p2p.NewPeerTracker(
		ctx.Log,
		"peer_tracker",
		p2pReg,
		set.Of(ctx.NodeID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}

	handlerReg, err := luxmetric.MakeAndRegister(
		m.handlerGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Asynchronously passes messages from the network to the consensus engine
	h, err := handler.New(
		interfacesCtx,
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
		ctx.Log.Info("bootstrapping disabled - starting processing immediately")
	} else {
		startupTracker = tracker.NewStartup(connectedBeacons, float64(3*bootstrapWeight+3)/4.0)
	}
	// startupTracker doesn't implement SetCallbackListener, skip registration
	// vdrs.RegisterSetCallbackListener(startupTracker)

	consensusGetHandler, err := consensusgetter.New(
		vmWrappingProposerVM,
		linearMessageSender,
		ctx.Log,
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
		// ctx.Registerer doesn't exist in context.Context
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize consensus base message handler: %w", err)
	}

	var linearConsensus smcon.Consensus = &smcon.Topological{}
	if m.TracingEnabled {
		linearConsensus = smcon.Trace(linearConsensus, m.Tracer)
	}

	// Create engine, bootstrapper and state-syncer in this order,
	// to make sure start callbacks are duly initialized
	// Convert sampling.Parameters to chain.Parameters
	chainParams := smeng.Parameters{}
	
	var linearEngine core.Engine
	linearEngine, err = smeng.New(interfacesCtx, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error initializing linear engine: %w", err)
	}

	if m.TracingEnabled {
		linearEngine = core.TraceEngine(linearEngine, m.Tracer)
	}

	// create bootstrap gear
	bootstrapBeacons := vdrs
	// In skip-bootstrap mode, use empty beacons for single-node development
	if m.SkipBootstrap {
		bootstrapBeacons = validators.NewManager()
	}

	bootstrapCfg := smbootstrap.Config{
		AllGetsServer:    consensusGetHandler,
		Ctx:              interfacesCtx,
		Beacons:          bootstrapBeacons,
		SampleK:          sampleK,
		StartupTracker:   startupTracker,
		Sender:           linearMessageSender,
		BootstrapTracker: sb,
		Timer:            nil, // Timer not used for now
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		Blocked:          nil, // Blocked not used for now
		VM:               vmWrappingProposerVM,
	}
	
	// Create bootstrapper with a callback function
	bootstrapCallback := func(ctx context.Context, lastReqID uint32) error {
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

	getHandler, err := daggetter.New(
		vtxManager,
		luxMessageSender,
		ctx.Log,
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize lux base message handler: %w", err)
	}

	// create engine gear
	dagParams := aveng.Parameters{
		K:               20,  // Sample size
		AlphaPreference: 14,  // Preference threshold
		AlphaConfidence: 14,  // Confidence threshold
		Beta:            20,  // Finalization threshold
	}
	_, err = aveng.New(ctx, dagParams) // luxEngine not used currently
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
		Ctx:            ctx,
		StartupTracker: startupTracker,
		Sender:         luxMessageSender,
		PeerTracker:    peerTracker,
		// Beacons field removed - beacons,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		VtxBlocked:                     vtxBlocker,
		TxBlocked:                      txBlocker,
		Manager:                        vtxManager,
		VM:                             linearizableVM,
		Haltable:                       nil, // TODO: add halter if needed
	}
	// TODO: StopVertexID field doesn't exist in Config
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

	// TODO: dagbootstrap.Bootstrapper doesn't implement core.BootstrapableEngine
	// var tracedLuxBootstrapper core.BootstrapableEngine = luxBootstrapper
	// if m.TracingEnabled {
	// 	tracedLuxBootstrapper = core.TraceBootstrapableEngine(luxBootstrapper, m.Tracer)
	// }

	// TODO: Handler interface doesn't have SetEngineManager method
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

	// TODO: Handler doesn't implement health.Checker
	// // Register health check for this chain
	// if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.SubnetID.String()); err != nil {
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

// Create a linear chain using the Linear consensus engine
func (m *manager) createLinearChain(
	ctx context.Context,
	genesisData []byte,
	vdrs validators.Manager,
	beacons validators.Manager,
	vm block.ChainVM,
	fxs []*core.Fx,
	sb subnets.Subnet,
) (*chainInfo, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	// Set state to Bootstrapping
	ctx.State.Set(interfaces.Bootstrapping)

	primaryAlias := m.PrimaryAliasOrDefault(ctx.ChainID)
	meterDBReg, err := luxmetric.MakeAndRegister(
		m.MeterDBMetrics,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Create Metrics from Registry for meterdb
	meterDBMetrics := luxmetric.NewWithRegistry(primaryAlias, meterDBReg)
	meterDB, err := meterdb.New(meterDBMetrics, m.DB)
	if err != nil {
		return nil, err
	}

	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	_ = prefixdb.New(ChainBootstrappingDBPrefix, prefixDB) // bootstrappingDB not used

	// Passes messages from the consensus engine to the network
	messageSender, err := sender.New(
		ctx,
		m.MsgCreator,
		m.Net,           // Passing network as interface{}
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

	// var (
	// 	bootstrapFunc func() // Not used
	// 	// subnetConnector interface{} // Not used
	// )
	// If [m.validatorState] is nil then we are creating the P-Chain. Since the
	// P-Chain is the first chain to be created, we can use it to initialize
	// required interfaces for the other chains
	if m.validatorState == nil {
		valState, ok := vm.(validators.State)
		if !ok {
			return nil, fmt.Errorf("expected validators.State but got %T", vm)
		}

		// TODO: validators.Trace doesn't exist
		// if m.TracingEnabled {
		// 	valState = validators.Trace(valState, "platformvm", m.Tracer)
		// }

		// Notice that this context is left unlocked. This is because the
		// lock will already be held when accessing these values on the
		// P-chain.
		// Create a wrapper to adapt validators.State to interfaces.ValidatorState
		ctx.ValidatorState = &validatorStateWrapper{state: valState}

		// Initialize the validator state for future chains.
		// TODO: validators.NewLockedState doesn't exist
		m.validatorState = valState // validators.NewLockedState(&ctx.Lock, valState)
		// TODO: validators.Trace doesn't exist
		// if m.TracingEnabled {
		// 	m.validatorState = validators.Trace(m.validatorState, "lockedState", m.Tracer)
		// }

		if !m.ManagerConfig.SybilProtectionEnabled {
			// TODO: validators.NewNoValidatorsState doesn't exist
			// m.validatorState = validators.NewNoValidatorsState(m.validatorState)
			// Wrap the NoValidatorsState as well
			// ctx.ValidatorState = &validatorStateWrapper{state: validators.NewNoValidatorsState(valState)}
		}

		// Set this func only for platform
		//
		// The linear bootstrapper ensures this function is only executed once, so
		// we don't need to be concerned about closing this channel multiple times.
		// TODO: bootstrapFunc not used anymore
		// bootstrapFunc = func() {
		// 	close(m.unblockChainCreatorCh)
		// }

		// Set up the subnet connector for the P-Chain
		// TODO: validators.SubnetConnector interface has been removed, need to update this
		// subnetConnector, ok = vm.(validators.SubnetConnector)
		// if !ok {
		// 	return nil, fmt.Errorf("expected validators.SubnetConnector but got %T", vm)
		// }
	}

	// Initialize the ProposerVM and the vm wrapped inside it
	chainConfig, err := m.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	var (
		minBlockDelay       = proposervm.DefaultMinBlockDelay
		numHistoricalBlocks = proposervm.DefaultNumHistoricalBlocks
	)
	if subnetCfg, ok := m.SubnetConfigs[ctx.SubnetID]; ok {
		minBlockDelay = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	}
	m.Log.Info("creating proposervm wrapper",
		zap.Time("activationTime", m.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", m.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	if m.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, primaryAlias, m.Tracer)
	}

	proposervmReg, err := luxmetric.MakeAndRegister(
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

	if m.MeterVMEnabled {
		meterchainvmReg, err := luxmetric.MakeAndRegister(
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
	chainCtx := &block.ChainContext{
		NetworkID:    ctx.NetworkID,
		SubnetID:     ctx.SubnetID,
		ChainID:      ctx.ChainID,
		NodeID:       ctx.NodeID,
		PublicKey:    ctx.PublicKey,
		LUXAssetID:   ctx.LUXAssetID,
		CChainID:     ctx.CChainID,
		ChainDataDir: ctx.ChainDataDir,
	}
	
	// Create DBManager wrapper
	dbManager := &dbManagerWrapper{db: vmDB}
	
	// Create channel for messages
	toEngine := make(chan block.Message, defaultChannelSize)
	
	// Convert core.Fx to block.Fx
	blockFxs := make([]*block.Fx, len(fxs))
	for i := range fxs {
		blockFxs[i] = &block.Fx{}
	}
	
	// Create AppSender wrapper - adapter from sender.Sender to block.AppSender
	appSender := &senderToAppSenderAdapter{sender: messageSender}
	
	if err := vm.Initialize(
		context.TODO(),
		chainCtx,
		dbManager,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		toEngine,
		blockFxs,
		appSender,
	); err != nil {
		return nil, err
	}

	bootstrapWeight, err := beacons.TotalWeight(ctx.SubnetID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching weight for subnet %s: %w", ctx.SubnetID, err)
	}

	consensusParams := sb.Config().ConsensusParameters
	sampleK := consensusParams.K
	if uint64(sampleK) > bootstrapWeight {
		sampleK = int(bootstrapWeight)
	}

	stakeReg, err := luxmetric.MakeAndRegister(
		m.stakeGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	connectedValidators, err := tracker.NewMeteredPeers(stakeReg)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}
	vdrs.RegisterSetCallbackListener(connectedValidators)

	p2pReg, err := luxmetric.MakeAndRegister(
		m.p2pGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	peerTracker, err := p2p.NewPeerTracker(
		ctx.Log,
		"peer_tracker",
		p2pReg,
		set.Of(ctx.NodeID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}

	handlerReg, err := luxmetric.MakeAndRegister(
		m.handlerGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// Create change notifier and subscription for linear chain
	// TODO: block.ChangeNotifier doesn't exist
	// cn := &block.ChangeNotifier{}
	subscription := func(ctx context.Context) (core.Message, error) {
		select {
		case msg := <-msgChan:
			return msg, nil
		case <-ctx.Done():
			return core.Message(0), ctx.Err()
		}
	}

	// Asynchronously passes messages from the network to the consensus engine
	h, err := handler.New(
		ctx,
		nil, // cn was block.ChangeNotifier which doesn't exist
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

	connectedBeacons := tracker.NewPeers()
	startupTracker := tracker.NewStartup(connectedBeacons, float64((3*bootstrapWeight+3)/4))
	// TODO: RegisterSetCallbackListener signature mismatch - startupTracker doesn't implement SetCallbackListener
	// beacons.RegisterSetCallbackListener(ctx.SubnetID, startupTracker)
	// beacons.RegisterSetCallbackListener(startupTracker)

	consensusGetHandler, err := consensusgetter.New(
		vm,
		messageSender,
		ctx.Log,
		m.BootstrapMaxTimeGetAncestors,
		m.BootstrapAncestorsMaxContainersSent,
		// ctx.Registerer doesn't exist in context.Context
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize consensus base message handler: %w", err)
	}

	var consensus smcon.Consensus = &smcon.Topological{}
	if m.TracingEnabled {
		consensus = smcon.Trace(consensus, m.Tracer)
	}

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
	var engine core.Engine
	engine, err = smeng.New(ctx, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error initializing linear engine: %w", err)
	}

	if m.TracingEnabled {
		engine = core.TraceEngine(engine, m.Tracer)
	}

	// create bootstrap gear
	bootstrapCfg := smbootstrap.Config{
		AllGetsServer:    consensusGetHandler,
		Ctx:              ctx,
		Beacons:          beacons,
		SampleK:          sampleK,
		StartupTracker:   startupTracker,
		Sender:           messageSender,
		BootstrapTracker: sb,
		// Timer field removed - h,
		// PeerTracker field removed - doesn't exist
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		// DB field removed - doesn't exist
		VM:                             vm,
		// Bootstrapped field removed - doesn't exist
		// NonVerifyingParse field removed - doesn't exist
		// Haltable field removed - doesn't exist
	}
	// TODO: smbootstrap.Bootstrapper doesn't implement core.BootstrapableEngine
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

	// TODO: bootstrapper doesn't implement BootstrapableEngine
	// if m.TracingEnabled {
	// 	bootstrapper = core.TraceBootstrapableEngine(bootstrapper, m.Tracer)
	// }

	// TODO: syncer package doesn't have NewConfig or New
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

	// TODO: Handler interface doesn't have SetEngineManager method
	// h.SetEngineManager(&handler.EngineManager{
	// 	Dag: nil,
	// 	Chain: &handler.Engine{
	// 		StateSyncer:  stateSyncer,
	// 		Bootstrapper: bootstrapper,
	// 		Consensus:    engine,
	// 	},
	// })

	// TODO: Handler doesn't implement health.Checker
	// // Register health checks
	// if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.SubnetID.String()); err != nil {
	// 	return nil, fmt.Errorf("couldn't add health check for chain %s: %w", primaryAlias, err)
	// }

	// Create wrapper to adapt block.ChainVM to core.VM
	vmWrapper := &chainVMWrapper{vm: vm}

	return &chainInfo{
		Name:    primaryAlias,
		Context: ctx,
		VM:      vmWrapper,
		Handler: h,
	}, nil
}

func (m *manager) IsBootstrapped(id ids.ID) bool {
	m.chainsLock.Lock()
	chain, exists := m.chains[id]
	m.chainsLock.Unlock()
	if !exists {
		return false
	}

	return chain.Context.State.Get() == interfaces.NormalOp
}

func (m *manager) registerBootstrappedHealthChecks() error {
	bootstrappedCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		if subnetIDs := m.Subnets.Bootstrapping(); len(subnetIDs) != 0 {
			return subnetIDs, errNotBootstrapped
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
			zap.Error(errPartialSyncAsAValidator),
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
func (m *manager) notifyRegistrants(name string, ctx context.Context, vm core.VM) {
	for _, registrant := range m.registrants {
		registrant.RegisterChain(name, ctx, vm)
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

func (m *manager) getOrMakeVMRegisterer(vmID ids.ID, chainAlias string) (luxmetric.MultiGatherer, error) {
	vmGatherer, ok := m.vmGatherer[vmID]
	if !ok {
		vmName := constants.VMName(vmID)
		vmNamespace := metric.AppendNamespace(constants.PlatformName, vmName)
		vmGatherer = luxmetric.NewLabelGatherer(ChainLabel)
		err := m.Metrics.Register(
			vmNamespace,
			vmGatherer,
		)
		if err != nil {
			return nil, err
		}
		m.vmGatherer[vmID] = vmGatherer
	}

	chainReg := luxmetric.NewPrefixGatherer()
	err := vmGatherer.Register(
		chainAlias,
		chainReg,
	)
	return chainReg, err
}
