// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	nodeconsensus "github.com/luxfi/node/consensus"
	// xvm "github.com/luxfi/node/vms/xvm" // Unused
	"context"
	"crypto"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/go-json-experiment/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/node/server/http"
	"github.com/luxfi/node/service/health"
	"github.com/luxfi/node/service/metrics"
	"github.com/luxfi/vm"
	"github.com/luxfi/vm/chains/atomic"

	// "github.com/luxfi/database/zapdb" // Unused
	dbmanager "github.com/luxfi/database/manager"
	"github.com/luxfi/runtime"

	// "github.com/luxfi/database/meterdb" // Unused
	// "github.com/luxfi/database/prefixdb" // Unused
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/proto/p2p"
	// vmpb "github.com/luxfi/node/proto/vm" // Removed - using vm.Ready instead
	"github.com/luxfi/warp"

	// "github.com/luxfi/consensus/engine/dag/bootstrap/queue" // Unused
	// "github.com/luxfi/consensus/engine/dag/state" // Unused
	// "github.com/luxfi/consensus/engine/vertex" // Unused

	// "github.com/luxfi/consensus/core/tracker"
	consensusconfig "github.com/luxfi/consensus/config"
	consensuschain "github.com/luxfi/consensus/engine/chain"
	consensusdag "github.com/luxfi/consensus/engine/dag"
	"github.com/luxfi/vm/chain"

	// "github.com/luxfi/vm/chain/syncer"
	"github.com/luxfi/consensus/networking/handler"
	// "github.com/luxfi/consensus/core/router" // Deprecated - using local ChainRouter interface instead
	// "github.com/luxfi/consensus/networking/sender" // Unused after dead code cleanup
	"github.com/luxfi/consensus/networking/timeout"
	"github.com/luxfi/constants"
	"github.com/luxfi/container/buffer"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/filesystem/perms"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	utilmetric "github.com/luxfi/metric"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/trace"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/vm/fx"

	// "github.com/luxfi/node/vms/metervm" // Temporarily disabled - needs consensus package updates
	"github.com/luxfi/utxo/bls12381fx"
	"github.com/luxfi/utxo/ed25519fx"
	"github.com/luxfi/utxo/mldsafx"
	"github.com/luxfi/utxo/nftfx"

	"github.com/luxfi/utxo/propertyfx"
	// "github.com/luxfi/node/vms/proposervm"
	"github.com/luxfi/utxo/schnorrfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/secp256r1fx"
	"github.com/luxfi/utxo/slhdsafx"
	// "github.com/luxfi/node/vms/tracedvm" // Temporarily disabled - needs consensus package updates

	// "github.com/luxfi/node/proto/p2p" // Available if needed for protobuf parsing
	// smcon "github.com/luxfi/vm/chain"
	// aveng "github.com/luxfi/consensus/engine/dag"
	// avbootstrap "github.com/luxfi/consensus/engine/dag/bootstrap"
	// avagetter "github.com/luxfi/consensus/engine/dag/getter"
	// smeng "github.com/luxfi/vm/chain"
	// smbootstrap "github.com/luxfi/vm/chain/bootstrap"
	// consensusgetter "github.com/luxfi/vm/chain/getter"
	timetracker "github.com/luxfi/node/network/tracker"
)

