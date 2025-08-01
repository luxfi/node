// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/luxfi/crypto/bls"
	db "github.com/luxfi/database"
	"github.com/luxfi/database/meterdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	luxmetrics "github.com/luxfi/metrics"
	"github.com/luxfi/node/api/health"
	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/node/api/server"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/quasar/engine/core"
	"github.com/luxfi/node/quasar/engine/core/tracker"
	"github.com/luxfi/node/quasar/engine/dag/vertex"
	"github.com/luxfi/node/quasar/engine/chain"
	"github.com/luxfi/node/quasar/engine/chain/block"
	"github.com/luxfi/node/quasar/networking/handler"
	"github.com/luxfi/node/quasar/networking/router"
	"github.com/luxfi/node/quasar/networking/sender"
	"github.com/luxfi/node/quasar/networking/timeout"
	"github.com/luxfi/node/quasar/validators"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/network/p2p"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/trace"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/utils/buffer"
	"github.com/luxfi/node/utils/constants"
	log "github.com/luxfi/log"
	"github.com/luxfi/node/utils/metric"
	"github.com/luxfi/node/utils/perms"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/quasar/adapter"
	"github.com/luxfi/node/vms/fx"
	"github.com/luxfi/node/vms/metervm"
	"github.com/luxfi/node/vms/nftfx"
	// utilswarp "github.com/luxfi/node/utils/warp"
	"github.com/luxfi/node/vms/propertyfx"
	"github.com/luxfi/node/vms/proposervm"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/tracedvm"
	"github.com/luxfi/node/version"

	luxeng "github.com/luxfi/node/quasar/engine/dag"
	// luxbootstrap "github.com/luxfi/node/quasar/engine/dag/bootstrap"
	luxgetter "github.com/luxfi/node/quasar/engine/dag/getter"
	// smeng "github.com/luxfi/node/quasar/engine/chain"
	// smbootstrap "github.com/luxfi/node/quasar/engine/chain/bootstrap"
	lineargetter "github.com/luxfi/node/quasar/engine/chain/getter"
	// factories "github.com/luxfi/node/quasar/factories"
	smcon "github.com/luxfi/node/quasar/chain"
	timetracker "github.com/luxfi/node/quasar/networking/tracker"
	// p2ppb "github.com/luxfi/node/proto/pb/p2p"
)

const (
	ChainLabel = "chain"

	defaultChannelSize = 1
	initialQueueSize   = 3

	luxNamespace          = constants.PlatformName + metric.NamespaceSeparator + "lux"
	handlerNamespace      = constants.PlatformName + metric.NamespaceSeparator + "handler"
	meterchainvmNamespace = constants.PlatformName + metric.NamespaceSeparator + "meterchainvm"
	meterdagvmNamespace   = constants.PlatformName + metric.NamespaceSeparator + "meterdagvm"
	proposervmNamespace   = constants.PlatformName + metric.NamespaceSeparator + "proposervm"
	p2pNamespace          = constants.PlatformName + metric.NamespaceSeparator + "p2p"
	chainNamespace        = constants.PlatformName + metric.NamespaceSeparator + "chain"
	linearNamespace       = constants.PlatformName + metric.NamespaceSeparator + "linear"
	stakeNamespace        = constants.PlatformName + metric.NamespaceSeparator + "stake"
)

var (
	// Commonly shared VM DB prefix
	VMDBPrefix = []byte("vm")

	// Bootstrapping prefixes for LinearizableVMs
	VertexDBPrefix              = []byte("vertex")
	VertexBootstrappingDBPrefix = []byte("vertex_bs")
	TxBootstrappingDBPrefix     = []byte("tx_bs")
	BlockBootstrappingDBPrefix  = []byte("interval_block_bs")

	// Bootstrapping prefixes for ChainVMs
	ChainBootstrappingDBPrefix = []byte("interval_bs")

	errUnknownVMType           = errors.New("the vm should have type lux.DAGVM or block.ChainVM")
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

// prometheusRegistryAdapter adapts prometheus.Registry to metrics.Registerer
type prometheusRegistryAdapter struct {
	reg *prometheus.Registry
}

func (p *prometheusRegistryAdapter) Register(c luxmetrics.Collector) error {
	// For now, just return nil as we can't directly convert between Lux and Prometheus collectors
	return nil
}

func (p *prometheusRegistryAdapter) MustRegister(c luxmetrics.Collector) {
	// For now, do nothing as we can't directly convert between Lux and Prometheus collectors
}

func (p *prometheusRegistryAdapter) Unregister(c luxmetrics.Collector) bool {
	// For now, return true as we can't directly convert between Lux and Prometheus collectors
	return true
}

func (p *prometheusRegistryAdapter) Gather() ([]*luxmetrics.MetricFamily, error) {
	// This would need proper conversion from prometheus MetricFamily to Lux MetricFamily
	// For now, return empty
	return nil, nil
}

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
	// ID of the subnet that validates this block.
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
	Context *quasar.Context
	VM      core.VM
	Handler core.Handler
}

