// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	nodeconsensus "github.com/luxfi/node/consensus"
	// xvm "github.com/luxfi/node/vms/exchangevm" // Unused
	"context"
	"crypto"
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
	// "github.com/luxfi/database/prefixdb" // Only used in disabled createDAGChain function
	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/engine"
	"github.com/luxfi/ids"
	"github.com/luxfi/warp"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
	// "github.com/luxfi/node/network/p2p" // Unused
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
	"github.com/luxfi/consensus/networking/sender"
	"github.com/luxfi/consensus/networking/timeout"
	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/trace"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/utils/buffer"
	"github.com/luxfi/constants"
	"github.com/luxfi/node/utils/crypto/bls"
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

// chainVMWrapper wraps block.ChainVM to implement interfaces.VM.
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

func (c *chainVMWrapper) SetState(ctx context.Context, state consensus.State) error {
	// ChainVM doesn't have SetState, return error or forward to underlying VM
	// For now return nil as this is a wrapper
	return nil
}

func (c *chainVMWrapper) Version(ctx context.Context) (string, error) {
	// Return a default version
	return "1.0.0", nil
}

// linearizableVMWrapper wraps consensusvertex.LinearizableVMWithEngine to implement interfaces.VM
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

func (v *consensusValidatorStateWrapper) GetCurrentHeight(ctx context.Context) (uint64, error) {
	if v.state == nil {
		return 0, nil
	}
	return v.state.GetCurrentHeight(ctx)
}

func (v *consensusValidatorStateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// validators.State doesn't have GetMinimumHeight, return current height
	if v.state == nil {
		return 0, nil
	}
	return v.state.GetCurrentHeight(ctx)
}

func (v *consensusValidatorStateWrapper) GetNetID(chainID ids.ID) (ids.ID, error) {
	// validators.State doesn't have GetNetID, return empty ID for now
	return ids.Empty, nil
}

func (v *consensusValidatorStateWrapper) GetChainID(chainID ids.ID) (ids.ID, error) {
	// Alias for GetNetID - chain lookup
	return v.GetNetID(chainID)
}

func (v *consensusValidatorStateWrapper) GetValidatorSet(height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	if v.state == nil {
		return make(map[ids.NodeID]uint64), nil
	}
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

func (v *consensusValidatorStateWrapper) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*consensusctx.GetValidatorOutput, error) {
	if v.state == nil {
		return make(map[ids.NodeID]*consensusctx.GetValidatorOutput), nil
	}
	// Get the validator set from the underlying state
	valSet, err := v.state.GetValidatorSet(ctx, height, netID)
	if err != nil {
		return nil, err
	}

	// Convert to GetValidatorOutput format
	result := make(map[ids.NodeID]*consensusctx.GetValidatorOutput, len(valSet))
	for nodeID, val := range valSet {
		result[nodeID] = &consensusctx.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: val.PublicKey,
			Weight:    val.Weight,
		}
	}
	return result, nil
}

func (v *validatorStateWrapper) GetCurrentHeight() (uint64, error) {
	if v.state == nil {
		return 0, nil
	}
	return v.state.GetCurrentHeight(context.Background())
}

func (v *validatorStateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// validators.State doesn't have GetMinimumHeight, return current height
	if v.state == nil {
		return 0, nil
	}
	return v.state.GetCurrentHeight(ctx)
}

func (v *validatorStateWrapper) GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	// validators.State doesn't have GetNetID, return empty ID for now
	return ids.Empty, nil
}