const (
	ChainLabel = "chain"

	defaultChannelSize = 1
	initialQueueSize   = 3

	luxNamespace          = constants.PlatformName + utilmetric.NamespaceSeparator + "lux"
	handlerNamespace      = constants.PlatformName + utilmetric.NamespaceSeparator + "handler"
	meterchainvmNamespace = constants.PlatformName + utilmetric.NamespaceSeparator + "meterchainvm"
	meterdagvmNamespace   = constants.PlatformName + utilmetric.NamespaceSeparator + "meterdagvm"
	proposervmNamespace   = constants.PlatformName + utilmetric.NamespaceSeparator + "proposervm"
	p2pNamespace          = constants.PlatformName + utilmetric.NamespaceSeparator + "p2p"
	chainNamespace        = constants.PlatformName + utilmetric.NamespaceSeparator + "linear"
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
	errNotBootstrapped         = errors.New("chains not bootstrapped")
	errPartialSyncAsAValidator = errors.New("partial sync should not be configured for a validator")

	// fxs lists every Feature eXtension factory the node knows how to load
	// when a chain genesis references it. Must stay in sync with the X-Chain
	// FxIDs[] block in genesis/builder/builder.go: any fx ID present there
	// MUST be registered here, otherwise chain init fails with "fx ... not
	// found" at startup. Keep the order and grouping mirroring genesis to
	// make drift obvious in review.
	fxs = map[ids.ID]fx.Factory{
		// Legacy / classical
		secp256k1fx.ID: &secp256k1fx.Factory{},
		nftfx.ID:       &nftfx.Factory{},
		propertyfx.ID:  &propertyfx.Factory{},
		// Post-quantum (FIPS 203/204/205 family)
		mldsafx.ID:  &mldsafx.Factory{},
		slhdsafx.ID: &slhdsafx.Factory{},
		// EdDSA / Schnorr / NIST P-256
		ed25519fx.ID:   &ed25519fx.Factory{},
		secp256r1fx.ID: &secp256r1fx.Factory{},
		schnorrfx.ID:   &schnorrfx.Factory{},
		// Pairing-friendly curve
		bls12381fx.ID: &bls12381fx.Factory{},
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
	// This assumes only chains in tracked chains are queued.
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

	// GetChains returns info about all locally running chains.
	GetChains() []ChainInfo

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
	// ID of the Net that validates this blockchain.
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

// ChainInfo is the public view of a locally running chain.
// Returned by Manager.GetChains() and exposed via info.GetChains API.
type ChainInfo struct {
	ID           ids.ID `json:"id"`
	Name         string `json:"name"`
	VMID         ids.ID `json:"vmID"`
	Bootstrapped bool   `json:"bootstrapped"`
}

type chainInfo struct {
	Name    string
	VMID    ids.ID
	Runtime *runtime.Runtime
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

func (n *noopValidatorState) GetValidatorSet(ctx context.Context, height uint64, chainID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

func (n *noopValidatorState) GetCurrentValidators(ctx context.Context, height uint64, chainID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

func (n *noopValidatorState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (n *noopValidatorState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (n *noopValidatorState) GetChainID(netID ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (n *noopValidatorState) GetNetworkID(chainID ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (n *noopValidatorState) GetWarpValidatorSets(ctx context.Context, heights []uint64, chainIDs []ids.ID) (map[ids.ID]map[uint64]*validators.WarpSet, error) {
	result := make(map[ids.ID]map[uint64]*validators.WarpSet)
	for _, chainID := range chainIDs {
		result[chainID] = make(map[uint64]*validators.WarpSet)
		for _, height := range heights {
			result[chainID][height] = &validators.WarpSet{
				Height:     height,
				Validators: make(map[ids.NodeID]*validators.WarpValidator),
			}
		}
	}
	return result, nil
}

func (n *noopValidatorState) GetWarpValidatorSet(ctx context.Context, height uint64, chainID ids.ID) (*validators.WarpSet, error) {
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

// quorumValidatorStateLive reports whether a HEIGHT-INDEXED validator state has
// actually been published for a K>1 quorum chain. A nil state means the P-Chain
// never published its validators.State into the manager — getValidatorState would
// then supply the no-op State, whose GetValidatorSet is empty at every height, so
// the ⅔-by-stake tally is 0 and VerifyWeighted fails closed FOREVER (a silent
// permanent finality stall). This predicate is the fail-closed guard (CRITICAL-1
// (c)): a K>1 chain whose state is not live must REFUSE TO START loudly rather
// than run with the no-op State. It is a pure function so the guard is unit-
// testable without building a whole chain (quorum_guard_node_test.go).
func quorumValidatorStateLive(state validators.State) bool {
	return state != nil
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
	UTXOAssetID               ids.ID
	SkipBootstrap             bool            // Skip bootstrapping and start processing immediately
	EnableAutomining          bool            // Enable automining in POA mode
	XChainID                  ids.ID          // ID of the X-Chain,
	CChainID                  ids.ID          // ID of the C-Chain,
	DChainID                  ids.ID          // ID of the D-Chain (DEX),
	CriticalChains            set.Set[ids.ID] // Chains that can't exit gracefully

	// ChainAuthorizations gates per-validator activation of specific chains on
	// X-Chain NFT ownership: a validator may track/validate a listed chain
	// only if its staking X-address (StakingXAddress) holds the required
	// nftfx NFT. A nil/empty map means no chain is NFT-gated, so every chain
	// behaves exactly as before. Critical chains are never gated regardless of
	// this map. Node wiring (e.g. DChainID -> dex-validator collection) is set
	// in node.go; see authorizeChainActivation.
	ChainAuthorizations map[ids.ID]NFTAuthorization
	// StakingXAddress is this node's X-Chain address derived from its staking
	// keys; it is the owner whose UTXOs the NFT-authorization gate inspects.
	// Empty when no chain is gated; an empty address with a gated chain causes
	// that chain to be opted out (fail closed).
	StakingXAddress ids.ShortID
	// DexValidator is the operator's opt-in to PARTICIPATE in the D-Chain DEX
	// (track/validate it + dial the venue matcher). It is a NECESSARY condition
	// for D-Chain activation that composes AND-wise with the NFT gate: the
	// D-Chain (DChainID) activates only if DexValidator is true AND the NFT
	// requirement (if any) is satisfied. Default false means a node does NOT
	// activate the D-Chain even when it is queued by --track-all-chains; the
	// activation authority is authorizeChainActivation, not the platformvm
	// tracking set. Only the D-Chain consults this flag; every other chain is
	// untouched. Sourced from node Config.DexValidator (see node.go).
	DexValidator   bool
	TimeoutManager timeout.Manager // Manages request timeouts when sending messages to other validators
	Health         health.Registerer
	NetConfigs     map[ids.ID]nets.Config // ID -> NetConfig
	ChainConfigs   map[string]ChainConfig // alias -> ChainConfig
	// ShutdownNodeFunc allows the chain manager to issue a request to shutdown the node
	ShutdownNodeFunc func(exitCode int)
	MeterVMEnabled   bool // Should each VM be wrapped with a MeterVM

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

	Nets *Nets

	// SecurityProfile is the chain-wide ChainSecurityProfile resolved
	// from the genesis pin during node bootstrap (F102). The chain
	// manager forwards a ProfileID + ProfileHash pin to every VM
	// Initialize call via the VM config bytes so the rpcchainvm plugin
	// (coreth C-Chain) inherits the chain-wide posture without
	// re-resolving genesis. Closes red-team finding F118.
	//
	// Nil under classical-compat or pre-locked-profile chains; the
	// stamping pass is a no-op in that case.
	SecurityProfile *consensusconfig.ChainSecurityProfile
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

	// gatedChainsLock guards the NFT-authorization gate's defer state.
	// pendingGatedChains holds chains parked because the X-Chain was not yet
	// bootstrapped when their activation was attempted; they are re-queued once
	// the X-Chain is created (retryPendingGatedChains). gatedAttempts caps
	// re-attempts per chain so a never-bootstrapping X-Chain cannot park a
	// chain forever. See manager_authz.go.
	gatedChainsLock    sync.Mutex
	pendingGatedChains []ChainParameters
	gatedAttempts      map[ids.ID]int

	chainsLock sync.Mutex
	// Key: Chain's ID
	// Value: The chain
	chains map[ids.ID]*chainInfo

	// chain++ related interface to allow validators retrieval
	validatorState validators.State

	luxGatherer          metrics.MultiGatherer            // chainID
	handlerGatherer      metrics.MultiGatherer            // chainID
	meterChainVMGatherer metrics.MultiGatherer            // chainID
	meterGRAPHVMGatherer metrics.MultiGatherer            // chainID
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

	meterGRAPHVMGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(meterdagvmNamespace, meterGRAPHVMGatherer); err != nil {
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

	linearGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(chainNamespace, linearGatherer); err != nil {
		return nil, err
	}

	stakeGatherer := metrics.NewLabelGatherer(ChainLabel)
	if err := config.Metrics.Register(stakeNamespace, stakeGatherer); err != nil {
		return nil, err
	}

	// Initialize chain database manager using single global ZapDB with prefix isolation
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
		gatedAttempts:          make(map[ids.ID]int),

		luxGatherer:          luxGatherer,
		handlerGatherer:      handlerGatherer,
		meterChainVMGatherer: meterChainVMGatherer,
		meterGRAPHVMGatherer: meterGRAPHVMGatherer,
		proposervmGatherer:   proposervmGatherer,
		p2pGatherer:          p2pGatherer,
		linearGatherer:       linearGatherer,
		stakeGatherer:        stakeGatherer,
		vmGatherer:           make(map[ids.ID]metrics.MultiGatherer),
	}, nil
}

// QueueChainCreation queues a chain creation request
// Invariant: Tracked Net must be checked before calling this function
func (m *manager) QueueChainCreation(chainParams ChainParameters) {
	m.Log.Info("QueueChainCreation called",
		log.String("vmID", chainParams.VMID.String()),
		log.String("EVMID", constants.EVMID.String()),
		log.Bool("vmIDEqualsEVMID", chainParams.VMID == constants.EVMID),
	)

	// Register blockchain→chain mapping with the network layer so gossip
	// can resolve which validator set to use for this blockchain's blocks.
	if chainParams.ChainID != constants.PrimaryNetworkID && m.Net != nil {
		m.Net.RegisterBlockchainNetwork(chainParams.ID, chainParams.ChainID)
	}

	if sb, _ := m.Nets.GetOrCreate(chainParams.ChainID); !sb.AddChain(chainParams.ID) {
		m.Log.Debug("skipping chain creation",
			log.String("reason", "chain already staged"),
			log.Stringer("chainID", chainParams.ChainID),
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
		)
		return
	}

	if ok := m.chainsQueue.PushRight(chainParams); !ok {
		m.Log.Warn("skipping chain creation",
			log.String("reason", "couldn't enqueue chain"),
			log.Stringer("chainID", chainParams.ChainID),
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
		log.Stringer("chainID", chainParams.ChainID),
		log.Stringer("chainID", chainParams.ID),
		log.Stringer("vmID", chainParams.VMID),
	)

	sb, _ := m.Nets.GetOrCreate(chainParams.ChainID)

	// NFT-authorization gate: a chain listed in ChainAuthorizations may only be
	// activated if this validator's staking X-address holds the required
	// X-Chain NFT. Ungated and critical chains short-circuit to authorized, so
	// this is a no-op for P/C/X/Q/… and whenever ChainAuthorizations is empty.
	if authorized, ready := m.authorizeChainActivation(chainParams.ID); !ready {
		// The X-Chain is not bootstrapped yet, so the gate cannot decide.
		// Park the chain out of the queue and retry once the X-Chain is
		// created; do not skip permanently. The attempt cap bounds this.
		if m.deferGatedChain(chainParams) {
			m.Log.Info("deferring gated chain activation until X-Chain bootstrapped",
				log.Stringer("chainID", chainParams.ID),
				log.Stringer("vmID", chainParams.VMID),
			)
			return
		}
		// Cap exhausted: the X-Chain never became queryable. Opt out cleanly,
		// exactly like an unauthorized chain — mark bootstrapped, no failing
		// health check.
		m.Log.Warn("opting out of gated chain: X-Chain did not bootstrap in time",
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
		)
		sb.Bootstrapped(chainParams.ID)
		return
	} else if !authorized {
		// Clean opt-out: this validator does not hold the required NFT. Mirror
		// the VM-plugin-not-loaded skip exactly — mark the slot bootstrapped
		// and return WITHOUT a failing health check, so the node stays healthy
		// while declining to validate a chain it is not authorized for.
		m.Log.Info("chain activation not authorized — validator X-address does not hold the required NFT",
			log.Stringer("chainID", chainParams.ChainID),
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
		)
		sb.Bootstrapped(chainParams.ID)
		return
	}

	// Note: buildChain builds all chain's relevant objects (notably engine and handler)
	// but does not start their operations. Starting of the handler (which could potentially
	// issue some internal messages), is delayed until chain dispatching is started and
	// the chain is registered in the manager. This ensures that no message generated by handler
	// upon start is dropped.
	chain, err := m.buildChain(chainParams, sb)
	if chain == nil && err == nil {
		m.Log.Info("chain skipped", log.Stringer("chainID", chainParams.ID))
		return
	}

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
				log.Stringer("chainID", chainParams.ChainID),
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
				log.Stringer("chainID", chainParams.ChainID),
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

		// Plugin-not-loaded is a deliberate skip, not a failure.
		//
		// Each validator opts into chains by loading their VM plugin
		// (Liquid loads /dex/fhe; doesn't load Lux's upstream
		// EVM plugin for C-Chain because Liquid runs its own EVM as a
		// chain on the primary's P-chain). When the chain manager hits a
		// chain whose VM isn't registered, that's the validator's "no I
		// don't validate this one" signal — log info, mark the slot
		// bootstrapped, and return WITHOUT registering a failing health
		// check. The chain stays in pending state for hot-load if the
		// plugin shows up later (e.g. via lpm install).
		//
		// The old behavior (always register a failing health check) made
		// /ext/health return 503 for any opted-out chain, which made
		// kubelet liveness probes kill the validator pod. That made
		// chain participation effectively all-or-nothing per validator.
		// Now: validators participate per-plugin, opt-in.
		if errors.Is(err, vms.ErrNotFound) {
			m.Log.Info("chain VM plugin not loaded — opting out of this chain",
				log.Stringer("chainID", chainParams.ChainID),
				log.Stringer("chainID", chainParams.ID),
				log.String("chainAlias", chainAlias),
				log.Stringer("vmID", chainParams.VMID),
			)
			sb.Bootstrapped(chainParams.ID)
			return
		}

		m.Log.Warn("non-critical chain failed to initialize",
			log.Stringer("chainID", chainParams.ChainID),
			log.Stringer("chainID", chainParams.ID),
			log.String("chainAlias", chainAlias),
			log.Stringer("vmID", chainParams.VMID),
			log.Err(err),
		)

		// Non-critical chain failed for a real reason (not missing
		// plugin). Mark it as bootstrapped so it doesn't block the
		// node-level health check, but DO register a per-chain failing
		// health check so operators see something is wrong with a chain
		// they did opt into.
		sb.Bootstrapped(chainParams.ID)

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
				log.Stringer("chainID", chainParams.ChainID),
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

	// The X-Chain is now tracked, so the NFT-authorization gate can query it.
	// Re-queue any gated chains that were parked waiting for it (no-op when
	// nothing is parked or this is not the X-Chain).
	if chainParams.ID == m.XChainID {
		m.retryPendingGatedChains()
	}

	// Associate the newly created chain with its default alias
	if err := m.Alias(chainParams.ID, chainParams.ID.String()); err != nil {
		m.Log.Error("failed to alias the new chain with itself",
			log.Stringer("chainID", chainParams.ChainID),
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
			log.Err(err),
		)
	}

	// Notify those who registered to be notified when a new chain is created
	m.notifyRegistrants(chain.Name, chain.Runtime, chain.VM)

	// Register HTTP handlers for this chain if the VM supports it
	m.Log.Info("checking if VM implements CreateHandlers",
		log.Stringer("chainID", chainParams.ID),
		log.String("vmType", fmt.Sprintf("%T", chain.VM)),
	)
	if vm, ok := chain.VM.(interface {
		CreateHandlers(context.Context) (map[string]http.Handler, error)
	}); ok {
		m.Log.Info("VM implements CreateHandlers, calling it",
			log.Stringer("chainID", chainParams.ID),
		)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		handlers, err := vm.CreateHandlers(ctx)
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

					// Register standard chain aliases (uppercase single-letter)
					for alias, name := range map[string]string{
						"C": "C-Chain", "X": "X-Chain", "D": "D-Chain",
						"P": "P-Chain", "Q": "Q-Chain", "T": "T-Chain",
					} {
						if strings.EqualFold(chainParams.Name, name) {
							m.Server.AddRoute(handler, "bc/"+alias, endpoint)
							m.Log.Info("Registered HTTP handler with chain alias",
								log.String("alias", alias),
								log.Stringer("chainID", chainParams.ID),
								log.String("endpoint", endpoint),
							)
						}
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
		routeCtx, routeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer routeCancel()
		m.ManagerConfig.Router.AddChain(routeCtx, chainParams.ID, chain.Handler)
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
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer stopCancel()
				chain.Engine.StopWithError(stopCtx, err)
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
	m.Log.Info("║ Network ID:", log.Stringer("chainID", chainParams.ChainID))
	m.Log.Info("║ Endpoints available at:")
	m.Log.Info("║   → /ext/bc/" + chainParams.ID.String())
	if chainAlias != chainParams.ID.String() {
		m.Log.Info("║   → /ext/bc/" + chainAlias)
	}
	m.Log.Info("╚══════════════════════════════════════════════════════════════════╝")

	// Tell the chain to start processing messages.
	// If the X, P, or C Chain panics, do not attempt to recover
	if chain.Engine != nil {
		startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer startCancel()
		chain.Engine.Start(startCtx, !m.CriticalChains.Contains(chainParams.ID))

		// Start a goroutine to monitor bootstrap completion and notify the chain
		// This is required because the health check (m.Nets.Bootstrapping()) reports
		// chains as not bootstrapped until sb.Bootstrapped(chainID) is called
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
	// The coreth EVM expects metric.MultiGatherer, not a legacy registry type
	m.Log.Info("Creating metrics gatherer", log.String("primaryAlias", primaryAlias))
	chainMetricsGatherer := metrics.NewMultiGatherer()

	// Create a registry and register it with the gatherer
	chainMetricsReg, err := metrics.MakeAndRegister(chainMetricsGatherer, primaryAlias)
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
		// BLS PublicKey serialization not yet wired
		pubKeyBytes = nil
	}

	// Create warp signer for this chain using the node's BLS key
	warpSigner := createWarpSigner(m.StakingBLSKey, m.NetworkID, chainParams.ID)

	// Create per-chain shared memory for cross-chain atomic operations.
	// This is required by coreth's atomic VM for import/export transactions.
	var chainSharedMemory atomic.SharedMemory
	if m.AtomicMemory != nil {
		chainSharedMemory = m.AtomicMemory.NewSharedMemory(chainParams.ID)
	}

	chainRuntime := &runtime.Runtime{
		NetworkID: m.NetworkID,
		ChainID:   chainParams.ID,
		NodeID:    m.NodeID,
		PublicKey: pubKeyBytes,

		XChainID:     m.XChainID,
		CChainID:     m.CChainID,
		UTXOAssetID:  m.UTXOAssetID,
		ChainDataDir: chainDataDir,

		BCLookup:       m,
		ValidatorState: getValidatorState(m.validatorState),
		SharedMemory:   chainSharedMemory,
		Metrics:        chainMetricsGatherer,
		Log:            chainLog,
		WarpSigner:     warpSigner,
	}

	// Get a factory for the vm we want to use on our chain
	m.Log.Info("Getting VM factory", log.Stringer("vmID", chainParams.VMID))
	vmFactory, err := m.VMManager.GetFactory(context.Background(), chainParams.VMID)
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
	vmImpl, err := vmFactory.New(chainLog)
	if err != nil {
		return nil, fmt.Errorf("error while creating vm for chain %s: %w", chainParams.ID, err)
	}

	chainFxs := make([]*vm.Fx, len(chainParams.FxIDs))
	for i, fxID := range chainParams.FxIDs {
		fxFactory, ok := fxs[fxID]
		if !ok {
			return nil, fmt.Errorf("fx %s not found", fxID)
		}

		chainFxs[i] = &vm.Fx{
			ID: fxID,
			Fx: fxFactory.New(),
		}
	}

	m.Log.Info("DEBUG: About to check VM type", log.Stringer("chainID", chainParams.ID), log.String("vmType", fmt.Sprintf("%T", vmImpl)))
	var createdChain *chainInfo
	switch vmTyped := vmImpl.(type) {
	// DAG VM support - for X-Chain and Q-Chain
	case interface{ GetEngine() consensusdag.Engine }:
		m.Log.Info("detected DAG VM with GetEngine()",
			log.Stringer("chainID", chainParams.ID),
		)
		createdChain, err = m.createDAG(chainRuntime, chainParams, vmTyped, chainFxs)
		if err != nil {
			return nil, fmt.Errorf("error creating DAG chain: %w", err)
		}
	case chain.ChainVM:
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
		m.Log.Info("creating linear chain", log.Stringer("chainID", chainRuntime.ChainID))

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
		toEngine := make(chan vm.Message, 1)

		// Convert []*vm.Fx to []interface{}
		fxsInterface := make([]interface{}, len(chainFxs))
		for i, fx := range chainFxs {
			fxsInterface[i] = fx
		}

		// Initialize the VM if it supports the Initialize interface
		// Inject automining config for dev mode (applies to C-Chain/coreth only),
		// then stamp the chain-wide SecurityProfile pin into the C-Chain
		// JSON config (F118) so the rpcchainvm plugin can resolve the
		// profile on its side of the boundary.
		vmConfigBytes := m.injectAutominingConfig(chainParams.VMID, chainConfig.Config)
		vmConfigBytes = m.injectSecurityProfileConfig(chainParams.VMID, vmConfigBytes)
		m.Log.Info("initializing VM", log.Stringer("chainID", chainParams.ID))
		initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer initCancel()
		err = vmTyped.Initialize(
			initCtx,
			vm.Init{
				Runtime:  chainRuntime,
				DB:       vmDB,
				Log:      chainLog,
				Genesis:  chainParams.GenesisData,
				Upgrade:  chainConfig.Upgrade,
				Config:   vmConfigBytes,
				ToEngine: toEngine,
				Fx:       fxsInterface,
				Sender:   nil, // appSender - not needed for simple VMs
			},
		)
		if err != nil {
			m.Log.Error("VM initialization failed",
				log.Stringer("chainID", chainParams.ID),
				log.Err(err))
			return nil, fmt.Errorf("failed to initialize VM: %w", err)
		}
		m.Log.Info("VM initialized successfully", log.Stringer("chainID", chainParams.ID))

		// CRITICAL-1(a): publish the P-Chain's HEIGHT-INDEXED validators.State so
		// every K>1 chain built AFTER it (C-Chain, L1s, …) resolves the weighted
		// validator set / per-voter pubkeys / stake at the block's P-chain epoch
		// height. The platformvm VM instance IS a validators.State (it embeds the
		// height-indexed pvalidators.Manager built from its own state DB, with the
		// real GetValidatorSet(ctx, height, netID)). It only EXISTS after the
		// P-Chain VM is initialized — which is here — and the P-Chain is created
		// before any other primary-network chain (single serial chain-creator
		// goroutine), so this assignment happens-before every later chain reads
		// m.validatorState in the K>1 wiring block below. Without this the field
		// stays nil → getValidatorState returns the no-op → empty set at every
		// height → ⅔-stake tally is 0 → finality stalls forever on every K>1 chain
		// (the bug the fail-closed guard now also catches). Plain assignment is
		// race-free: chain creation is serialized (dispatchChainCreator).
		if chainParams.ID == constants.PlatformChainID {
			if vdrState, ok := vmImpl.(validators.State); ok {
				m.validatorState = vdrState
				m.Log.Info("published P-Chain height-indexed validator state to chain manager (MEDIUM-1/CRITICAL-1)",
					log.Stringer("chainID", chainParams.ID))
			} else {
				// The P-Chain MUST be a validators.State; if it is not, every K>1
				// chain (including the P-Chain itself) would stall. Fail loud.
				return nil, fmt.Errorf(
					"P-Chain VM %T does not implement validators.State — cannot publish the "+
						"height-indexed validator set; every K>1 quorum chain would stall finality",
					vmImpl)
			}
		}

		// X-Chain (and any DAG-native VM) must be linearized into linear block mode
		// BEFORE it goes Ready, so its embedded block builder is wired to the real
		// toEngine channel the cert runtime reads from. Interface-gated: only VMs that
		// implement Linearize (X-Chain) take this path; C/D/P/Q are untouched. This is
		// what lets X-Chain finalize through the same 2/3-stake cert path as every other
		// chain instead of the (now-removed) undriven DAG engine.
		if linearVM, ok := vmTyped.(interface {
			Linearize(context.Context, ids.ID, chan<- vm.Message) error
		}); ok {
			m.Log.Info("linearizing DAG-native VM into linear block mode",
				log.Stringer("chainID", chainParams.ID))
			linCtx, linCancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := linearVM.Linearize(linCtx, ids.Empty, toEngine); err != nil {
				linCancel()
				m.Log.Error("failed to linearize VM",
					log.Stringer("chainID", chainParams.ID),
					log.Err(err))
				return nil, fmt.Errorf("failed to linearize VM into linear block mode: %w", err)
			}
			linCancel()
		}

		// Transition VM to normal operation after initialization
		// For genesis-based networks with pre-configured validators, this is required
		// to make the VM APIs available immediately
		if stateVM, ok := vmTyped.(interface {
			SetState(context.Context, uint32) error
		}); ok {
			m.Log.Info("transitioning VM to normal operation",
				log.Stringer("chainID", chainParams.ID))
			stateCtx, stateCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer stateCancel()
			if err := stateVM.SetState(stateCtx, uint32(vm.Ready)); err != nil {
				m.Log.Error("failed to transition VM to normal operation",
					log.Stringer("chainID", chainParams.ID),
					log.Err(err))
				return nil, fmt.Errorf("failed to transition VM to normal operation: %w", err)
			}
		}

		// Create integrated consensus engine - the ONE right way to set up chain consensus
		// This consolidates: engine creation, emitter wiring, VM registration
		var blockBuilder consensuschain.BlockBuilder
		if bb, ok := vmTyped.(consensuschain.BlockBuilder); ok {
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
		// For L1/net chains, use the net's validator set ID (chainParams.ChainID).
		networkID := chainParams.ChainID
		isNative := ids.IsNativeChain(chainParams.ID)
		if isNative {
			// Native chains (P, C, X, Q, A, B, T, Z, G, I, K) use PrimaryNetworkID for validator lookups
			networkID = constants.PrimaryNetworkID
		}
		if m.Validators != nil && networkID != constants.PrimaryNetworkID {
			if m.Validators.Count(networkID) == 0 {
				m.Log.Warn("no validators found for network ID; falling back to primary network validators",
					log.Stringer("chainID", chainParams.ID),
					log.Stringer("networkID", networkID),
					log.Stringer("fallbackNetworkID", constants.PrimaryNetworkID),
				)
				networkID = constants.PrimaryNetworkID
			}
		}
		m.Log.Info("[CONSENSUS DEBUG] Creating consensus engine for chain",
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("chainParams.ChainID", chainParams.ChainID),
			log.Bool("isNativeChain", isNative),
			log.Stringer("networkIDForValidators", networkID),
			log.Stringer("PrimaryNetworkID", constants.PrimaryNetworkID),
		)

		// Choose consensus parameters based on mode:
		//   - Single-node (--dev, sybil protection disabled): K=1, self-voting
		//     (the sole validator's accept is the 1-of-1 quorum).
		//   - Multi-node (sybil-protected): a BYZANTINE-fault-tolerant param set
		//     selected by network (Mainnet K=21 / Testnet K=11 / Default K=20).
		//
		// CRITICAL-2: the prior code used LocalParams() (K=3, α=2 → f=0, CFT) for
		// ALL sybil-protected nets — a SINGLE Byzantine validator forks K=3/α=2.
		// We now select a real BFT set and FAIL CLOSED if it is not value-safe.
		consensusParams := selectConsensusParams(m.SybilProtectionEnabled, m.NetworkID)
		if m.SybilProtectionEnabled {
			if err := consensusParams.ValidateForValueNetwork(m.NetworkID); err != nil {
				return nil, fmt.Errorf("refusing to start multi-node chain %s with non-BFT consensus params: %w", chainParams.ID, err)
			}
		}

		// HIGH-4: wire the α-of-K vote/cert topology for multi-validator (K>1)
		// chains so finality is LIVE (votes broadcast to all, certs gossiped,
		// followers finalize on the cert). Without a verifier a K>1 engine refuses
		// to Start; without the gossiper Mode() reports degraded and value-DEX is
		// refused. For K==1 these stay nil (no quorum, no signatures).
		gossiper := &networkGossiper{net: m.Net, msgCreator: m.MsgCreator, networkID: networkID}
		netCfg := consensuschain.NetworkConfig{
			ChainID:    chainParams.ID,
			NetworkID:  networkID,
			NodeID:     m.NodeID,
			Validators: m.Validators, // CRITICAL: Pass validator sampler for k-peer polling
			Logger:     m.Log,
			Gossiper:   gossiper,
			VM:         blockBuilder,
			Params:     &consensusParams,
		}
		if consensusParams.K > 1 {
			// CRITICAL-1 FAIL-CLOSED GUARD (c): a K>1 quorum chain finalizes ONLY
			// on a ⅔-by-stake supermajority read from the HEIGHT-INDEXED validator
			// state. If that state was never published (m.validatorState == nil →
			// getValidatorState returns the no-op, whose GetValidatorSet is empty at
			// every height), the stake tally is 0 → VerifyWeighted fails closed →
			// NO block EVER finalizes. That is a SILENT permanent finality stall.
			// Refuse to build the chain LOUDLY instead. The P-Chain publishes its
			// own height-indexed State into m.validatorState the moment it is
			// created (it is built before any other K>1 chain), so the P-Chain and
			// every chain after it sees a live state here; only a genuine wiring
			// regression trips this.
			if !quorumValidatorStateLive(m.validatorState) {
				return nil, fmt.Errorf(
					"refusing to start K>1 quorum chain %s: height-indexed validator state is not wired "+
						"(getValidatorState would supply the no-op State → zero stake at every height → "+
						"VerifyWeighted fails closed → finality would stall permanently). The P-Chain must be "+
						"created first and publish its validators.State; this is a wiring bug, not a runtime condition",
					chainParams.ID)
			}
			// Height-indexed validator state is the SINGLE source of epoch truth for
			// ALL FOUR epoch-pinned reads (MEDIUM-1 / CRITICAL-1 / RESIDUAL-B):
			// membership, per-voter PUBKEY (the verifier), the ⅔-by-stake tally, and
			// the set-root commitment. They are all read at the block's P-CHAIN epoch
			// height so every node computes the IDENTICAL set/pubkeys/weights/root for
			// a given block — independent of async current-map skew during a P-chain /
			// L1-staking change. (Reading the CURRENT map made the signer and the
			// assembler disagree on the set-root, and dropped the votes of validators
			// that had left the current map but were members at the epoch — stalling
			// finality at every staking change.)
			vdrState := getValidatorState(m.validatorState)
			// Vote verifier resolves the voter's pubkey from set@epoch (RESIDUAL-B),
			// the SAME height-pinned source as the stake + set-root — NOT m.Validators
			// (the current map).
			netCfg.VoteVerifier = newBLSVoteVerifier(vdrState, networkID)
			netCfg.VoteSigner = newBLSVoteSigner(m.StakingBLSKey)
			// Stake-weighted finality (HIGH-3): require a ⅔-of-stake supermajority,
			// not just the α-of-K count, so a low-stake coalition cannot finalize.
			netCfg.StakeSource = newValidatorStakeSource(vdrState, networkID)
			// Epoch binding (MEDIUM-1): pin every vote/cert to the weighted validator
			// set IN FORCE AT the block's P-chain epoch height so the ⅔-by-stake
			// predicate is enforced at that epoch — a cert gathered under one set
			// cannot be re-verified against another (its signatures were over this
			// height-pinned root).
			netCfg.ValidatorSetRoot = newValidatorSetRootSource(vdrState, networkID)
			// b2: deliver the REAL P-chain epoch height to the engine. The four reads
			// above are height-pinned but the engine reads that height off the VM
			// block (pChainHeightOf) — and a bare plugin block exposes none, so the
			// engine would resolve the set at P-chain height 0 (the GENESIS set),
			// freezing the epoch and dropping every post-genesis validator's vote.
			// Wrap the BlockBuilder so the block the engine builds/parses carries the
			// proposer's live P-chain height (max(GetCurrentHeight, parentH)), stamped
			// into the gossiped bytes so every follower adopts the IDENTICAL height —
			// the set-root/stake/pubkey reads then track the LIVE set at that height.
			// Installed ONLY here (K>1): a K==1 chain has no cert and no epoch, so the
			// stamp is inert and the inner VM is used directly.
			if blockBuilder != nil {
				blockBuilder = newPChainHeightVM(blockBuilder, vdrState, networkID)
				netCfg.VM = blockBuilder
				m.Log.Info("wired P-chain epoch height into consensus block builder (b2)",
					log.Stringer("chainID", chainParams.ID),
					log.Stringer("networkID", networkID))
			}
		}
		consensusEngine := consensuschain.NewRuntime(netCfg)

		// Start the consensus engine with a LIFETIME context (not a timeout):
		// engine.Start parents all four long-running loops (poll, vote, pipeline,
		// re-poll) to this ctx, so a WithTimeout here kills them ~30s after the
		// chain starts and the quorum cert never assembles — the finality wedge
		// fixed in v1.30.55 (ba3561778e). Do not reintroduce a timeout here.
		if err := consensusEngine.Start(context.Background(), true); err != nil {
			m.Log.Error("failed to start consensus engine",
				log.Stringer("chainID", chainParams.ID),
				log.Err(err))
			return nil, fmt.Errorf("failed to start consensus engine: %w", err)
		}
		m.Log.Info("consensus engine started with Lux consensus (Photon → Wave → Focus)",
			log.Stringer("chainID", chainParams.ID))
		if blockBuilder != nil {
			syncCtx, syncCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer syncCancel()
			lastAcceptedID, height, err := consensuschain.SyncStateFromVM(syncCtx, blockBuilder, consensusEngine.Transitive)
			if err != nil {
				m.Log.Warn("failed to sync consensus state from VM",
					log.Stringer("chainID", chainParams.ID),
					log.Stringer("lastAccepted", lastAcceptedID),
					log.Uint64("height", height),
					log.Err(err))
			} else {
				m.Log.Info("synced consensus state from VM",
					log.Stringer("chainID", chainParams.ID),
					log.Stringer("lastAccepted", lastAcceptedID),
					log.Uint64("height", height))
			}
		}

		// Bridge VM's WaitForEvent to toEngine channel.
		// This is the critical missing piece: ForwardVMNotifications reads from toEngine,
		// but nothing was writing to it! This goroutine calls WaitForEvent on the VM
		// and writes the result to toEngine, which ForwardVMNotifications then reads
		// and forwards to the consensus engine via Notify().
		go func() {
			ctx := context.Background()
			for {
				// Call WaitForEvent on the VM - this blocks until there are pending txs
				// or staker changes that should trigger block building
				msg, err := vmTyped.WaitForEvent(ctx)
				if err != nil {
					if ctx.Err() != nil {
						// Context cancelled, exit gracefully
						return
					}
					m.Log.Warn("WaitForEvent error, retrying",
						log.Stringer("chainID", chainParams.ID),
						log.Err(err))
					time.Sleep(time.Second)
					continue
				}

				// WaitForEvent now returns vm.Message directly
				m.Log.Debug("[VM NOTIFICATION] WaitForEvent returned",
					log.Stringer("chainID", chainParams.ID),
					log.Uint32("messageType", uint32(msg.Type)))
				toEngine <- msg
				m.Log.Debug("[VM NOTIFICATION] Sent to toEngine channel",
					log.Stringer("chainID", chainParams.ID),
					log.Uint32("msgType", uint32(msg.Type)))
			}
		}()

		// Forward VM notifications to consensus (single goroutine)
		go consensusEngine.ForwardVMNotifications(toEngine)

		chainName := chainParams.Name
		if chainName == "" {
			chainName = chainRuntime.ChainID.String()
		}
		createdChain = &chainInfo{
			Name:    chainName,
			VMID:    chainParams.VMID,
			Runtime: chainRuntime,
			VM:      vmTyped,         // Use the real VM directly
			Engine:  consensusEngine, // Use real consensus engine directly
			// Handler parses inbound blocks through the SAME builder the engine emits
			// through (blockBuilder == the P-chain-height wrapper on K>1, the inner VM
			// on K==1), so the container bytes it parses match the bytes the engine
			// framed — one codec, no raw-vs-wrapped split.
			Handler: newBlockHandler(blockBuilder, m.Log, consensusEngine, m.Net, m.MsgCreator, chainParams.ID, networkID),
		}
	default:
		return nil, fmt.Errorf("unsupported VM type: %T", vmImpl)
	}

	vmGatherer, err := m.getOrMakeVMGatherer(chainParams.VMID)
	if err != nil {
		return nil, err
	}
	_ = vmGatherer

	return createdChain, nil
}

func (m *manager) AddRegistrant(r Registrant) {
	m.registrants = append(m.registrants, r)
}

// dagVMAdapter adapts a DAG VM to interfaces.VM for HTTP handler registration
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

func (v *dagVMAdapter) SetState(ctx context.Context, state vm.State) error {
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
	chainRuntime *runtime.Runtime,
	dbMgr dbmanager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- vm.Message,
	fxs []*vm.Fx,
	appSender interface{},
) error {
	return nil // DAG VMs are pre-initialized
}

// createDAG creates a DAG chain (X-Chain, Q-Chain) using the VM's DAG engine
func (m *manager) createDAG(
	rt *runtime.Runtime,
	chainParams ChainParameters,
	vmImpl interface{},
	fxs []*vm.Fx,
) (*chainInfo, error) {
	// Type assert to get GetEngine() method from exchangevm/qvm
	dagVM, ok := vmImpl.(interface{ GetEngine() consensusdag.Engine })
	if !ok {
		return nil, fmt.Errorf("VM does not implement GetEngine() for DAG consensus")
	}

	m.Log.Info("creating DAG chain",
		log.Stringer("chainID", chainParams.ID),
		log.String("vmID", chainParams.VMID.String()),
	)

	// Register chain aliases early so the VM's address parser can resolve them.
	// The "X" prefix in addresses like "X-dev1..." must resolve to the actual blockchain ID.
	if err := m.Alias(chainParams.ID, chainParams.ID.String()); err != nil {
		m.Log.Warn("failed to alias chain with itself", log.Err(err))
	}
	if strings.EqualFold(chainParams.Name, "X-Chain") {
		_ = m.Alias(chainParams.ID, "X")
	} else if strings.EqualFold(chainParams.Name, "Q-Chain") {
		_ = m.Alias(chainParams.ID, "Q")
	}

	// Get chain configuration
	chainConfig, err := m.getChainConfig(chainParams.ID)
	if err != nil {
		m.Log.Warn("failed to get chain config, using empty config",
			log.Stringer("chainID", chainParams.ID),
			log.Err(err))
		chainConfig = ChainConfig{}
	}

	// Inject automining config for dev mode (applies to C-Chain/coreth only)
	chainConfig.Config = m.injectAutominingConfig(chainParams.VMID, chainConfig.Config)

	// Get chain alias for database directory naming
	chainAlias := chainParams.ID.String()
	if aliases, _ := m.Aliases(chainParams.ID); len(aliases) > 0 {
		chainAlias = aliases[0] // Use first alias (e.g., "X", "Q")
	}

	// Get VM database from chain database manager
	// In isolated mode, each chain gets its own ZapDB
	// In legacy mode, uses prefixdb on shared database
	vmDB, err := m.chainDBManager.GetVMDatabase(chainParams.ID, chainAlias)
	if err != nil {
		return nil, fmt.Errorf("failed to get database for chain %s: %w", chainParams.ID, err)
	}

	// Create a context for VM initialization with timeout
	initCtx, cancelInit := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelInit() // Ensure cleanup on function exit

	// Initialize VM if it supports Initialize
	// Try multiple Initialize signatures since VMs may have different interfaces
	vmInitialized := false

	// Try QVM Initialize signature (uses consensus/core types)
	if initVM, ok := vmImpl.(interface {
		Initialize(
			ctx context.Context,
			chainRuntime interface{},
			db database.Database,
			genesisBytes []byte,
			upgradeBytes []byte,
			configBytes []byte,
			toEngine chan<- vm.Message,
			fxs []*vm.Fx,
			appSender warp.Sender,
		) error
	}); ok {
		toEngine := make(chan vm.Message, 1)
		err := initVM.Initialize(
			initCtx,
			rt,
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
		if initVM, ok := vmImpl.(interface {
			Initialize(
				ctx context.Context,
				chainRuntime interface{},
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
				rt,
				vmDB,
				chainParams.GenesisData,
				chainConfig.Upgrade,
				chainConfig.Config,
				toEngine,
				fxsInterface,
				&noopWarpSender{}, // Implements p2p.Sender interface
			)
			if err != nil {
				m.Log.Warn("ExchangeVM-style initialization failed", log.Stringer("chainID", chainParams.ID), log.Err(err))
			} else {
				m.Log.Info("ExchangeVM initialized successfully", log.Stringer("chainID", chainParams.ID))
				vmInitialized = true
			}
		}
	}

	// Try vmcore.Init struct-based Initialize (used by exchangevm)
	if !vmInitialized {
		if initVM, ok := vmImpl.(interface {
			Initialize(context.Context, vm.Init) error
		}); ok {
			toEngine := make(chan vm.Message, 1)
			// Convert []*vm.Fx to []any for vm.Init.Fx field
			fxAny := make([]any, len(fxs))
			for i, fx := range fxs {
				fxAny[i] = fx
			}
			err := initVM.Initialize(initCtx, vm.Init{
				Runtime:  rt,
				DB:       vmDB,
				Genesis:  chainParams.GenesisData,
				Upgrade:  chainConfig.Upgrade,
				Config:   chainConfig.Config,
				ToEngine: toEngine,
				Fx:       fxAny,
			})
			if err != nil {
				m.Log.Warn("vmcore.Init-style initialization failed",
					log.Stringer("chainID", chainParams.ID), log.Err(err))
			} else {
				m.Log.Info("ExchangeVM initialized via vmcore.Init",
					log.Stringer("chainID", chainParams.ID))
				vmInitialized = true
			}
		}
	}

	// Linearize the DAG chain (required for X-Chain).
	// This transitions the chain from DAG mode to linear block mode.
	if vmInitialized {
		if linearVM, ok := vmImpl.(interface {
			Linearize(context.Context, ids.ID, chan<- vm.Message) error
		}); ok {
			toEngine := make(chan vm.Message, 1)
			if err := linearVM.Linearize(initCtx, ids.Empty, toEngine); err != nil {
				m.Log.Warn("failed to linearize DAG chain", log.Stringer("chainID", chainParams.ID), log.Err(err))
			} else {
				m.Log.Info("DAG chain linearized successfully", log.Stringer("chainID", chainParams.ID))
			}
		}
	}

	// Only transition VM to normal operation if initialization succeeded
	if vmInitialized {
		if stateVM, ok := vmImpl.(interface {
			SetState(context.Context, uint32) error
		}); ok {
			if err := stateVM.SetState(initCtx, uint32(vm.Ready)); err != nil {
				m.Log.Warn("failed to transition VM to normal op", log.Stringer("chainID", chainParams.ID), log.Err(err))
			}
		}
	}

	// Get and start the DAG engine
	dagEngine := dagVM.GetEngine()
	if starter, ok := dagEngine.(interface {
		Start(context.Context, uint32) error
	}); ok {
		if err := starter.Start(context.Background(), 0); err != nil {
			return nil, fmt.Errorf("failed to start DAG engine: %w", err)
		}
	}

	m.Log.Info("DAG chain created successfully",
		log.Stringer("chainID", chainParams.ID),
		log.String("status", "using native DAG consensus"),
	)

	// Register HTTP handlers for DAG VMs (exchangevm, qvm, etc.)
	adapter := &dagVMAdapter{underlying: vmImpl}
	dagHandlerCtx, dagHandlerCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dagHandlerCancel()
	handlers, err := adapter.CreateHandlers(dagHandlerCtx)
	if err != nil {
		m.Log.Warn("failed to create HTTP handlers for DAG chain",
			log.Stringer("chainID", chainParams.ID),
			log.Err(err),
		)
	} else if len(handlers) > 0 {
		chainIDStr := chainParams.ID.String()
		for endpoint, handler := range handlers {
			m.Server.AddRoute(handler, "bc/"+chainIDStr, endpoint)
			// Register name alias (e.g., "x-chain")
			if chainParams.Name != "" {
				m.Server.AddRoute(handler, "bc/"+strings.ToLower(chainParams.Name), endpoint)
			}
			// Register standard single-letter alias
			for alias, name := range map[string]string{
				"X": "X-Chain", "Q": "Q-Chain",
			} {
				if strings.EqualFold(chainParams.Name, name) {
					m.Server.AddRoute(handler, "bc/"+alias, endpoint)
					m.Log.Info("Registered DAG chain HTTP handler",
						log.String("alias", alias),
						log.Stringer("chainID", chainParams.ID),
						log.String("endpoint", endpoint),
					)
				}
			}
		}
	}

	dagName := chainParams.Name
	if dagName == "" {
		dagName = chainParams.ID.String()
	}
	return &chainInfo{
		Name:    dagName,
		VMID:    chainParams.VMID,
		Runtime: rt,
		VM:      adapter,
		Handler: &noopHandler{},
	}, nil
}

// errBootstrapTimeout is returned when a chain fails to bootstrap within the timeout period
var errBootstrapTimeout = errors.New("chain failed to bootstrap within timeout")

// monitorBootstrap monitors when a chain finishes bootstrapping and notifies the chain.
// This is critical for health checks because the health check queries m.Nets.Bootstrapping()
// which returns chains that have chains still in bootstrapping state. Without this notification,
// the health check would permanently report "chains not bootstrapped".
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
				m.Log.Info("chain finished bootstrapping, notifying chain",
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

	// Bootstrapped chains start in NormalOp
	return true
}

func (m *manager) GetChains() []ChainInfo {
	m.chainsLock.Lock()
	defer m.chainsLock.Unlock()

	result := make([]ChainInfo, 0, len(m.chains))
	for id, info := range m.chains {
		result = append(result, ChainInfo{
			ID:           id,
			Name:         info.Name,
			VMID:         info.VMID,
			Bootstrapped: true,
		})
	}
	return result
}

func (m *manager) registerBootstrappedHealthChecks() error {
	bootstrappedCheck := health.CheckerFunc(func(context.Context) (interface{}, error) {
		if chainIDs := m.Nets.Bootstrapping(); len(chainIDs) != 0 {
			return chainIDs, errNotBootstrapped
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
	return m.VMManager.Lookup(context.Background(), alias)
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
func (m *manager) notifyRegistrants(name string, rt *runtime.Runtime, vmImpl interface{}) {
	for _, registrant := range m.registrants {
		if coreVM, ok := vmImpl.(vm.VM); ok {
			registrant.RegisterChain(name, rt, coreVM)
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

// injectSecurityProfileConfig stamps the chain-wide ChainSecurityProfile
// pin into the C-Chain JSON config bytes so the coreth plugin VM can
// resolve the profile on its side of the rpcchainvm boundary. Closes
// red-team finding F118.
//
// The pin form is intentionally minimal — ProfileID byte + ProfileHash
// hex — so this manager package does not need to round-trip the full
// ChainSecurityProfile struct. The plugin VM calls
// consensusconfig.ProfileByID + ComputeHash and refuses the chain if the
// pinned hash does not match the live canonical content. Forked binaries
// that swap canonical profile content fail the hash compare; legitimate
// upgrades land via a new ProfileID and ProfileHash pair.
//
// Only applies to C-Chain (EVMID); other VMs are passed through.
func (m *manager) injectSecurityProfileConfig(vmID ids.ID, configBytes []byte) []byte {
	if m.SecurityProfile == nil {
		return configBytes
	}
	if vmID != constants.EVMID {
		return configBytes
	}

	// Parse existing config or create empty object.
	var cfg map[string]interface{}
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			m.Log.Warn("failed to parse C-Chain config for security profile injection, creating new config",
				log.Err(err))
			cfg = make(map[string]interface{})
		}
	} else {
		cfg = make(map[string]interface{})
	}

	cfg["lux-security-profile"] = map[string]interface{}{
		"profileID":      m.SecurityProfile.ProfileID & 0xff,
		"profileHashHex": fmt.Sprintf("%x", m.SecurityProfile.ProfileHash[:]),
	}

	out, err := json.Marshal(cfg)
	if err != nil {
		m.Log.Warn("failed to marshal C-Chain config after security profile injection", log.Err(err))
		return configBytes
	}
	m.Log.Info("injected chain security profile pin into C-Chain plugin config",
		log.Uint32("profileID", m.SecurityProfile.ProfileID),
		log.String("profileName", m.SecurityProfile.ProfileName))
	return out
}

// injectAutominingConfig modifies the config bytes to include enable-automining flag
// when dev mode automining is enabled. This is used for C-Chain (coreth) to enable
// anvil-like block production behavior.
// Only applies to VMs that use JSON config format (EVMID). VMs using binary codec
// config (ThresholdVM, ZKVM, etc.) are not modified.
func (m *manager) injectAutominingConfig(vmID ids.ID, configBytes []byte) []byte {
	if !m.EnableAutomining {
		return configBytes
	}

	// Only inject automining config for EVM-based chains (C-Chain)
	// Other VMs like ThresholdVM, ZKVM use binary codec config format
	if vmID != constants.EVMID {
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

func (e *emptyValidatorManager) GetValidator(chainID ids.ID, nodeID ids.NodeID) (*validators.GetValidatorOutput, bool) {
	return nil, false
}

func (e *emptyValidatorManager) GetValidators(chainID ids.ID) (validators.Set, error) {
	// Return nil for empty validator set since NewEmpty doesn't exist
	return nil, nil
}

func (e *emptyValidatorManager) GetWeight(chainID ids.ID, nodeID ids.NodeID) uint64 {
	return 0
}

func (e *emptyValidatorManager) GetCurrentHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) GetValidatorSet(ctx context.Context, height uint64, chainID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
}

func (e *emptyValidatorManager) GetChainHeight(ctx context.Context, chainID ids.ID) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) OnAcceptedBlockID(blkID ids.ID) {}

func (e *emptyValidatorManager) String() string {
	return "empty validator manager"
}

func (e *emptyValidatorManager) TotalWeight(chainID ids.ID) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) GetLight(chainID ids.ID, nodeID ids.NodeID) uint64 {
	return 0
}

func (e *emptyValidatorManager) TotalLight(chainID ids.ID) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) AddStaker(chainID ids.ID, nodeID ids.NodeID, publicKey []byte, txID ids.ID, light uint64) error {
	return nil
}

func (e *emptyValidatorManager) AddWeight(chainID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return nil
}

func (e *emptyValidatorManager) RemoveWeight(chainID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return nil
}

func (e *emptyValidatorManager) GetMap(chainID ids.ID) map[ids.NodeID]*validators.GetValidatorOutput {
	return nil
}

func (e *emptyValidatorManager) GetValidatorIDs(chainID ids.ID) []ids.NodeID {
	return nil
}

func (e *emptyValidatorManager) NumValidators(chainID ids.ID) int {
	return 0
}

func (e *emptyValidatorManager) NumNets() int {
	return 0
}

func (e *emptyValidatorManager) SubsetWeight(chainID ids.ID, nodeIDs set.Set[ids.NodeID]) (uint64, error) {
	return 0, nil
}

func (e *emptyValidatorManager) Sample(chainID ids.ID, size int) ([]ids.NodeID, error) {
	return nil, nil
}

func (e *emptyValidatorManager) Count(chainID ids.ID) int {
	return 0
}

func (e *emptyValidatorManager) RegisterCallbackListener(listener validators.ManagerCallbackListener) {
}

func (e *emptyValidatorManager) RegisterSetCallbackListener(chainID ids.ID, listener validators.SetCallbackListener) {
}

func (e *emptyValidatorManager) GetCurrentValidators(ctx context.Context, height uint64, chainID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return nil, nil
}

// blockHandler implements handler.Handler interface and processes incoming blocks
// This enables block propagation between validators
type blockHandler struct {
	// vm is the SAME BlockBuilder the consensus engine builds/parses through — the
	// P-chain-height-stamping wrapper on K>1 chains (so ParseBlock unwraps the
	// transport envelope the engine emits and recovers the proposer's epoch
	// height), the inner VM directly on K==1. The handler only needs the
	// BlockBuilder subset (GetBlock / ParseBlock / LastAccepted); typing it as the
	// builder — not chain.ChainVM — keeps the engine and the handler on ONE block
	// codec, so inbound P2P container bytes are parsed by the same code that framed
	// them (no raw-vs-wrapped mismatch).
	vm         consensuschain.BlockBuilder
	logger     log.Logger
	engine     *consensuschain.Runtime    // Consensus engine for proper block handling
	net        network.Network            // Network for sending Qbit responses
	msgCreator message.OutboundMsgBuilder // Message creator for Qbit responses
	chainID    ids.ID                     // Chain ID for message routing
	networkID  ids.ID                     // Network ID for validator routing

	// Context sync support - when a block fails verification due to missing context,
	// we request the prerequisite blocks from the peer to catch up
	pendingContext   map[ids.ID]contextRequest // Map from blockID to pending context request
	requestIDCounter uint32                    // Counter for generating unique request IDs
	maxContextBlocks int                       // Max context blocks to request/serve (default: 256)
	contextRequestMu sync.Mutex                // Protects pendingContext and requestIDCounter

	// Qbit event buffering - when we receive a Qbit for a block we don't have yet,
	// buffer the event and drain when the block arrives
	pendingQbits  map[ids.ID][]QbitEvent // Map from blockID to buffered Qbit events
	pendingQbitMu sync.Mutex             // Protects pendingQbits
}

// QbitEvent is the normalized internal representation of a received Qbit message.
// This is pure data - no VM calls, no Verify, no Accept derivation.
// Vote creation happens separately in applyQbit when the block is available.
type QbitEvent struct {
	From       ids.NodeID // The node that sent the Qbit
	BlockID    ids.ID     // The block being signaled (preferredID)
	RequestID  uint32     // Request ID for dedup and stale detection
	ReceivedAt time.Time  // When the Qbit was received
}

// contextRequest tracks a pending context request (wire: GetAncestors)
type contextRequest struct {
	nodeID    ids.NodeID
	requestID uint32
	blockID   ids.ID
	timestamp time.Time
}

func newBlockHandler(vm consensuschain.BlockBuilder, logger log.Logger, engine *consensuschain.Runtime, net network.Network, msgCreator message.OutboundMsgBuilder, chainID ids.ID, networkID ids.ID) *blockHandler {
	return &blockHandler{
		vm:               vm,
		logger:           logger,
		engine:           engine,
		net:              net,
		msgCreator:       msgCreator,
		chainID:          chainID,
		networkID:        networkID,
		pendingContext:   make(map[ids.ID]contextRequest),
		maxContextBlocks: 256, // Default max context blocks to request/serve
		pendingQbits:     make(map[ids.ID][]QbitEvent),
	}
}

// bufferQbit stores a QbitEvent for later processing when the block isn't available yet
func (b *blockHandler) bufferQbit(ev QbitEvent) {
	b.pendingQbitMu.Lock()
	defer b.pendingQbitMu.Unlock()

	// Add to buffer, limiting max buffered Qbits per block to prevent memory growth
	const maxQbitsPerBlock = 100
	existing := b.pendingQbits[ev.BlockID]
	if len(existing) >= maxQbitsPerBlock {
		return // Don't buffer more
	}

	b.pendingQbits[ev.BlockID] = append(existing, ev)
}

// popBufferedQbits removes and returns all buffered QbitEvents for a given block
func (b *blockHandler) popBufferedQbits(blockID ids.ID) []QbitEvent {
	b.pendingQbitMu.Lock()
	defer b.pendingQbitMu.Unlock()

	evs := b.pendingQbits[blockID]
	delete(b.pendingQbits, blockID)
	return evs
}

// hasBlock returns true if the block is available (either in consensus pendingBlocks or VM storage).
// This is critical for Qbit handling: when we receive votes for a block we've built or received,
// the block may only be in pendingBlocks (not yet verified/stored in VM).
func (b *blockHandler) hasBlock(ctx context.Context, blockID ids.ID) bool {
	// First check if the block is in consensus pending (built or received but not yet finalized).
	// This allows votes to be processed for blocks we're currently considering in consensus.
	if b.engine != nil && b.engine.HasPendingBlock(blockID) {
		return true
	}

	// Fall back to checking VM storage for verified blocks
	if b.vm == nil {
		return false
	}
	_, err := b.vm.GetBlock(ctx, blockID)
	return err == nil
}

// enqueueQbit immediately processes a QbitEvent when the block is available
func (b *blockHandler) enqueueQbit(ctx context.Context, ev QbitEvent) {
	b.applyQbit(ctx, ev)
}

// applyQbit derives a Vote from a QbitEvent and sends it to the consensus engine.
// This is the ONLY place where Vote creation happens.
func (b *blockHandler) applyQbit(ctx context.Context, ev QbitEvent) {
	if b.engine == nil || b.vm == nil {
		b.logger.Warn("engine or vm is nil",
			log.Stringer("from", ev.From),
			log.Stringer("blockID", ev.BlockID))
		return
	}

	// Skip stale Qbits (older than 30 seconds)
	if time.Since(ev.ReceivedAt) > 30*time.Second {
		b.logger.Debug("skipping stale Qbit",
			log.Stringer("from", ev.From),
			log.Stringer("blockID", ev.BlockID),
			log.Duration("age", time.Since(ev.ReceivedAt)))
		return
	}

	// First check if the block is in consensus pending (recently proposed but not yet finalized).
	var blk chain.Block
	var err error

	if pendingBlk, ok := b.engine.GetPendingBlock(ev.BlockID); ok {
		blk = pendingBlk
		b.logger.Debug("found block in consensus pending",
			log.Stringer("from", ev.From),
			log.Stringer("blockID", ev.BlockID))
	} else {
		// Fall back to VM storage for already-verified blocks
		blk, err = b.vm.GetBlock(ctx, ev.BlockID)
		if err != nil {
			b.logger.Warn("block not found in pending or VM",
				log.Stringer("from", ev.From),
				log.Stringer("blockID", ev.BlockID),
				log.Err(err))
			return
		}
		b.logger.Debug("found block in VM storage",
			log.Stringer("from", ev.From),
			log.Stringer("blockID", ev.BlockID))
	}

	// Derive Accept from verification
	accept := (blk.Verify(ctx) == nil)

	// Create Vote from QbitEvent + local verification
	vote := consensuschain.Vote{
		BlockID:  ev.BlockID,
		NodeID:   ev.From,
		Accept:   accept,
		SignedAt: ev.ReceivedAt,
	}
	queued := b.engine.ReceiveVote(vote)

	b.logger.Debug("sent Vote to consensus engine",
		log.Stringer("from", ev.From),
		log.Stringer("blockID", ev.BlockID),
		log.Bool("accept", accept),
		log.Bool("queued", queued))
}

// onBlockArrived is called when a block becomes available locally.
// It drains all buffered QbitEvents for that block and applies them.
func (b *blockHandler) onBlockArrived(ctx context.Context, blockID ids.ID) {
	evs := b.popBufferedQbits(blockID)
	if len(evs) == 0 {
		return
	}

	b.logger.Info("draining buffered Qbits for arrived block",
		log.Stringer("blockID", blockID),
		log.Int("count", len(evs)))

	for _, ev := range evs {
		b.enqueueQbit(ctx, ev)
	}
}

// isMissingContextError returns true if the error indicates missing prerequisite blocks
func isMissingContextError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "unknown ancestor") ||
		strings.Contains(errStr, "missing parent") ||
		strings.Contains(errStr, "parent not found") ||
		strings.Contains(errStr, "unknown parent") ||
		strings.Contains(errStr, "missing context") ||
		strings.Contains(errStr, "not found") // parent block not in local state
}

// requestContext sends a context request (wire: GetAncestors) to fetch missing blocks from a peer
func (b *blockHandler) requestContext(ctx context.Context, nodeID ids.NodeID, blockID ids.ID) {
	if b.net == nil || b.msgCreator == nil {
		return
	}

	b.contextRequestMu.Lock()
	// Check if we already have a pending request for this block
	if _, exists := b.pendingContext[blockID]; exists {
		b.contextRequestMu.Unlock()
		return
	}

	// Generate a new request ID
	b.requestIDCounter++
	requestID := b.requestIDCounter

	// Record the pending request
	b.pendingContext[blockID] = contextRequest{
		nodeID:    nodeID,
		requestID: requestID,
		blockID:   blockID,
		timestamp: time.Now(),
	}
	b.contextRequestMu.Unlock()

	// Create and send context request (wire: GetAncestors message)
	msg, err := b.msgCreator.GetAncestors(
		b.chainID,
		requestID,
		10*time.Second, // Deadline
		blockID,
		p2p.EngineType_ENGINE_TYPE_CHAIN, // Use chain consensus engine type
	)
	if err != nil {
		b.logger.Error("failed to create context request message",
			log.Stringer("blockID", blockID),
			log.Err(err))
		return
	}

	nodeSet := set.NewSet[ids.NodeID](1)
	nodeSet.Add(nodeID)

	sentTo := b.net.Send(msg, nodeSet, b.networkID, 0)
	b.logger.Info("requested context for missing prerequisites",
		log.Stringer("from", nodeID),
		log.Stringer("blockID", blockID),
		log.Uint32("requestID", requestID),
		log.Int("sentTo", sentTo.Len()))
}

func (b *blockHandler) Runtime() *runtime.Runtime                     { return nil }
func (b *blockHandler) Start(ctx context.Context, startReqID uint32)  {}
func (b *blockHandler) Push(ctx context.Context, msg handler.Message) {}
func (b *blockHandler) Len() int                                      { return 0 }
func (b *blockHandler) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}

// GetContext responds to a request for verification context (parent chain blocks)
// starting from containerID. We respond with up to maxAncestors blocks in
// chronological order (oldest first) so the requester can attach the missing context.
func (b *blockHandler) GetContext(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
	if b.vm == nil || b.net == nil || b.msgCreator == nil {
		return nil
	}

	b.logger.Debug("received context request",
		log.Stringer("from", nodeID),
		log.Stringer("containerID", containerID),
		log.Uint32("requestID", requestID))

	// Collect context blocks (walk parent chain)
	var containers [][]byte
	currentID := containerID

	for i := 0; i < b.maxContextBlocks; i++ {
		// First check pending blocks (for recently proposed but not yet accepted blocks)
		// This is critical: when we propose a block and send PullQuery, other validators
		// request context for the proposed block which may not be accepted yet.
		var blk chain.Block
		var found bool

		if b.engine != nil {
			blk, found = b.engine.GetPendingBlock(currentID)
		}

		if !found {
			// Fall back to VM storage for accepted blocks
			var err error
			blk, err = b.vm.GetBlock(ctx, currentID)
			if err != nil {
				// Block not found in either location, stop walking
				break
			}
		}

		// Add block bytes to the response (prepend to get oldest first)
		blockBytes := blk.Bytes()
		containers = append([][]byte{blockBytes}, containers...)

		// Get parent ID for next iteration
		parentID := blk.Parent()
		if parentID == ids.Empty {
			// Reached genesis, stop
			break
		}
		currentID = parentID
	}

	if len(containers) == 0 {
		b.logger.Debug("no context found for request",
			log.Stringer("from", nodeID),
			log.Stringer("containerID", containerID))
		return nil
	}

	// Create and send Context response (wire protocol uses Ancestors message type)
	msg, err := b.msgCreator.Ancestors(b.chainID, requestID, containers)
	if err != nil {
		b.logger.Error("failed to create context response",
			log.Stringer("containerID", containerID),
			log.Err(err))
		return nil
	}

	nodeSet := set.NewSet[ids.NodeID](1)
	nodeSet.Add(nodeID)

	sentTo := b.net.Send(msg, nodeSet, b.networkID, 0)
	b.logger.Info("sent context response",
		log.Stringer("to", nodeID),
		log.Stringer("containerID", containerID),
		log.Int("numBlocks", len(containers)),
		log.Int("sentTo", sentTo.Len()))

	return nil
}

// handleContext processes an incoming context response (wire: Ancestors message).
// This is called when we previously requested context for a block we couldn't verify.
// Each block in the context is processed via Put to add it to our state.
// After processing, we drain any buffered Qbits that were waiting for context.
func (b *blockHandler) handleContext(ctx context.Context, nodeID ids.NodeID, requestID uint32, data []byte) error {
	if b.vm == nil || len(data) == 0 {
		return nil
	}

	b.logger.Debug("received context response",
		log.Stringer("from", nodeID),
		log.Uint32("requestID", requestID),
		log.Int("dataLen", len(data)))

	// The data encodes multiple blocks as:
	// [len][block][len][block]... where len is a 4-byte big-endian length.
	processed := 0
	remaining := data

	for len(remaining) >= 4 {
		blockLen := int(binary.BigEndian.Uint32(remaining[:4]))
		remaining = remaining[4:]
		if blockLen <= 0 || blockLen > len(remaining) {
			b.logger.Debug("invalid context block length",
				log.Stringer("from", nodeID),
				log.Int("processed", processed),
				log.Int("blockLen", blockLen),
				log.Int("remaining", len(remaining)))
			break
		}

		blockBytes := remaining[:blockLen]
		remaining = remaining[blockLen:]

		if err := b.Put(ctx, nodeID, requestID, blockBytes); err != nil {
			b.logger.Debug("failed to process context block",
				log.Stringer("from", nodeID),
				log.Int("processed", processed),
				log.Err(err))
			break
		}
		processed++
	}

	b.logger.Info("processed context blocks",
		log.Stringer("from", nodeID),
		log.Int("processed", processed))

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
		if isMissingContextError(err) {
			if b.vm != nil {
				missingBlk, parseErr := b.vm.ParseBlock(ctx, container)
				if parseErr == nil {
					b.requestContext(ctx, nodeID, missingBlk.ID())
				} else {
					b.logger.Debug("failed to parse missing-context block",
						log.Stringer("from", nodeID),
						log.Err(parseErr))
				}
			}
		}
		b.logger.Debug("failed to handle incoming block",
			log.Stringer("from", nodeID),
			log.Err(err))
		return nil
	}

	if blk != nil {
		blockID := blk.ID()
		b.logger.Info("processed incoming block through consensus",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", blockID),
			log.Uint64("height", blk.Height()))

		// Drain any buffered Qbits that were waiting for this block
		b.onBlockArrived(ctx, blockID)
	}

	return nil
}
func (b *blockHandler) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, container []byte) error {
	// PushQuery sends block data AND expects a Qbit response.
	// CRITICAL: We must ALWAYS respond with Chits, even if we can't fully
	// process the block. This is how Quasar consensus works - the proposer
	// needs vote responses to reach quorum.

	b.logger.Debug("received PushQuery",
		log.Stringer("from", nodeID),
		log.Uint32("requestID", requestID),
		log.Int("containerLen", len(container)))

	if b.net == nil || b.msgCreator == nil || b.vm == nil {
		b.logger.Warn("missing net/msgCreator/vm - cannot respond")
		return nil
	}

	// 1. Process the block via Put (may trigger context fetch asynchronously)
	if err := b.Put(ctx, nodeID, requestID, container); err != nil {
		b.logger.Debug("Put returned error (will still respond with Chits)",
			log.Stringer("from", nodeID),
			log.Err(err))
	}

	// 2. Get the correct block ID by parsing the block through the VM.
	// CRITICAL: The block ID must match the proposer's vmBlock.ID() exactly.
	// The VM computes block IDs using its own hash function (e.g., Keccak256 for EVM).
	// Using SHA256(containerBytes) would produce a DIFFERENT ID, causing votes to
	// never match the proposer's pending block, and consensus would never accept.
	var preferredID ids.ID
	if b.vm != nil {
		if blk, err := b.vm.ParseBlock(ctx, container); err == nil {
			preferredID = blk.ID()
		} else {
			b.logger.Warn("ParseBlock failed, cannot vote correctly",
				log.Stringer("from", nodeID),
				log.Err(err))
			// Cannot vote correctly without the proper block ID - respond with last accepted
			acceptedBlkID, _ := b.vm.LastAccepted(ctx)
			preferredID = acceptedBlkID
		}
	}

	b.logger.Debug("using VM block ID for vote",
		log.Stringer("from", nodeID),
		log.Stringer("blockID", preferredID))

	// 3. Get the last accepted block info for the Chits response
	acceptedBlkID, err := b.vm.LastAccepted(ctx)
	if err != nil {
		b.logger.Warn("failed to get last accepted - responding with empty accepted",
			log.Stringer("from", nodeID),
			log.Err(err))
		// Use empty ID as fallback
		acceptedBlkID = ids.Empty
	}

	var acceptedHeight uint64
	if acceptedBlkID != ids.Empty {
		acceptedBlk, err := b.vm.GetBlock(ctx, acceptedBlkID)
		if err != nil {
			b.logger.Debug("failed to get accepted block (using height 0)",
				log.Stringer("from", nodeID),
				log.Stringer("acceptedBlkID", acceptedBlkID),
				log.Err(err))
			// Use height 0 as fallback - this is OK for genesis state
		} else {
			acceptedHeight = acceptedBlk.Height()
		}
	}

	// 4. Create Chits response (vote for the proposed block)
	qbitMsg, err := b.msgCreator.Chits(b.chainID, requestID, preferredID, preferredID, acceptedBlkID, acceptedHeight)
	if err != nil {
		b.logger.Error("failed to create Chits message",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", preferredID),
			log.Err(err))
		return nil
	}

	// 5. Send Chits response to the requesting node
	nodeSet := set.NewSet[ids.NodeID](1)
	nodeSet.Add(nodeID)

	sentTo := b.net.Send(qbitMsg, nodeSet, b.networkID, 0)
	b.logger.Debug("responded with Chits",
		log.Stringer("from", nodeID),
		log.Stringer("preferredID", preferredID),
		log.Stringer("acceptedID", acceptedBlkID),
		log.Uint64("acceptedHeight", acceptedHeight),
		log.Int("sentTo", sentTo.Len()))

	return nil
}
func (b *blockHandler) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
	// PullQuery requests a preference signal on a block identified by containerID
	// We respond with a Qbit (wire: p2p.Chits) containing our preference

	if b.net == nil || b.msgCreator == nil {
		b.logger.Debug("cannot respond to PullQuery - no network sender",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", containerID))
		return nil
	}

	// Try to get the block from pending blocks first, then VM storage.
	// When we receive a PullQuery, the proposer has the block but it may not
	// be in our VM storage yet. First check pending blocks (proposed but not accepted).
	var blk chain.Block
	var found bool

	// First check pending blocks (for recently proposed but not yet accepted blocks)
	if b.engine != nil {
		blk, found = b.engine.GetPendingBlock(containerID)
		if found {
			b.logger.Debug("found block in pending for PullQuery",
				log.Stringer("blockID", containerID),
				log.Uint64("height", blk.Height()))
		}
	}

	// If not in pending, try VM storage with retries
	if !found {
		var err error
		const maxRetries = 3
		const retryDelay = 20 * time.Millisecond

		for attempt := 0; attempt < maxRetries; attempt++ {
			blk, err = b.vm.GetBlock(ctx, containerID)
			if err == nil {
				found = true
				break // Found the block
			}
			if attempt < maxRetries-1 {
				// Wait before retry to allow concurrent Put to complete
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(retryDelay):
					// Continue to next attempt
				}
			}
		}
	}

	if !found {
		b.logger.Debug("cannot respond to PullQuery - block not found in pending or storage",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", containerID))
		// Block not found - request context (parent chain) from peer
		b.requestContext(ctx, nodeID, containerID)
		return nil
	}

	// Verify the block before voting for it
	if err := blk.Verify(ctx); err != nil {
		b.logger.Debug("cannot respond to PullQuery - block verification failed",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", containerID),
			log.Err(err))
		// If verification failed due to missing context, request it from peer
		if isMissingContextError(err) {
			b.logger.Info("block missing context - requesting from peer",
				log.Stringer("from", nodeID),
				log.Stringer("blockID", containerID))
			b.requestContext(ctx, nodeID, containerID)
		}
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

	// Create Qbit response message (wire: p2p.Chits)
	// preferredID: the block we prefer
	// preferredIDAtHeight: same as preferredID
	// acceptedID: the last accepted block
	// acceptedHeight: height of the accepted block
	qbitMsg, err := b.msgCreator.Chits(b.chainID, requestID, containerID, containerID, acceptedBlkID, acceptedHeight)
	if err != nil {
		b.logger.Error("failed to create Qbit message",
			log.Stringer("from", nodeID),
			log.Stringer("blockID", containerID),
			log.Err(err))
		return nil
	}

	// Send Qbit response to the requesting node
	nodeSet := set.NewSet[ids.NodeID](1)
	nodeSet.Add(nodeID)

	sentTo := b.net.Send(qbitMsg, nodeSet, b.networkID, 0)
	b.logger.Debug("responded to PullQuery with Qbit",
		log.Stringer("from", nodeID),
		log.Stringer("blockID", containerID),
		log.Uint64("height", blk.Height()),
		log.Int("sentTo", sentTo.Len()))

	return nil
}
func (b *blockHandler) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}
func (b *blockHandler) CrossChainRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (b *blockHandler) CrossChainRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	return nil
}
func (b *blockHandler) CrossChainResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	return nil
}
func (b *blockHandler) Request(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (b *blockHandler) RequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}
func (b *blockHandler) Response(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return nil
}
func (b *blockHandler) Gossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	// HIGH-4 receive demux: a quorum envelope (signed vote / finality cert) is
	// routed to the engine's α-of-K topology; anything else is a plain block
	// gossip handled by Put (backward-compatible — decode fails soft).
	//
	// The quorum path is consulted ONLY when the engine is in quorum-finality
	// mode (K>1, live topology). A single-validator engine never emits vote/cert
	// envelopes, so it always treats gossip as a block — this makes a chance
	// collision of the envelope magic with block bytes impossible to misroute on
	// the K==1 path.
	if b.engine != nil && b.engine.Mode() == consensuschain.ModeQuorumFinality {
		if kind, blockID, payload, err := decodeQuorumGossip(msg); err == nil {
			switch kind {
			case quorumKindVote:
				b.engine.HandleIncomingVote(blockID, payload)
			case quorumKindCert:
				b.engine.HandleIncomingCert(payload)
			}
			return nil
		}
	}
	// Plain block gossip.
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
	case handler.PushQuery:
		// PushQuery contains block data AND expects a Qbit (vote) response
		if len(msg.Message) > 0 {
			return b.PushQuery(ctx, msg.NodeID, msg.RequestID, time.Now().Add(10*time.Second), msg.Message)
		}
	case handler.Put:
		// Put contains block data but does not expect a vote response
		if len(msg.Message) > 0 {
			return b.Put(ctx, msg.NodeID, msg.RequestID, msg.Message)
		}
	case handler.PullQuery:
		// PullQuery asks for a preference signal on a block identified by ID
		// Extract the blockID from the message and respond with Qbit
		if len(msg.Message) >= 32 {
			var containerID ids.ID
			copy(containerID[:], msg.Message[:32])
			return b.PullQuery(ctx, msg.NodeID, msg.RequestID, time.Now().Add(10*time.Second), containerID)
		}
	case handler.Vote:
		// Vote contains a preference signal for a block (preferredID)
		// Note: msg.Message already contains the extracted PreferredId from the Qbit protobuf
		// (extracted by chain_router.go via GetContainerBytes which returns m.GetPreferredId())
		b.logger.Debug("received Chits message",
			log.Stringer("from", msg.NodeID),
			log.Uint32("requestID", msg.RequestID),
			log.Int("msgLen", len(msg.Message)))
		if len(msg.Message) >= 32 {
			var preferredID ids.ID
			copy(preferredID[:], msg.Message[:32])

			b.logger.Debug("extracted preferredID",
				log.Stringer("from", msg.NodeID),
				log.Stringer("preferredID", preferredID))

			// Create QbitEvent - pure data, no VM calls here
			ev := QbitEvent{
				From:       msg.NodeID,
				BlockID:    preferredID,
				RequestID:  msg.RequestID,
				ReceivedAt: time.Now(),
			}

			// If block is missing, buffer the event and return
			if !b.hasBlock(ctx, preferredID) {
				b.bufferQbit(ev)
				b.logger.Debug("buffered Qbit - block not in pending or VM",
					log.Stringer("from", ev.From),
					log.Stringer("blockID", ev.BlockID))
				return nil
			}

			b.logger.Debug("block found - enqueuing for processing",
				log.Stringer("from", ev.From),
				log.Stringer("blockID", ev.BlockID))

			// Block is available - enqueue for processing
			// Vote creation happens in applyQbit, not here
			b.enqueueQbit(ctx, ev)
		} else {
			b.logger.Warn("message too short for preferredID",
				log.Stringer("from", msg.NodeID),
				log.Int("msgLen", len(msg.Message)))
		}
	case handler.GetContext:
		// GetContext requests verification context (parent chain) for a block
		if len(msg.Message) >= 32 {
			var containerID ids.ID
			copy(containerID[:], msg.Message[:32])
			return b.GetContext(ctx, msg.NodeID, msg.RequestID, time.Now().Add(10*time.Second), containerID)
		}
	case handler.Context:
		// Context contains prerequisite blocks - process each one via Put
		return b.handleContext(ctx, msg.NodeID, msg.RequestID, msg.Message)
	case handler.Gossip:
		// App-gossip envelope carrying an α-of-K quorum vote or finality cert.
		// Route to Gossip, which demuxes the quorum envelope into
		// engine.HandleIncomingVote / HandleIncomingCert (the vote TRANSPORT that
		// drives finality). Without this case the router delivers the message here
		// but the switch drops it, so no vote is ever counted and the chain wedges.
		// Non-quorum gossip falls through inside Gossip to a plain block Put.
		return b.Gossip(ctx, msg.NodeID, msg.Message)
	}
	return nil
}
func (b *blockHandler) HandleOutbound(ctx context.Context, msg handler.Message) error {
	return nil
}

// noopHandler implements handler.Handler interface
type noopHandler struct{}

func (p *noopHandler) Runtime() *runtime.Runtime                     { return nil }
func (p *noopHandler) Start(ctx context.Context, startReqID uint32)  {}
func (p *noopHandler) Push(ctx context.Context, msg handler.Message) {}
func (p *noopHandler) Len() int                                      { return 0 }
func (p *noopHandler) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (p *noopHandler) GetContext(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
	return nil
}
func (p *noopHandler) GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time) error {
	return nil
}
func (p *noopHandler) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerIDs []ids.ID) error {
	return nil
}
func (p *noopHandler) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	return nil
}
func (p *noopHandler) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, container []byte) error {
	return nil
}
func (p *noopHandler) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
	return nil
}
func (p *noopHandler) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}
func (p *noopHandler) CrossChainRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (p *noopHandler) CrossChainRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	return nil
}
func (p *noopHandler) CrossChainResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	return nil
}
func (p *noopHandler) Request(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}
func (p *noopHandler) RequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}
func (p *noopHandler) Response(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return nil
}
func (p *noopHandler) Gossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return nil
}
func (p *noopHandler) GetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time) error {
	return nil
}
func (p *noopHandler) StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return nil
}
func (p *noopHandler) GetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, heights []uint64) error {
	return nil
}
func (p *noopHandler) AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error {
	return nil
}
func (p *noopHandler) GetStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, height uint64) error {
	return nil
}
func (p *noopHandler) StateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return nil
}
func (p *noopHandler) Connected(ctx context.Context, nodeID ids.NodeID) error    { return nil }
func (p *noopHandler) Disconnected(ctx context.Context, nodeID ids.NodeID) error { return nil }
func (p *noopHandler) HealthCheck(ctx context.Context) (interface{}, error)      { return nil, nil }
func (p *noopHandler) Stop(ctx context.Context)                                  {}
func (p *noopHandler) HandleInbound(ctx context.Context, msg handler.Message) error {
	return nil
}
func (p *noopHandler) HandleOutbound(ctx context.Context, msg handler.Message) error {
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
	networkID  ids.ID
}