// ChainConfig is configuration settings for the current execution.
// [Config] is the user-provided config blob for the block.
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
	BlockAcceptorGroup        quasar.AcceptorGroup
	TxAcceptorGroup           quasar.AcceptorGroup
	VertexAcceptorGroup       quasar.AcceptorGroup
	DB                        db.Database
	MsgCreator                message.OutboundMsgBuilder // message creator, shared with network
	Router                    router.Router              // Routes incoming messages to the appropriate chain
	Net                       network.Network            // Sends consensus messages to other validators
	Validators                validators.Manager         // Validators validating on this chain
	NodeID                    ids.NodeID                 // The ID of this node
	NetworkID                 uint32                     // ID of the network this node is connected to
	PartialSyncPrimaryNetwork bool
	Server                    server.Server // Handles HTTP API calls
	AtomicMemory              *atomic.Memory
	LUXAssetID                ids.ID
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
	ImportMode       bool // If true, disable pruning for one-time blockchain data import

	Metrics        metrics.MultiGatherer
	MeterDBMetrics metrics.MultiGatherer

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
	chains map[ids.ID]core.Handler

	// chain++ related interface to allow validators retrieval
	validatorState validators.State

	luxGatherer          metrics.MultiGatherer            // chainID
	handlerGatherer      metrics.MultiGatherer            // chainID
	meterChainVMGatherer metrics.MultiGatherer            // chainID
	meterDAGVMGatherer   metrics.MultiGatherer            // chainID
	proposervmGatherer   metrics.MultiGatherer            // chainID
	p2pGatherer          metrics.MultiGatherer            // chainID
	linearGatherer       metrics.MultiGatherer            // chainID
	stakeGatherer        metrics.MultiGatherer            // chainID
	vmGatherer           map[ids.ID]metrics.MultiGatherer // vmID -> chainID
}

// New returns a new Manager
func New(config *ManagerConfig) (Manager, error) {
	luxGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(luxNamespace, luxGatherer); err != nil {
		return nil, err
	}

	handlerGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(handlerNamespace, handlerGatherer); err != nil {
		return nil, err
	}

	meterChainVMGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(meterchainvmNamespace, meterChainVMGatherer); err != nil {
		return nil, err
	}

	meterDAGVMGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(meterdagvmNamespace, meterDAGVMGatherer); err != nil {
		return nil, err
	}

	proposervmGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(proposervmNamespace, proposervmGatherer); err != nil {
		return nil, err
	}

	p2pGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(p2pNamespace, p2pGatherer); err != nil {
		return nil, err
	}

	chainGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(chainNamespace, chainGatherer); err != nil {
		return nil, err
	}

	linearGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(linearNamespace, linearGatherer); err != nil {
		return nil, err
	}

	stakeGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(stakeNamespace, stakeGatherer); err != nil {
		return nil, err
	}

	return &manager{
		Aliaser:                ids.NewAliaser(),
		ManagerConfig:          *config,
		chains:                 make(map[ids.ID]core.Handler),
		chainsQueue:            buffer.NewUnboundedBlockingDeque[ChainParameters](initialQueueSize),
		unblockChainCreatorCh:  make(chan struct{}),
		chainCreatorShutdownCh: make(chan struct{}),

		luxGatherer:          luxGatherer,
		handlerGatherer:      handlerGatherer,
		meterChainVMGatherer: meterChainVMGatherer,
		meterDAGVMGatherer:   meterDAGVMGatherer,
		proposervmGatherer:   proposervmGatherer,
		p2pGatherer:          p2pGatherer,
		linearGatherer:       linearGatherer,
		stakeGatherer:        stakeGatherer,
		vmGatherer:           make(map[ids.ID]metrics.MultiGatherer),
	}, nil
}