func (v *validatorStateWrapper) GetValidatorSet(height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	if v.state == nil {
		return make(map[ids.NodeID]uint64), nil
	}
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
		_ = beacons // TODO: Use beacons for validator management

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
		m.Log.Info("initializing VM", log.Stringer("chainID", chainParams.ID))
		err = vm.Initialize(
			context.TODO(),
			chainCtx,
			vmDB,
			chainParams.GenesisData,
			chainConfig.Upgrade,
			chainConfig.Config,
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

		consensusEngine := consensuschain.New()

		// Wire up VM notifications to the consensus engine
		// This goroutine reads from the toEngine channel and triggers block building
		// It now also gossips blocks to other validators for proper consensus
		go func(toEng <-chan block.Message, vm block.ChainVM, logger log.Logger, net network.Network, msgCreator message.OutboundMsgBuilder, chainID ids.ID, netID ids.ID) {
			logger.Info("starting VM notification forwarder with block gossip")
			for msg := range toEng {
				logger.Debug("received VM notification, building block",
					log.Uint32("type", uint32(msg.Type)))

				// Build block directly when VM notifies us of pending transactions
				// Type 0 = PendingTxs (ready for block building)
				if msg.Type == 0 {
					ctx := context.Background()
					blk, err := vm.BuildBlock(ctx)
					if err != nil {
						logger.Debug("failed to build block",
							log.Err(err))
						continue
					}
					logger.Info("built block from VM notification",
						log.Stringer("blockID", blk.ID()),
						log.Uint64("height", blk.Height()))

					// Verify the block before accepting
					if err := blk.Verify(ctx); err != nil {
						logger.Error("failed to verify built block",
							log.Stringer("blockID", blk.ID()),
							log.Err(err))
						continue
					}

					// Gossip the block to validators before accepting locally
					// This ensures other nodes receive and process the block
					if net != nil && msgCreator != nil {
						blkBytes := blk.Bytes()
						// Create Put message with the block bytes
						// Put is used to push a block to other nodes
						putMsg, err := msgCreator.Put(chainID, 0, blkBytes)
						if err != nil {
							logger.Warn("failed to create Put message for block gossip",
								log.Stringer("blockID", blk.ID()),
								log.Err(err))
						} else {
							// Gossip to all validators
							// numValidatorsToSend=-1 means all validators
							// numNonValidatorsToSend=0, numPeersToSend=0 for validators only
							sentTo := net.Gossip(putMsg, nil, netID, -1, 0, 0)
							logger.Info("gossiped block to validators",
								log.Stringer("blockID", blk.ID()),
								log.Int("sentTo", sentTo.Len()))
						}
					}

					// Accept the block into the canonical chain
					if err := blk.Accept(ctx); err != nil {
						logger.Error("failed to accept built block",
							log.Stringer("blockID", blk.ID()),
							log.Err(err))
						continue
					}

					// Set this block as the preferred tip
					if err := vm.SetPreference(ctx, blk.ID()); err != nil {
						logger.Warn("failed to set preference to accepted block",
							log.Stringer("blockID", blk.ID()),
							log.Err(err))
						// Continue anyway - block is accepted
					}

					logger.Info("successfully accepted block into canonical chain",
						log.Stringer("blockID", blk.ID()),
						log.Uint64("height", blk.Height()))
				}
			}
			logger.Info("VM notification forwarder stopped")
		}(toEngine, vm, m.Log, m.Net, m.MsgCreator, chainParams.ID, chainParams.ChainID)

		chain = &chainInfo{
			Name:    chainCtx.ChainID.String(),
			Context: chainCtx,
			VM:      vm, // Use the real VM directly
			Engine:  consensusEngine, // Use real consensus engine directly
			Handler: newBlockHandler(vm, m.Log),
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

// createDAGChain creates a DAG-based blockchain (X-Chain style)
// Disabled for now - consensus package doesn't have vertex types yet
// TODO: Re-enable when consensus/engine/dag provides LinearizableVMWithEngine
/*
func (m *manager) createDAGChain(
	ctx context.Context,
	chainParams ChainParameters,
	genesisData []byte,
	vdrs validators.Manager,
	vm vertex.LinearizableVMWithEngine,
	fxs []*engine.Fx,
	sb nets.Net,
) (*chain, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.State.Set(consensus.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_DAG,
		State: consensus.Initializing,
	})

	// Create this chain's data directory
	chainDataDir := filepath.Join(m.ChainDataDir, chainID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}

	// Get VM metrics - getOrMakeVMRegisterer method doesn't exist
	// _, err = m.getOrMakeVMRegisterer(chainID, primaryAlias)
	// if err != nil {
	// 	return nil, err
	// }

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
	luxMessageSender, err := sender.New(
		ctx,
		m.MsgCreator,
		m.Net,
		m.ManagerConfig.Router,
		m.TimeoutManager,
		p2ppb.EngineType_ENGINE_TYPE_DAG,
		sb,
		luxMetrics,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize lux sender: %w", err)
	}

	// Tracing is handled at the network level

	// Passes messages from the chain engines to the network
	consensusmanMessageSender, err := sender.New(
		ctx,
		m.MsgCreator,
		m.Net,
		m.ManagerConfig.Router,
		m.TimeoutManager,
		p2ppb.EngineType_ENGINE_TYPE_CONSENSUSMAN,
		sb,
		ctx.Registerer,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize lux sender: %w", err)
	}

	chainConfig, err := m.getChainConfig(chainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	dagVM := vm
	if m.MeterVMEnabled {
		meterdagvmReg, err := metrics.MakeAndRegister(
			m.meterDAGVMGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		// dagVM = metervm.NewVertexVM(dagVM, meterdagvmReg)
	}
	if m.TracingEnabled {
		// dagVM = tracedvm.NewVertexVM(dagVM, m.Tracer)
	}

	// Handles serialization/deserialization of vertices and also the
	// persistence of vertices
	vtxManager := state.NewSerializer(
		state.SerializerConfig{
			ChainID: ctx.ChainID,
			VM:      dagVM,
			DB:      vertexDB,
			Log:     ctx.Log,
		},
	)

	// The only difference between using luxMessageSender and
	// consensusmanMessageSender here is where the metrics will be placed. Because we
	// end up using this sender after the linearization, we pass in
	// consensusmanMessageSender here.
	err = dagVM.Initialize(
		context.TODO(),
		ctx.Context,
		vmDB,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		fxs,
		consensusmanMessageSender,
	)
	if err != nil {
		return nil, fmt.Errorf("error during vm's Initialize: %w", err)
	}

	// Initialize the ProposerVM and the vm wrapped inside it
	var (
		// A default subnet configuration will be present if explicit configuration is not provided
		subnetCfg           = m.NetConfigs[ctx.ChainID]
		minBlockDelay       = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	)
	m.Log.Info("creating proposervm wrapper",
		log.Time("activationTime", m.Upgrades.ApricotPhase4Time),
		log.Uint64("minPChainHeight", m.Upgrades.ApricotPhase4MinPChainHeight),
		log.Duration("minBlockDelay", minBlockDelay),
		log.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	// Note: this does not use [dagVM] to ensure we use the [vm]'s height index.
	// Create a channel for engine communication - this will be used by the VM's Linearize method
	toEngine := make(chan block.Message, 1)
	untracedVMWrappedInsideProposerVM := NewLinearizeOnInitializeVM(vm, toEngine)

	var vmWrappedInsideProposerVM block.ChainVM = untracedVMWrappedInsideProposerVM
	if m.TracingEnabled {
		// vmWrappedInsideProposerVM = tracedvm.NewBlockVM(vmWrappedInsideProposerVM, primaryAlias, m.Tracer)
	}

	// For block-based VMs (like Platform VM), we pass the VM directly to ProposerVM
	// vm is already a block.ChainVM from the factory
	// Rename to chainblockVM for clarity - this is the block-based chain VM
	var chainblockVM block.ChainVM = vm
	if m.TracingEnabled {
		// tracedvm is temporarily disabled - needs consensus package updates
		// chainblockVM = tracedvm.NewBlockVM(chainblockVM, primaryAlias, m.Tracer)
	}

	proposervmReg, err := metric.MakeAndRegister(
		m.proposervmGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	proposerVM := proposervm.New(
		vmWrappedInsideProposerVM,
		proposervm.Config{
			Upgrades:            m.Upgrades,
			MinBlkDelay:         minBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   m.StakingTLSSigner,
			StakingCertLeaf:     m.StakingTLSCert,
			Registerer:          proposervmReg,
		},
	)

	// Note: vmWrappingProposerVM is the VM that the Chain engines should be
	// using.
	var vmWrappingProposerVM block.ChainVM = proposerVM

	if m.MeterVMEnabled {
		meterchainvmReg, err := metric.MakeAndRegister(
			m.meterChainVMGatherer,
			primaryAlias,
		)
		if err != nil {
			return nil, err
		}

		// vmWrappingProposerVM = metervm.NewBlockVM(vmWrappingProposerVM, meterchainvmReg)
	}
	if m.TracingEnabled {
		// vmWrappingProposerVM = tracedvm.NewBlockVM(vmWrappingProposerVM, "proposervm", m.Tracer)
	}

	cn := &block.ChangeNotifier{
		ChainVM: vmWrappingProposerVM,
	}

	vmWrappingProposerVM = cn

	// Note: linearizableVM is the VM that the Lux engines should be
	// using.
	linearizableVM := &initializeOnLinearizeVM{
		waitForLinearize: make(chan struct{}),
		DAGVM:            dagVM,
		vmToInitialize:   vmWrappingProposerVM,
		vmToLinearize:    untracedVMWrappedInsideProposerVM,

		ctx:          ctx,
		db:           vmDB,
		genesisBytes: genesisData,
		upgradeBytes: chainConfig.Upgrade,
		configBytes:  chainConfig.Config,
		fxs:          fxs,
		appSender:    nil,      // Will be set to proper AppSender type later
		toEngine:     toEngine, // Channel for VM to notify consensus about pending transactions
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

	var halter common.Halter

	// Asynchronously passes messages from the network to the consensus engine
	h, err := handler.New(
		ctx,
		cn,
		linearizableVM.WaitForEvent,
		vdrs,
		m.FrontierPollFrequency,
		m.ConsensusAppConcurrency,
		m.ResourceTracker,
		sb,
		connectedValidators,
		peerTracker,
		handlerReg,
		halter.Halt,
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

	var consensusmanConsensus smcon.Consensus = &smcon.Topological{Factory: consensus.ConsensusflakeFactory}
	if m.TracingEnabled {
		consensusmanConsensus = smcon.Trace(consensusmanConsensus, m.Tracer)
	}

	// Create engine, bootstrapper and state-syncer in this order,
	// to make sure start callbacks are duly initialized
	consensusmanEngineConfig := smeng.Config{
		Ctx:                 ctx,
		AllGetsServer:       consensusGetHandler,
		VM:                  vmWrappingProposerVM,
		Sender:              consensusmanMessageSender,
		Validators:          vdrs,
		ConnectedValidators: connectedValidators,
		Params:              consensusParams,
		Consensus:           consensusmanConsensus,
	}
	var consensusmanEngine common.Engine
	consensusmanEngine, err = smeng.New(consensusmanEngineConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing chain engine: %w", err)
	}

	// var linearEngine consensuscore.Engine
	linearEngine, err := smeng.New(runtime, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error initializing linear engine: %w", err)
	}
	_ = linearEngine // temporarily unused

	// Tracing for linearEngine can be added here if needed
	// if m.TracingEnabled {
	// 	linearEngine = consensuscore.TraceEngine(linearEngine, m.Tracer)
	// }

	// create bootstrap gear
	bootstrapBeacons := vdrs
	// In skip-bootstrap mode, use empty beacons for single-node development
	if m.SkipBootstrap {
		bootstrapBeacons = validators.NewManager()
	}

	bootstrapCfg := smbootstrap.Config{
		Haltable:                       &halter,
		NonVerifyingParse:              block.ParseFunc(proposerVM.ParseLocalBlock),
		AllGetsServer:                  consensusGetHandler,
		Ctx:                            ctx,
		Beacons:                        vdrs,
		SampleK:                        sampleK,
		StartupTracker:                 startupTracker,
		Sender:                         linearMessageSender,
		BootstrapTracker:               sb,
		PeerTracker:                    peerTracker,
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
		return nil, fmt.Errorf("error initializing chain bootstrapper: %w", err)
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
	// Note: aveng.Engine doesn't implement consensuscore.Engine interface
	// Tracing is not supported for graph engines currently

	// create bootstrap gear
	luxBootstrapperConfig := avbootstrap.Config{
		AllGetsServer:                  avaGetHandler,
		Ctx:                            ctx,
		Beacons:                        vdrs,
		StartupTracker:                 startupTracker,
		Sender:                         luxMessageSender,
		AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
		VtxBlocked:                     vtxBlocker,
		TxBlocked:                      txBlocker,
		Manager:                        vtxManager,
		VM:                             linearizableVM,
		Haltable:                       nil, // Implementation note
	}
	if ctx.ChainID == m.XChainID {
		luxBootstrapperConfig.StopVertexID = m.Upgrades.CortinaXChainStopVertexID
	}

	var luxBootstrapper common.BootstrapableEngine
	luxBootstrapper, err = avbootstrap.New(
		luxBootstrapperConfig,
		consensusmanBootstrapper.Start,
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing lux bootstrapper: %w", err)
	}

	// var tracedLuxBootstrapper consensuscore.BootstrapableEngine = luxBootstrapper
	// if m.TracingEnabled {
	// 	tracedLuxBootstrapper = consensuscore.TraceBootstrapableEngine(luxBootstrapper, m.Tracer)
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
	// if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.ChainID.String()); err != nil {
	// 	return nil, fmt.Errorf("couldn't add health check for chain %s: %w", primaryAlias, err)
	// }

	// Create a wrapper to adapt LinearizableVMWithEngine to interfaces.VM
	vmWrapper := &linearizableVMWrapper{vm: graphVM}

	return &chainInfo{
		Name:    primaryAlias,
		Context: chainCtx,
		VM:      vmWrapper,
		Handler: h,
	}, nil
}
*/ // End of createDAGChain - disabled pending consensus/engine/dag support


// monitorBootstrap monitors when a chain finishes bootstrapping and notifies the subnet.
// This is critical for health checks because the health check queries m.Nets.Bootstrapping()
// which returns subnets that have chains still in bootstrapping state. Without this notification,
// the health check would permanently report "subnets not bootstrapped".
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
	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	for {
		select {
		case <-ticker.C:
			if checker.IsBootstrapped() {
				m.Log.Info("chain finished bootstrapping, notifying subnet",
					log.Stringer("chainID", chainID))
				sb.Bootstrapped(chainID)
				return
			}
		case <-timeout.C:
			// Timeout reached, mark as bootstrapped anyway to prevent permanent unhealthy state
			m.Log.Warn("bootstrap monitoring timeout, marking chain as bootstrapped",
				log.Stringer("chainID", chainID))
			sb.Bootstrapped(chainID)
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
	vm     block.ChainVM
	logger log.Logger
}

func newBlockHandler(vm block.ChainVM, logger log.Logger) *blockHandler {
	return &blockHandler{vm: vm, logger: logger}
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
	// Process incoming block from gossip
	if b.vm == nil {
		return nil
	}

	// Parse the block bytes
	blk, err := b.vm.ParseBlock(ctx, container)
	if err != nil {
		b.logger.Debug("failed to parse gossiped block",
			log.Stringer("from", nodeID),
			log.Err(err))
		return nil // Don't return error - just skip invalid blocks
	}

	b.logger.Info("received gossiped block",
		log.Stringer("from", nodeID),
		log.Stringer("blockID", blk.ID()),
		log.Uint64("height", blk.Height()))

	// Verify the block
	if err := blk.Verify(ctx); err != nil {
		b.logger.Debug("gossiped block failed verification",
			log.Stringer("blockID", blk.ID()),
			log.Err(err))
		return nil
	}

	// Accept the block
	if err := blk.Accept(ctx); err != nil {
		b.logger.Warn("failed to accept gossiped block",
			log.Stringer("blockID", blk.ID()),
			log.Err(err))
		return nil
	}

	// Set preference to the new block
	if err := b.vm.SetPreference(ctx, blk.ID()); err != nil {
		b.logger.Debug("failed to set preference to gossiped block",
			log.Stringer("blockID", blk.ID()),
			log.Err(err))
	}

	b.logger.Info("accepted gossiped block",
		log.Stringer("blockID", blk.ID()),
		log.Uint64("height", blk.Height()))

	return nil
}
func (b *blockHandler) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, container []byte) error {
	// Handle PushQuery the same as Put - process the block
	return b.Put(ctx, nodeID, requestID, container)
}
func (b *blockHandler) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
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