// Compile-time check that networkGossiper implements Gossiper
var _ consensuschain.Gossiper = (*networkGossiper)(nil)

// Compile-time check that networkGossiper implements QuorumGossiper — the
// vote/cert distribution topology that makes α-of-K finality LIVE (HIGH-4).
// Without this the consensus engine runs degraded (Mode() != ModeQuorumFinality)
// and value-DEX is refused.
var _ consensuschain.QuorumGossiper = (*networkGossiper)(nil)

// BroadcastVote sends this node's SIGNED accept vote for blockID to ALL
// validators on the network (not just the proposer). The signed vote rides on
// app-gossip framed in a quorum envelope (decoded by blockHandler.Gossip into
// engine.HandleIncomingVote). Broadcasting to ALL — rather than SendVote-to-
// proposer-only — is the structural fix for the proposer-freeze: any node that
// collects α distinct signed votes can assemble + gossip the cert, so finality
// no longer hinges on one node's inbound Chits.
func (g *networkGossiper) BroadcastVote(chainID ids.ID, networkID ids.ID, blockID ids.ID, voteBytes []byte) int {
	if g.net == nil || g.msgCreator == nil {
		return 0
	}
	envelope := encodeQuorumGossip(quorumKindVote, blockID, voteBytes)
	msg, err := g.msgCreator.Gossip(chainID, envelope)
	if err != nil {
		return 0
	}
	return g.net.Gossip(msg, nil, g.networkID, -1, 0, 0).Len()
}