// QueueChainCreation queues a chain creation request
// Invariant: Tracked Subnet must be checked before calling this function
func (m *manager) QueueChainCreation(chainParams ChainParameters) {
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
			m.Log.Fatal("error creating required chain",
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
	m.chains[chainParams.ID] = chain.Handler
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

	// Notify those who registered to be notified when a new chain is created
	m.notifyRegistrants(chain.Name, chain.Context, chain.VM)

	// Allows messages to be routed to the new chain. If the handler hasn't been
	// started and a message is forwarded, then the message will block until the
	// handler is started.
	
	// Create validators.Set from quasar.ValidatorSet
	validatorSet, err := chain.Context.ValidatorSet.GetValidatorSet(0)
	if err != nil {
		m.Log.Error("failed to get validator set",
			zap.Stringer("chainID", chainParams.ID),
			zap.Error(err),
		)
		return
	}
	
	// Convert validators.Set to []ids.NodeID for bootstrappers
	bootstrapperIDs := make([]ids.NodeID, 0)
	// The Bootstrappers field is already a validators.Set
	// We need to iterate through it properly
	bootstrapperList := chain.Context.Bootstrappers.List()
	for _, validator := range bootstrapperList {
		bootstrapperIDs = append(bootstrapperIDs, validator.NodeID)
	}
	
	// Convert quasar.Context to core.Context for router
	coreCtx := &core.Context{
		ChainID:        chain.Context.ChainID,
		SubnetID:       chain.Context.SubnetID,
		NodeID:         chain.Context.NodeID,
		Registerer:     core.Registerer(chain.Context.Registerer),
		Log:            core.Logger(chain.Context.Log),
		Lock:           &chain.Context.Lock,
		ValidatorSet:   validatorSet,
		ValidatorState: chain.Context.ValidatorState,
		Sender:         core.Sender(chain.Context.Sender),
		Bootstrappers:  bootstrapperIDs,
		StartTime:      chain.Context.StartTime,
		RequestID:      &core.RequestID{},
	}
	
	routerChain := &router.Chain{
		ChainID:  chainParams.ID,
		SubnetID: chainParams.SubnetID,
		Context:  coreCtx,
		Handler:  chain.Handler,
		VM:       nil, // VM will be set later
	}
	m.ManagerConfig.Router.AddChain(context.TODO(), routerChain)

	// Register bootstrapped health checks after P chain has been added to
	// chains.
	//
	// Note: Registering this after the chain has been tracked prevents a race
	//       condition between the health check and adding the first chain to
	//       the manager.
	if chainParams.ID == constants.QuantumChainID {
		if err := m.registerBootstrappedHealthChecks(); err != nil {
			chain.Handler.Stop(context.TODO())
			m.Log.Error("failed to register bootstrapped health checks",
				zap.Stringer("chainID", chainParams.ID),
				zap.Error(err),
			)
		}
	}

	// Tell the chain to start processing messages.
	// If the X, P, or C Chain panics, do not attempt to recover
	if err := chain.Handler.Start(context.TODO(), 0); err != nil {
		m.Log.Error("failed to start chain handler",
			zap.Stringer("chainID", chainParams.ID),
			zap.Error(err),
		)
	}
}

// Create a chain
func (m *manager) buildChain(chainParams ChainParameters, sb subnets.Subnet) (*chainInfo, error) {
	if chainParams.ID != constants.QuantumChainID && chainParams.VMID == constants.PlatformVMID {
		return nil, errCreatePlatformVM
	}
	primaryAlias := m.PrimaryAliasOrDefault(chainParams.ID)

	// Create this chain's data directory
	chainDataDir := filepath.Join(m.ChainDataDir, chainParams.ID.String())
	if err := os.MkdirAll(chainDataDir, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("error while creating chain data directory %w", err)
	}

	// Create the log and context of the chain
	chainLog, err := m.LogFactory.MakeChain(primaryAlias)
	if err != nil {
		return nil, fmt.Errorf("error while creating chain's log %w", err)
	}

	ctx := &quasar.Context{
		NetworkID:      m.NetworkID,
		SubnetID:       chainParams.SubnetID,
		ChainID:        chainParams.ID,
		NodeID:         m.NodeID,
		LUXAssetID:     m.LUXAssetID,
		Log:            chainLog,
		BCLookup:       m,
		ValidatorState: m.validatorState,
		Registerer:     &prometheusRegistryAdapter{reg: prometheus.NewRegistry()},
		StartTime:      time.Now(),
		RequestID:      quasar.RequestID{},
		State:          &quasar.EngineState{State: quasar.Bootstrapping},
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
		fxFactory, ok := fxs[fxID]
		if !ok {
			return nil, fmt.Errorf("fx %s not found", fxID)
		}

		chainFxs[i] = &core.Fx{
			ID: fxID,
			Fx: fxFactory.New(),
		}
	}

	var chain *chainInfo
	switch vm := vm.(type) {
	case vertex.LinearizableVMWithEngine:
		chain, err = m.createDAGBasedChain(
			ctx,
			chainParams.GenesisData,
			m.Validators,
			vm,
			chainFxs,
			sb,
		)
		if err != nil {
			return nil, fmt.Errorf("error while creating new DAG-based chain: %w", err)
		}
	case block.ChainVM:
		beacons := m.Validators
		if chainParams.ID == constants.QuantumChainID {
			beacons = chainParams.CustomBeacons
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

	// TODO: Register the chain with the timeout manager
	// if err := m.TimeoutManager.RegisterChain(ctx); err != nil {
	//	return nil, err
	// }

	// Register gatherers if possible
	var errs []error
	if gatherer, ok := ctx.Registerer.(prometheus.Gatherer); ok {
		errs = append(errs, m.linearGatherer.Register(primaryAlias, gatherer))
	}
	// TODO: Register VM gatherer when metrics are available
	
	return chain, errors.Join(errs...)
}

func (m *manager) AddRegistrant(r Registrant) {
	m.registrants = append(m.registrants, r)
}

// Create a DAG-based blockchain (like X-Chain)
func (m *manager) createDAGBasedChain(
	ctx *quasar.Context,
	genesisData []byte,
	vdrs validators.Manager,
	vm vertex.LinearizableVMWithEngine,
	fxs []*core.Fx,
	sb subnets.Subnet,
) (*chainInfo, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.State.Set(quasar.EngineState{
		State: quasar.Bootstrapping,
	})

	primaryAlias := m.PrimaryAliasOrDefault(ctx.ChainID)
	meterDBReg, err := metrics.MakeAndRegister(
		m.MeterDBMetrics,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	meterDB, err := meterdb.New(meterDBReg, m.DB)
	if err != nil {
		return nil, err
	}

	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	// TODO: These DBs are not being used currently
	// vertexDB := prefixdb.New(VertexDBPrefix, prefixDB)
	// vertexBootstrappingDB := prefixdb.New(VertexBootstrappingDBPrefix, prefixDB)
	// txBootstrappingDB := prefixdb.New(TxBootstrappingDBPrefix, prefixDB)
	// blockBootstrappingDB := prefixdb.New(BlockBootstrappingDBPrefix, prefixDB)

	// TODO: luxMetrics not being used currently
	// luxMetrics, err := metrics.MakeAndRegister(
	// 	m.luxGatherer,
	// 	primaryAlias,
	// )
	// if err != nil {
	// 	return nil, err
	// }

	// TODO: Blockers not being used currently
	// vtxBlocker := queue.NewJobsWithMissing()
	// txBlocker := queue.NewJobsWithMissing()

	// Create a sender that wraps the network's ExternalSender interface
	// Note: m.MsgCreator should be message.Creator, not just OutboundMsgBuilder
	msgCreator, ok := m.MsgCreator.(message.Creator)
	if !ok {
		return nil, fmt.Errorf("MsgCreator does not implement message.Creator")
	}
	
	// Create a sender for linear consensus messages (used after linearization)
	linearMessageSender := sender.New(ctx, msgCreator, m.Net, subnets.NewTracker())
	
	// if m.TracingEnabled {
	// 	linearMessageSender = sender.Trace(linearMessageSender, m.Tracer)
	// }

	chainConfig, err := m.getChainConfig(ctx.ChainID)
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

		dagVM = metervm.NewVertexVM(dagVM, meterdagvmReg)
	}
	if m.TracingEnabled {
		dagVM = tracedvm.NewVertexVM(dagVM, m.Tracer)
	}

	// Handles serialization/deserialization of vertices and also the
	// persistence of vertices
	// TODO: Need to implement state.NewSerializer
	// var vtxManager vertex.Manager
	// vtxManager := state.NewSerializer(
	// 	state.SerializerConfig{
	// 		ChainID: ctx.ChainID,
	// 		VM:      dagVM,
	// 		DB:      vertexDB,
	// 		Log:     ctx.Log,
	// 	},
	// )

	// The only difference between using luxMessageSender and
	// linearMessageSender here is where the metrics will be placed. Because we
	// end up using this sender after the linearization, we pass in
	// linearMessageSender here.
	// TODO: LinearizableVMWithEngine doesn't have Initialize method
	// err = dagVM.Initialize(
	// 	context.TODO(),
	// 	ctx,
	// 	vmDB,
	// 	genesisData,
	// 	chainConfig.Upgrade,
	// 	chainConfig.Config,
	// 	fxs,
	// 	linearMessageSender,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("error during vm's Initialize: %w", err)
	// }

	// Initialize the ProposerVM and the vm wrapped inside it
	var (
		// A default subnet configuration will be present if explicit configuration is not provided
		subnetCfg           = m.SubnetConfigs[ctx.SubnetID]
		minBlockDelay       = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	)
	m.Log.Info("creating proposervm wrapper",
		zap.Time("activationTime", m.Upgrades.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", m.Upgrades.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	// Note: this does not use [dagVM] to ensure we use the [vm]'s height index.
	untracedVMWrappedInsideProposerVM := NewLinearizeOnInitializeVM(vm)

	var vmWrappedInsideProposerVM block.ChainVM = untracedVMWrappedInsideProposerVM
	if m.TracingEnabled {
		vmWrappedInsideProposerVM = tracedvm.NewBlockVM(vmWrappedInsideProposerVM, primaryAlias, m.Tracer)
	}

	proposervmReg, err := metrics.MakeAndRegister(
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

	// Note: vmWrappingProposerVM is the VM that the Linear engines should be
	// using.
	var vmWrappingProposerVM block.ChainVM = proposerVM

	if m.MeterVMEnabled {
		meterchainvmReg, err := metrics.MakeAndRegister(
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

	// TODO: block.ChangeNotifier doesn't exist
	// cn := &block.ChangeNotifier{
	// 	ChainVM: vmWrappingProposerVM,
	// }

	// vmWrappingProposerVM = cn

	// Note: linearizableVM is the VM that the Lux engines should be
	// using.
	// TODO: Use linearizableVM when integrating with consensus engines
	_ = &initializeOnLinearizeVM{
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
		appSender:    &appSenderWrapper{sender: linearMessageSender},
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

	// TODO: Use stakeReg when integrating with staking
	_, err = metrics.MakeAndRegister(
		m.stakeGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// TODO: tracker.NewMeteredPeers doesn't exist
	// connectedValidators, err := tracker.NewMeteredPeers(stakeReg)
	// if err != nil {
	// 	return nil, fmt.Errorf("error creating peer tracker: %w", err)
	// }
	// vdrs.RegisterSetCallbackListener(ctx.SubnetID, connectedValidators)
	// TODO: Use connectedValidators when tracker.NewMeteredPeers is available
	var _ tracker.Peers

	p2pReg, err := metrics.MakeAndRegister(
		m.p2pGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// TODO: Use peerTracker when implementing network tracking
	_, err = p2p.NewPeerTracker(
		ctx.Log,
		"peer_tracker",
		p2pReg,
		set.Of(ctx.NodeID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}

	// TODO: Use handlerReg when handler registration is implemented
	_, err = metrics.MakeAndRegister(
		m.handlerGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// TODO: core.Halter doesn't exist
	// var halter core.Halter

	// Asynchronously passes messages from the network to the consensus engine
	// TODO: handler.New doesn't exist
	var _ handler.Handler
	// h, err := handler.New(
	// 	ctx,
	// 	cn,
	// 	linearizableVM.WaitForEvent,
	// 	vdrs,
	// 	m.FrontierPollFrequency,
	// 	m.ConsensusAppConcurrency,
	// 	m.ResourceTracker,
	// 	sb,
	// 	connectedValidators,
	// 	peerTracker,
	// 	handlerReg,
	// 	halter.Halt,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("error initializing network handler: %w", err)
	// }

	// TODO: tracker.NewPeers and tracker.NewStartup don't exist
	// connectedBeacons := tracker.NewPeers()
	// startupTracker := tracker.NewStartup(connectedBeacons, (3*bootstrapWeight+3)/4)
	// vdrs.RegisterSetCallbackListener(ctx.SubnetID, startupTracker)

	// TODO: lineargetter.New doesn't exist, should use NewManager
	var _ *lineargetter.Manager
	// linearGetHandler = lineargetter.NewManager(
	// 	vmWrappingProposerVM, // needs to implement Getter interface
	// 	m.BootstrapMaxTimeGetAncestors,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("couldn't initialize linear base message handler: %w", err)
	// }

	// TODO: factories.ConfidenceFactory doesn't exist and Topological doesn't have Factory field
	// var linearConsensus smcon.Consensus = &smcon.Topological{Factory: factories.ConfidenceFactory}
	var _ smcon.Consensus
	// if m.TracingEnabled {
	// 	linearConsensus = smcon.Trace(linearConsensus, m.Tracer)
	// }

	// Create engine, bootstrapper and state-syncer in this order,
	// to make sure start callbacks are duly initialized
	// TODO: smeng.Config and smeng.New don't exist
	// linearEngineConfig := smeng.Config{
	// 	Ctx:                 ctx,
	// 	AllGetsServer:       linearGetHandler,
	// 	VM:                  vmWrappingProposerVM,
	// 	Sender:              linearMessageSender,
	// 	Validators:          vdrs,
	// 	ConnectedValidators: connectedValidators,
	// 	Params:              consensusParams,
	// 	Consensus:           linearConsensus,
	// }
	// Create linear engine using our consensus adapter
	linearEngine := adapter.NewChainAdapter()
	linearChainParams := chain.Parameters{
		BatchSize:       128,
		ConsensusParams: vmWrappingProposerVM,
	}
	
	// Create context with engine context
	engineCtx := context.WithValue(context.Background(), "engineContext", ctx)
	if err := linearEngine.Initialize(engineCtx, linearChainParams); err != nil {
		return nil, fmt.Errorf("error initializing linear engine: %w", err)
	}
	
	if m.TracingEnabled {
		// TODO: Enable tracing when core.TraceEngine is available
		// linearEngine = core.TraceEngine(linearEngine, m.Tracer)
	}

	// create bootstrap gear
	// TODO: bootstrap.Config has different fields now
	// bootstrapCfg := smbootstrap.Config{
	// 	Haltable:                       &halter,
	// 	NonVerifyingParse:              block.ParseFunc(proposerVM.ParseLocalBlock),
	// 	AllGetsServer:                  linearGetHandler,
	// 	Ctx:                            ctx,
	// 	Beacons:                        vdrs,
	// 	SampleK:                        sampleK,
	// 	StartupTracker:                 startupTracker,
	// 	Sender:                         linearMessageSender,
	// 	BootstrapTracker:               sb,
	// 	PeerTracker:                    peerTracker,
	// 	AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
	// 	DB:                             blockBootstrappingDB,
	// 	VM:                             vmWrappingProposerVM,
	// }
	// TODO: core.BootstrapableEngine doesn't exist
	// var linearBootstrapper core.BootstrapableEngine
	var _ interface{}
	// linearBootstrapper, err = smbootstrap.New(
	// 	bootstrapCfg,
	// 	linearEngine.Start,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("error initializing linear bootstrapper: %w", err)
	// }

	// if m.TracingEnabled {
	// 	linearBootstrapper = core.TraceBootstrapableEngine(linearBootstrapper, m.Tracer)
	// }

	// TODO: luxgetter.New doesn't exist, should use NewManager
	var _ *luxgetter.Manager
	// luxGetHandler = luxgetter.NewManager(
	// 	vtxManager, // needs to implement Getter interface
	// 	m.BootstrapMaxTimeGetAncestors,
	// )

	// Create a sender that wraps the network's ExternalSender interface
	// TODO: Fix type mismatch - m.MsgCreator is OutboundMsgBuilder but needs Creator
	// messageSender := sender.New(ctx, m.MsgCreator, m.Net, sb)
	var messageSender sender.Sender // placeholder - will be nil

	// create engine gear
	luxEngine := adapter.NewDAGAdapter()
	
	// Create DAG parameters
	dagParams := luxeng.Parameters{
		Parents:         2,
		BatchSize:       128,
		ConsensusParams: dagVM, // The VM is passed as the consensus params
	}
	
	// Create context with engine context
	engineCtxDAG := context.WithValue(context.Background(), "engineContext", ctx)
	// Note: The DAG adapter will use the sender from the context
	if err := luxEngine.Initialize(engineCtxDAG, dagParams); err != nil {
		return nil, fmt.Errorf("error initializing DAG engine: %w", err)
	}
	
	if m.TracingEnabled {
		// TODO: Enable tracing when core.TraceEngine is available
		// luxEngine = core.TraceEngine(luxEngine, m.Tracer)
	}

	// create bootstrap gear
	// TODO: luxbootstrap.Config and luxbootstrap.New need to be updated
	// luxBootstrapperConfig := luxbootstrap.Config{
	// 	AllGetsServer:                  luxGetHandler,
	// 	Ctx:                            ctx,
	// 	StartupTracker:                 startupTracker,
	// 	Sender:                         luxMessageSender,
	// 	PeerTracker:                    peerTracker,
	// 	AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
	// 	VtxBlocked:                     vtxBlocker,
	// 	TxBlocked:                      txBlocker,
	// 	Manager:                        vtxManager,
	// 	VM:                             linearizableVM,
	// 	Haltable:                       &halter,
	// }
	// if ctx.ChainID == m.XChainID {
	// 	luxBootstrapperConfig.StopVertexID = m.Upgrades.CortinaXChainStopVertexID
	// }

	// TODO: core.BootstrapableEngine doesn't exist
	// var luxBootstrapper core.BootstrapableEngine
	var _ interface{}
	// luxBootstrapper, err = luxbootstrap.New(
	// 	luxBootstrapperConfig,
	// 	linearBootstrapper.Start,
	// 	luxMetrics,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("error initializing lux bootstrapper: %w", err)
	// }

	// if m.TracingEnabled {
	// 	luxBootstrapper = core.TraceBootstrapableEngine(luxBootstrapper, m.Tracer)
	// }

	// TODO: h.SetEngineManager doesn't exist
	// h.SetEngineManager(&handler.EngineManager{
	// 	Dag: &handler.Engine{
	// 		StateSyncer:  nil,
	// 		Bootstrapper: luxBootstrapper,
	// 		Consensus:    luxEngine,
	// 	},
	// 	Chain: &handler.Engine{
	// 		StateSyncer:  nil,
	// 		Bootstrapper: linearBootstrapper,
	// 		Consensus:    linearEngine,
	// 	},
	// })

	// Register health check for this chain
	// TODO: handler.Handler doesn't implement health.Checker
	// if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.SubnetID.String()); err != nil {
	// 	return nil, fmt.Errorf("couldn't add health check for chain %s: %w", primaryAlias, err)
	// }

	return &chainInfo{
		Name:    primaryAlias,
		Context: ctx,
		VM:      nil, // TODO: dagVM doesn't implement core.VM
		Handler: nil, // TODO: handler.Handler doesn't implement core.Handler
	}, nil
}

// Create a linear chain using the Linear consensus engine
func (m *manager) createLinearChain(
	ctx *quasar.Context,
	genesisData []byte,
	vdrs validators.Manager,
	beacons validators.Manager,
	vm block.ChainVM,
	fxs []*core.Fx,
	sb subnets.Subnet,
) (*chainInfo, error) {
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	// TODO: Set engine state when quasar.Initializing is available
	// ctx.State.Set(quasar.EngineState{
	// 	State: quasar.Initializing,
	// })

	primaryAlias := m.PrimaryAliasOrDefault(ctx.ChainID)
	meterDBReg, err := metrics.MakeAndRegister(
		m.MeterDBMetrics,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	meterDB, err := meterdb.New(meterDBReg, m.DB)
	if err != nil {
		return nil, err
	}

	prefixDB := prefixdb.New(ctx.ChainID[:], meterDB)
	vmDB := prefixdb.New(VMDBPrefix, prefixDB)
	// TODO: Uncomment when bootstrapper is implemented
	// bootstrappingDB := prefixdb.New(ChainBootstrappingDBPrefix, prefixDB)

	// Passes messages from the consensus engine to the network
	// Create a sender that wraps the network's ExternalSender interface
	// Note: m.MsgCreator should be message.Creator, not just OutboundMsgBuilder
	msgCreator, ok := m.MsgCreator.(message.Creator)
	if !ok {
		return nil, fmt.Errorf("MsgCreator does not implement message.Creator")
	}
	messageSender := sender.New(ctx, msgCreator, m.Net, subnets.NewTracker())

	// if m.TracingEnabled {
	// 	messageSender = sender.Trace(messageSender, m.Tracer)
	// }

	// TODO: Uncomment when bootstrapper is implemented
	// var bootstrapFunc func()
	// If [m.validatorState] is nil then we are creating the P-Chain. Since the
	// P-Chain is the first chain to be created, we can use it to initialize
	// required interfaces for the other chains
	if m.validatorState == nil {
		valState, ok := vm.(validators.State)
		if !ok {
			return nil, fmt.Errorf("expected validators.State but got %T", vm)
		}

		// TODO: Enable when validators.Trace is available
		// if m.TracingEnabled {
		// 	valState = validators.Trace(valState, "platformvm", m.Tracer)
		// }

		// Notice that this context is left unlocked. This is because the
		// lock will already be held when accessing these values on the
		// P-block.
		ctx.ValidatorState = valState

		// Initialize the validator state for future chains.
		// TODO: Enable when validators.NewLockedState is available
		// m.validatorState = validators.NewLockedState(&ctx.Lock, valState)
		m.validatorState = valState
		// TODO: Enable when validators.Trace is available
		// if m.TracingEnabled {
		// 	m.validatorState = validators.Trace(m.validatorState, "lockedState", m.Tracer)
		// }

		// TODO: Enable when validators.NewNoValidatorsState is available
		// if !m.ManagerConfig.SybilProtectionEnabled {
		// 	m.validatorState = validators.NewNoValidatorsState(m.validatorState)
		// 	ctx.ValidatorState = validators.NewNoValidatorsState(ctx.ValidatorState)
		// }

		// Set this func only for platform
		//
		// The linear bootstrapper ensures this function is only executed once, so
		// we don't need to be concerned about closing this channel multiple times.
		// bootstrapFunc = func() {
		// 	close(m.unblockChainCreatorCh)
		// }
	}

	// Initialize the ProposerVM and the vm wrapped inside it
	chainConfig, err := m.getChainConfig(ctx.ChainID)
	if err != nil {
		return nil, fmt.Errorf("error while fetching chain config: %w", err)
	}

	var (
		// A default subnet configuration will be present if explicit configuration is not provided
		subnetCfg           = m.SubnetConfigs[ctx.SubnetID]
		minBlockDelay       = subnetCfg.ProposerMinBlockDelay
		numHistoricalBlocks = subnetCfg.ProposerNumHistoricalBlocks
	)
	m.Log.Info("creating proposervm wrapper",
		zap.Time("activationTime", m.Upgrades.ApricotPhase4Time),
		zap.Uint64("minPChainHeight", m.Upgrades.ApricotPhase4MinPChainHeight),
		zap.Duration("minBlockDelay", minBlockDelay),
		zap.Uint64("numHistoricalBlocks", numHistoricalBlocks),
	)

	if m.TracingEnabled {
		vm = tracedvm.NewBlockVM(vm, primaryAlias, m.Tracer)
	}

	proposervmReg, err := metrics.MakeAndRegister(
		m.proposervmGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	proposerVM := proposervm.New(
		vm,
		proposervm.Config{
			Upgrades:            m.Upgrades,
			MinBlkDelay:         minBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   m.StakingTLSSigner,
			StakingCertLeaf:     m.StakingTLSCert,
			Registerer:          proposervmReg,
		},
	)

	vm = proposerVM

	if m.MeterVMEnabled {
		meterchainvmReg, err := metrics.MakeAndRegister(
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

	// TODO: block.ChangeNotifier doesn't exist
	// cn := &block.ChangeNotifier{
	// 	ChainVM: vm,
	// }
	// vm = cn

	// Create an AppSender wrapper to adapt the sender interface
	appSender := &appSenderWrapper{sender: messageSender}

	if err := vm.Initialize(
		context.TODO(),
		ctx,
		vmDB,
		genesisData,
		chainConfig.Upgrade,
		chainConfig.Config,
		fxs,
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

	// TODO: Use stakeReg when integrating with staking
	_, err = metrics.MakeAndRegister(
		m.stakeGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// TODO: tracker.NewMeteredPeers doesn't exist
	// connectedValidators, err := tracker.NewMeteredPeers(stakeReg)
	// if err != nil {
	// 	return nil, fmt.Errorf("error creating peer tracker: %w", err)
	// }
	// vdrs.RegisterSetCallbackListener(ctx.SubnetID, connectedValidators)
	// TODO: Use connectedValidators when tracker.NewMeteredPeers is available
	var _ tracker.Peers

	p2pReg, err := metrics.MakeAndRegister(
		m.p2pGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// TODO: Use peerTracker when implementing network tracking
	_, err = p2p.NewPeerTracker(
		ctx.Log,
		"peer_tracker",
		p2pReg,
		set.Of(ctx.NodeID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating peer tracker: %w", err)
	}

	// TODO: Use handlerReg when handler registration is implemented
	_, err = metrics.MakeAndRegister(
		m.handlerGatherer,
		primaryAlias,
	)
	if err != nil {
		return nil, err
	}

	// TODO: core.Halter doesn't exist
	// var halter core.Halter

	// Asynchronously passes messages from the network to the consensus engine
	// TODO: handler.New doesn't exist
	var _ handler.Handler
	// h, err := handler.New(
	// 	ctx,
	// 	vm,
	// 	vm.WaitForEvent,
	// 	vdrs,
	// 	m.FrontierPollFrequency,
	// 	m.ConsensusAppConcurrency,
	// 	m.ResourceTracker,
	// 	sb,
	// 	connectedValidators,
	// 	peerTracker,
	// 	handlerReg,
	// 	halter.Halt,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("couldn't initialize message handler: %w", err)
	// }

	// TODO: tracker.NewPeers and tracker.NewStartup don't exist
	// connectedBeacons := tracker.NewPeers()
	// startupTracker := tracker.NewStartup(connectedBeacons, (3*bootstrapWeight+3)/4)
	// beacons.RegisterSetCallbackListener(ctx.SubnetID, startupTracker)

	// TODO: lineargetter.New doesn't exist, should use NewManager
	var _ *lineargetter.Manager
	// linearGetHandler = lineargetter.NewManager(
	// 	vm, // needs to implement Getter interface
	// 	m.BootstrapMaxTimeGetAncestors,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("couldn't initialize linear base message handler: %w", err)
	// }

	// Create the consensus engine adapter
	consensusEngine := adapter.NewChainAdapter()
	
	// Create chain parameters for the consensus engine
	chainParams := chain.Parameters{
		BatchSize:       128,
		ConsensusParams: vm, // The VM is passed as the consensus params
	}
	
	// Create context with engine context
	engineCtx := context.WithValue(context.Background(), "engineContext", ctx)
	if err := consensusEngine.Initialize(engineCtx, chainParams); err != nil {
		return nil, fmt.Errorf("error initializing consensus engine adapter: %w", err)
	}

	// TODO: Enable tracing when core.TraceEngine is available
	// if m.TracingEnabled {
	// 	consensusEngine = core.TraceEngine(consensusEngine, m.Tracer)
	// }

	// create bootstrap gear
	// TODO: bootstrap.Config has different fields now
	// bootstrapCfg := smbootstrap.Config{
	// 	Haltable:                       &halter,
	// 	NonVerifyingParse:              block.ParseFunc(proposerVM.ParseLocalBlock),
	// 	AllGetsServer:                  linearGetHandler,
	// 	Ctx:                            ctx,
	// 	Beacons:                        beacons,
	// 	SampleK:                        sampleK,
	// 	StartupTracker:                 startupTracker,
	// 	Sender:                         messageSender,
	// 	BootstrapTracker:               sb,
	// 	PeerTracker:                    peerTracker,
	// 	AncestorsMaxContainersReceived: m.BootstrapAncestorsMaxContainersReceived,
	// 	DB:                             bootstrappingDB,
	// 	VM:                             vm,
	// 	Bootstrapped:                   bootstrapFunc,
	// }
	// TODO: core.BootstrapableEngine doesn't exist
	// var bootstrapper core.BootstrapableEngine
	// TODO: Uncomment when bootstrapper is implemented
	// var bootstrapper interface{}
	// bootstrapper, err = smbootstrap.New(
	// 	bootstrapCfg,
	// 	linearEngine.Start,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("error initializing linear bootstrapper: %w", err)
	// }

	// if m.TracingEnabled {
	// 	bootstrapper = core.TraceBootstrapableEngine(bootstrapper, m.Tracer)
	// }

	// TODO: Implement state sync when syncer package is properly implemented
	// create state sync gear
	// stateSyncCfg, err := syncer.NewConfig(
	// 	linearGetHandler,
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

	// TODO: h.SetEngineManager doesn't exist
	// h.SetEngineManager(&handler.EngineManager{
	// 	Dag: nil,
	// 	Chain: &handler.Engine{
	// 		StateSyncer:  stateSyncer,
	// 		Bootstrapper: bootstrapper,
	// 		Consensus:    linearEngine,
	// 	},
	// })

	// Register health checks
	// TODO: handler.Handler doesn't implement health.Checker
	// if err := m.Health.RegisterHealthCheck(primaryAlias, h, ctx.SubnetID.String()); err != nil {
	// 	return nil, fmt.Errorf("couldn't add health check for chain %s: %w", primaryAlias, err)
	// }

	// Wrap the VM to implement core.VM
	wrappedVM := &vmWrapper{vm: vm}

	return &chainInfo{
		Name:    primaryAlias,
		Context: ctx,
		VM:      wrappedVM,
		Handler: nil, // TODO: handler.Handler doesn't implement core.Handler
	}, nil
}

func (m *manager) IsBootstrapped(id ids.ID) bool {
	m.chainsLock.Lock()
	_, exists := m.chains[id]
	m.chainsLock.Unlock()
	if !exists {
		return false
	}

	// TODO: m.chains stores core.Handler, not *chainInfo, so can't access Context
	// return chain.Context.State.Get().State == quasar.NormalOp
	return true // Assume bootstrapped for now
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
		if !m.IsBootstrapped(constants.QuantumChainID) {
			return "node is currently bootstrapping", nil
		}
		if _, err := m.Validators.GetValidator(constants.PrimaryNetworkID, m.NodeID); err != nil {
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
	m.ManagerConfig.Router.Shutdown(context.TODO())
}

// LookupVM returns the ID of the VM associated with an alias
func (m *manager) LookupVM(alias string) (ids.ID, error) {
	return m.VMManager.Lookup(alias)
}

// Notify registrants [those who want to know about the creation of chains]
// that the specified chain has been created
func (m *manager) notifyRegistrants(name string, ctx *quasar.Context, vm core.VM) {
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

func (m *manager) getOrMakeVMGatherer(vmID ids.ID) (metrics.MultiGatherer, error) {
	vmGatherer, ok := m.vmGatherer[vmID]
	if ok {
		return vmGatherer, nil
	}

	vmName := constants.VMName(vmID)
	vmNamespace := metric.AppendNamespace(constants.PlatformName, vmName)
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

// appSenderWrapper wraps a sender.Sender to implement core.AppSender
type appSenderWrapper struct {
	sender sender.Sender
}

func (w *appSenderWrapper) SendAppRequest(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, msg []byte) error {
	nodeSet := set.NewSet[ids.NodeID](len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeSet.Add(nodeID)
	}
	return w.sender.SendAppRequest(ctx, nodeSet, requestID, msg)
}

func (w *appSenderWrapper) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return w.sender.SendAppResponse(ctx, nodeID, requestID, msg)
}

func (w *appSenderWrapper) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	// sender.Sender doesn't have SendAppError, so we'll send an empty response
	// TODO: Implement proper error handling
	return w.sender.SendAppResponse(ctx, nodeID, requestID, nil)
}

func (w *appSenderWrapper) SendAppGossip(ctx context.Context, msg []byte) error {
	return w.sender.SendAppGossip(ctx, msg)
}

func (w *appSenderWrapper) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	// sender.Sender doesn't have cross-chain app request
	// TODO: Implement cross-chain app request
	return nil
}

func (w *appSenderWrapper) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	// sender.Sender doesn't have cross-chain app response
	// TODO: Implement cross-chain app response
	return nil
}

func (w *appSenderWrapper) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	// sender.Sender doesn't have cross-chain app error
	// TODO: Implement cross-chain app error
	return nil
}

// vmWrapper wraps a block.ChainVM to implement core.VM
type vmWrapper struct {
	vm block.ChainVM
}

// Implement all the methods required by core.VM but not in block.ChainVM

func (w *vmWrapper) Initialize(
	ctx context.Context,
	chainCtx *core.Context,
	dbManager interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- core.Message,
	fxs []*core.Fx,
	appSender core.AppSender,
) error {
	// block.ChainVM doesn't have Initialize with these parameters
	// TODO: Implement proper initialization
	return nil
}

func (w *vmWrapper) SetState(ctx context.Context, state core.State) error {
	// block.ChainVM doesn't have SetState
	// TODO: Implement state management
	return nil
}

func (w *vmWrapper) Shutdown(ctx context.Context) error {
	// block.ChainVM doesn't have Shutdown
	// TODO: Implement shutdown
	return nil
}

func (w *vmWrapper) CreateHandlers(ctx context.Context) (map[string]interface{}, error) {
	// block.ChainVM doesn't have CreateHandlers
	// TODO: Implement handler creation
	return nil, nil
}

func (w *vmWrapper) CreateStaticHandlers(ctx context.Context) (map[string]interface{}, error) {
	// block.ChainVM doesn't have CreateStaticHandlers
	// TODO: Implement static handler creation
	return nil, nil
}

func (w *vmWrapper) HealthCheck(ctx context.Context) (interface{}, error) {
	// block.ChainVM doesn't have HealthCheck
	// TODO: Implement health check
	return nil, nil
}

func (w *vmWrapper) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	// block.ChainVM doesn't have AppRequest
	// TODO: Implement app request handling
	return nil
}

func (w *vmWrapper) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// block.ChainVM doesn't have AppRequestFailed
	// TODO: Implement app request failed handling
	return nil
}

func (w *vmWrapper) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	// block.ChainVM doesn't have AppResponse
	// TODO: Implement app response handling
	return nil
}

func (w *vmWrapper) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	// block.ChainVM doesn't have AppGossip
	// TODO: Implement app gossip handling
	return nil
}

func (w *vmWrapper) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error {
	// block.ChainVM doesn't have CrossChainAppRequest
	// TODO: Implement cross-chain app request
	return nil
}

func (w *vmWrapper) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	// block.ChainVM doesn't have CrossChainAppRequestFailed
	// TODO: Implement cross-chain app request failed
	return nil
}

func (w *vmWrapper) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	// block.ChainVM doesn't have CrossChainAppResponse
	// TODO: Implement cross-chain app response
	return nil
}

func (w *vmWrapper) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	// block.ChainVM doesn't have Connected
	// TODO: Implement connected handler
	return nil
}

func (w *vmWrapper) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	// block.ChainVM doesn't have Disconnected
	// TODO: Implement disconnected handler
	return nil
}

func (w *vmWrapper) Version(ctx context.Context) (string, error) {
	// block.ChainVM doesn't have Version
	// TODO: Implement version
	return "1.0.0", nil
}