// GossipCert broadcasts an assembled α-of-K finality cert to ALL validators so
// followers finalize blockID on a verifiable proof (HandleIncomingCert), not a
// fast-follow guess. Best effort: the gossiping node's own finality is already
// established by the verified cert.
func (g *networkGossiper) GossipCert(chainID ids.ID, networkID ids.ID, blockID ids.ID, certBytes []byte) int {
	if g.net == nil || g.msgCreator == nil {
		return 0
	}
	envelope := encodeQuorumGossip(quorumKindCert, blockID, certBytes)
	msg, err := g.msgCreator.Gossip(chainID, envelope)
	if err != nil {
		return 0
	}
	return g.net.Gossip(msg, nil, g.networkID, -1, 0, 0).Len()
}

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
	// Must use g.networkID (chain ID) not chainID (blockchain ID) —
	// net.Gossip filters validators by chain ID in the validator manager.
	sentTo := g.net.Gossip(putMsg, nil, g.networkID, -1, 0, 0)
	return sentTo.Len()
}

// SendPullQuery sends a PullQuery (blockID only) to validators requesting votes.
// Prefer SendPushQuery when block bytes are available for immediate peer verification.
func (g *networkGossiper) SendPullQuery(chainID ids.ID, networkID ids.ID, blockID ids.ID, validators []ids.NodeID) int {
	if g.net == nil || g.msgCreator == nil {
		return 0
	}

	pullMsg, err := g.msgCreator.PullQuery(chainID, 0, 5*time.Second, blockID, 0)
	if err != nil {
		return 0
	}

	if len(validators) == 0 {
		return g.net.Gossip(pullMsg, nil, chainID, -1, 0, 0).Len()
	}

	validatorSet := set.NewSet[ids.NodeID](len(validators))
	for _, v := range validators {
		validatorSet.Add(v)
	}
	return g.net.Send(pullMsg, validatorSet, chainID, 0).Len()
}

// SendPushQuery sends a PushQuery (block bytes + vote request) to validators.
// PushQuery includes the block data so peers can immediately verify and vote,
// unlike PullQuery which only sends the blockID requiring a separate fetch.
func (g *networkGossiper) SendPushQuery(chainID ids.ID, networkID ids.ID, blockData []byte, validators []ids.NodeID) int {
	if g.net == nil || g.msgCreator == nil {
		return 0
	}

	pushMsg, err := g.msgCreator.PushQuery(chainID, 0, 5*time.Second, blockData, 0)
	if err != nil {
		log.Warn("SendPushQuery: message creation failed",
			"chainID", chainID,
			"error", err,
		)
		return 0
	}

	log.Info("SendPushQuery: sending to network",
		"chainID", chainID,
		"networkID", networkID,
		"blockDataLen", len(blockData),
		"numValidators", len(validators),
	)

	if len(validators) == 0 {
		return g.net.Gossip(pushMsg, nil, chainID, -1, 0, 0).Len()
	}

	validatorSet := set.NewSet[ids.NodeID](len(validators))
	for _, v := range validators {
		validatorSet.Add(v)
	}
	return g.net.Send(pushMsg, validatorSet, chainID, 0).Len()
}

// SendQbit sends a preference response (Qbit) back to the node that requested our preference.
// This is called after verifying a block received via PullQuery.
func (g *networkGossiper) SendQbit(toNodeID ids.NodeID, chainID ids.ID, requestID uint32, preferredID ids.ID) error {
	if g.net == nil || g.msgCreator == nil {
		return nil
	}

	// Create Qbit message (wire: p2p.Chits) with the preferred block ID
	// Uses preferredID as both preferred and accepted (block is verified)
	qbitMsg, err := g.msgCreator.Chits(chainID, requestID, preferredID, preferredID, preferredID, 0)
	if err != nil {
		return err
	}

	// Send to the specific node
	nodeSet := set.Of(toNodeID)
	g.net.Send(qbitMsg, nodeSet, g.networkID, 0)
	return nil
}

// SendVote sends a vote response back to the proposer node after fast-follow acceptance.
// This is required by the consensuschain.Gossiper interface.
func (g *networkGossiper) SendVote(chainID ids.ID, toNodeID ids.NodeID, blockID ids.ID) error {
	if g.net == nil || g.msgCreator == nil {
		return nil
	}

	// Create a Chits message to send the vote
	// Use blockID as all three IDs (preferred, accepted, last accepted)
	// since this is a positive vote confirming we've accepted the block
	voteMsg, err := g.msgCreator.Chits(chainID, 0, blockID, blockID, blockID, 0)
	if err != nil {
		return err
	}

	// Send to the proposer node
	nodeSet := set.Of(toNodeID)
	g.net.Send(voteMsg, nodeSet, g.networkID, 0)
	return nil
}
