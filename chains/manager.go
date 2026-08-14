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
	gatomic "sync/atomic"
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
	"github.com/luxfi/node/network/peer"
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
	"github.com/luxfi/crypto/mldsa"
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
	version "github.com/luxfi/version"
	"github.com/luxfi/vm/fx"

	// "github.com/luxfi/node/vms/metervm" // Temporarily disabled - needs consensus package updates
	"github.com/luxfi/utxo/bls12381fx"
	"github.com/luxfi/utxo/ed25519fx"
	"github.com/luxfi/utxo/mldsafx"
	"github.com/luxfi/utxo/nftfx"

	"github.com/luxfi/node/vms/proposervm"
	"github.com/luxfi/utxo/propertyfx"
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

	// vmStartupTimeout bounds each VM startup/lifecycle operation (Initialize,
	// Linearize, SetState, CreateHandlers, router AddChain, state-sync). It must
	// be generous enough to cover a cold coreth "Regenerate historical state"
	// pass after an unclean shutdown (observed 67-134s on mainnet C-Chain),
	// which the previous hardcoded 30s budget blew — the context was cancelled
	// mid-init, so the VM was marked failed and its C-Chain route never
	// registered (the recurring post-restart 404 that needed a second restart).
	// Bounded so a genuinely hung VM still surfaces rather than hanging forever.
	vmStartupTimeout = 10 * time.Minute

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

// quasarExportVM is the OPTIONAL two-tier-consensus (v1.36) export sink a VM may
// expose: the consensus engine pushes each Quasar (⅔-by-stake) EXPORT-FINAL
// frontier advance in, and re-seeds from the VM's durable height on boot, so the
// VM's `finalized`/`safe` tags and cross-chain export gate track ⅔-stake
// finality instead of the reorgable Nova accept tip. NOT part of chain.ChainVM —
// generic VMs never implement it and run Nova-only. For a plugin VM the concrete
// implementation is in another process; the rpcchainvm client carries these
// across the boundary and reports whether the plugin advertised the capability
// via SupportsQuasarExport (see the wiring in createChain).
type quasarExportVM interface {
	SetLastQuasarFinalized(uint64)
	LastQuasarHeight() uint64
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
	// Name of the chain (used for HTTP routing alias, e.g., /v1/bc/zoo/rpc)
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

	// ConsensusOverrides are the consensus parameters the operator set
	// explicitly; nil (or all-nil fields) leaves the networkID default alone.
	ConsensusOverrides *ConsensusOverrides
	StakingTLSSigner   crypto.Signer
	StakingTLSCert     *staking.Certificate
	// Strict-PQ proposer identity. When StakingMLDSASigner is non-nil, chains wrap
	// their proposervm with ML-DSA-65 block signing so the signed block's
	// Proposer() matches the ML-DSA-keyed validator set. Nil ⇒ classical TLS-leaf.
	StakingMLDSASigner *mldsa.PrivateKey
	StakingMLDSAPub    []byte
	// ProposerWindowDuration overrides the proposervm proposer-slot spacing (0 ⇒
	// the 5s mainnet default). Small local/dev nets set it low for fast cadence.
	ProposerWindowDuration time.Duration
	// ProposerMinBlockDelay overrides the proposervm minimum block delay (0 ⇒ the
	// 1s default). High-throughput / DEX nets set it low (e.g. 1ms).
	ProposerMinBlockDelay time.Duration
	StakingBLSKey         bls.Signer
	TracingEnabled        bool
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

	// MChainID is the ID of the M-Chain (MPC), whose committed state holds the
	// ownership attestations that entitle a node to a restricted chain. Empty when
	// this network's genesis has no M-Chain, in which case no entitlement can be
	// verified and every restricted chain refuses.
	MChainID ids.ID
	// RestrictedChains are the chains a node may activate only when it is entitled
	// — that is, when the M-Chain holds an ownership attestation naming this node.
	// A nil/empty set means no chain is restricted, so every chain behaves exactly
	// as before. Critical chains are never restricted regardless of this set.
	// Derived in node.go from the single OptionalVMs registry; see
	// authorizeChainActivation.
	RestrictedChains set.Set[ids.ID]
	// DexValidator is the operator's opt-in to PARTICIPATE in the D-Chain DEX
	// (track/validate it + dial the venue matcher). It is a NECESSARY condition
	// for D-Chain activation that composes AND-wise with the entitlement: the
	// D-Chain (DChainID) activates only if DexValidator is true AND this node is
	// entitled. Default false means a node does NOT activate the D-Chain even when
	// it is queued by --track-all-chains; the activation authority is
	// authorizeChainActivation, not the platformvm tracking set. Only the D-Chain
	// consults this flag; every other chain is untouched. Sourced from node
	// Config.DexValidator (see node.go).
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

	// restrictedChainsLock guards the entitlement check's defer state.
	// pendingRestrictedChains holds chains parked because the M-Chain was not yet
	// bootstrapped when their activation was attempted; they are re-queued once the
	// M-Chain converges (retryPendingRestrictedChains). restrictedAttempts caps
	// re-attempts per chain so a never-converging M-Chain cannot park a chain
	// forever. See manager_authz.go.
	restrictedChainsLock    sync.Mutex
	pendingRestrictedChains []ChainParameters
	restrictedAttempts      map[ids.ID]int

	chainsLock sync.Mutex
	// Key: Chain's ID
	// Value: The chain
	chains map[ids.ID]*chainInfo

	// pChainBootstrapped flips true the instant monitorBootstrap declares the P-CHAIN's initial
	// sync COMPLETE (its handler's BootstrapComplete fired — the P-chain fetch+executed to the
	// network frontier, so it has replayed every staker tx and m.Validators now holds the TRUE
	// FULL primary-network set). A native non-platform chain reads it (blockHandler.primaryNetworkReady)
	// to gate its OWN bootstrap frontier-trust: until it is true the staked beacon set is a PARTIAL
	// (mid-replay) set, a non-representative stake denominator that would let a degenerate at-or-below
	// responder subset false-complete the node at its stale height (the mainnet luxd-2 canary). It is
	// the authoritative "primary-network validator set is fully loaded" signal — distinct from
	// manager.IsBootstrapped, which returns true the instant the P-chain merely EXISTS in m.chains
	// (right after createChain launched its bootstrap goroutine — far too early).
	pChainBootstrapped gatomic.Bool

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
		restrictedAttempts:     make(map[ids.ID]int),

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

	// Entitlement check: a chain in RestrictedChains may only be activated by a node
	// the M-Chain holds an ownership attestation for. Unrestricted and critical
	// chains short-circuit to authorized, so this is a no-op for P/C/X/Q/… and
	// whenever RestrictedChains is empty.
	if authorized, ready := m.authorizeChainActivation(chainParams.ID); !ready {
		// The M-Chain is not bootstrapped yet, so entitlement cannot be read. Park
		// the chain out of the queue and retry once the M-Chain converges; do not
		// skip permanently. The attempt cap bounds this.
		if m.deferRestrictedChain(chainParams) {
			m.Log.Info("deferring restricted chain activation until M-Chain bootstrapped",
				log.Stringer("chainID", chainParams.ID),
				log.Stringer("vmID", chainParams.VMID),
			)
			return
		}
		// Cap exhausted: the M-Chain never became queryable. Opt out cleanly,
		// exactly like an unentitled chain — mark bootstrapped, no failing health
		// check.
		m.Log.Warn("opting out of restricted chain: M-Chain did not bootstrap in time",
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
		)
		sb.Bootstrapped(chainParams.ID)
		return
	} else if !authorized {
		// Clean opt-out: this node is not entitled. Mirror the
		// VM-plugin-not-loaded skip exactly — mark the slot bootstrapped and
		// return WITHOUT a failing health check, so the node stays healthy while
		// declining to validate a chain it is not authorized for. The specific
		// reason was already logged by authorizeChainActivation.
		m.Log.Info("chain activation not authorized: no ownership attestation entitles this node",
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
		// /v1/health return 503 for any opted-out chain, which made
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

	m.stateChainIdentity(chainParams)

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
		ctx, cancel := context.WithTimeout(context.Background(), vmStartupTimeout)
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

				// AddRoute will build the full path as /v1/<base><endpoint>
				m.Server.AddRoute(handler, chainBase, endpoint)
				if chainAlias != chainParams.ID.String() {
					m.Server.AddRoute(handler, chainIDBase, endpoint)
				}

				// Also register with chain name alias for user-friendly routing (e.g., /v1/bc/zoo/rpc)
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
		routeCtx, routeCancel := context.WithTimeout(context.Background(), vmStartupTimeout)
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
	m.Log.Info("║   → /v1/bc/" + chainParams.ID.String())
	if chainAlias != chainParams.ID.String() {
		m.Log.Info("║   → /v1/bc/" + chainAlias)
	}
	m.Log.Info("╚══════════════════════════════════════════════════════════════════╝")

	// Tell the chain to start processing messages.
	// If the X, P, or C Chain panics, do not attempt to recover
	if chain.Engine != nil {
		startCtx, startCancel := context.WithTimeout(context.Background(), vmStartupTimeout)
		defer startCancel()
		chain.Engine.Start(startCtx, !m.CriticalChains.Contains(chainParams.ID))

		// Start a goroutine to monitor bootstrap completion and notify the chain.
		// This is required because the health check (m.Nets.Bootstrapping()) reports
		// chains as not bootstrapped until sb.Bootstrapped(chainID) is called. Gate on
		// the HANDLER's real initial-sync completion (BootstrapComplete), not the
		// engine's "am I started" flag — the latter returned true immediately at the
		// local last-accepted height, so a behind/empty node was marked bootstrapped
		// before fetching anything.
		go m.monitorBootstrap(chain.Engine, chain.Handler, sb, chainParams.ID)
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
		isPlatformChain := chainParams.ID == constants.PlatformChainID
		if isPlatformChain {
			beacons = chainParams.CustomBeacons
		}

		// expectsStakedBeacons gates the EMPTY-beacon-set behavior of the bootstrap frontier
		// quorum (see blockHandler.expectsStakedBeacons). True for a native NON-platform chain
		// (C/X/Q/...) on a SYBIL-PROTECTED (real staked) network — those sync against the STAKED
		// primary-network validator set (m.Validators, populated by the already-bootstrapped
		// P-chain), so an empty set means "not yet loaded / misconfig → wait then fail safe",
		// NOT "single-node → done". Driven by sybil-protection, NOT --skip-bootstrap: a production
		// validator sets skip-bootstrap and that must NOT make it masquerade as single-node and
		// wedge behind at its stale local tip (tasks #66/#74). See chainExpectsStakedBeacons.
		// Discriminate on the validating Net (chainParams.ChainID), NOT the blockchain ID:
		// deployed C/X/Q carry HASH blockchain IDs, and ids.IsNativeChain only matches the
		// symbolic 111...C alias form (no deployed chain has it). Keying off the blockchain ID
		// (the prior ids.IsNativeChain(chainParams.ID)) was therefore ALWAYS false for the real
		// C-Chain, leaving this fix inert — the exact wedge #66/#74 set out to kill fired anyway.
		isPrimaryNetworkChain := chainValidatesOnPrimaryNetwork(chainParams.ChainID)
		expectsStakedBeacons := chainExpectsStakedBeacons(m.SybilProtectionEnabled, isPrimaryNetworkChain, isPlatformChain)

		// skip-bootstrap forces empty beacons (single-node immediate-start) for every chain EXCEPT
		// one that syncs against the staked set — those MUST always catch a behind validator up from
		// its peers, so emptying their beacons (the prior UNCONDITIONAL override) is precisely the
		// rejoin wedge this fixes. A genuine single-node / dev net runs sybil-protection OFF, so its
		// native chains still fall here and get the empty-beacon immediate-start path.
		if m.SkipBootstrap && !expectsStakedBeacons {
			beacons = &emptyValidatorManager{}
			m.Log.Info("skip-bootstrap enabled - using empty beacons for single-node mode")
		}
		// The beacon set is the ANCHOR for INITIAL SYNC (bootstrap): the blockHandler
		// names the network frontier ONLY from a ⅔-by-stake quorum of these beacons —
		// an arbitrary connected peer can no longer name a forged frontier (the C1
		// forged-chain gate). The validator manager (n.vdrs) is populated by PlatformVM
		// during its initialization via state.initValidatorSets(); for native chains the
		// beacon set IS that primary-network validator set (looked up under networkID =
		// PrimaryNetworkID below). Empty under --skip-bootstrap (single-node), in which
		// case the frontier quorum is inert and the node treats itself as already synced.

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
		// CONSENSUS-SAFETY (single-proposer-per-height): re-wrap multi-validator
		// linear chains in proposervm so block production follows the proposervm's
		// proposer schedule — exactly ONE validator builds height H, the rest wait
		// and vote. Without it every validator's engine calls BuildBlock
		// UNCONDITIONALLY at every height off a slightly-different mempool, so two
		// nodes can each gather an α-of-K cert for DIFFERENT blocks at the same
		// height → reportCertEquivocation → Logger.Crit → crash (also the DEX-tx
		// non-determinism wedge). proposervm's WaitForEvent only returns PendingTxs
		// inside THIS node's proposer window, collapsing the race at its source.
		// This restores the wrapper avalanchego (chains/manager.go) uses; Lux had
		// commented it out and hand-rolled a bridge that dropped the invariant.
		//
		// Gate (all three required):
		//   - K>1            : single-proposer only matters for a multi-validator
		//                      quorum; K==1 (single-node --dev) is trivially one
		//                      proposer and needs no schedule.
		//   - not the P-Chain: the P-Chain publishes its OWN height-indexed
		//                      validators.State DURING its createChain — AFTER the
		//                      chainRuntime.ValidatorState snapshot used here was
		//                      taken — so proposervm's windower would see an empty
		//                      set and degrade to anyone-can-propose with a
		//                      P-chain-height-0 stamp. The P-Chain keeps the existing
		//                      path (its newPChainHeightVM gets the live state, which
		//                      IS published by the K>1 block below). P-Chain blocks
		//                      are rare (staking events) and ⅔-by-stake finality
		//                      makes an equivocation crash require Byzantine
		//                      double-voting, so the residual exposure is low.
		//   - not DAG-native : a VM that implements Linearize (X-Chain) drives the
		//                      engine through a push-notification linearize bridge
		//                      that does not compose with proposervm's pull/window
		//                      model without avalanchego's initializeOnLinearizeVM
		//                      machinery (out of scope). It keeps the existing path.
		// The networkID switch is the DEFAULT; explicitly-set --consensus-* flags
		// are the OVERRIDE. Before this, selection consulted no operator input at
		// all and a five-validator L1 silently ran DefaultParams (K=20, alpha=14,
		// BetaVirtuous=14) — unreachable, so nothing ever finalized.
		consensusParams := m.ConsensusOverrides.ApplyTo(
			selectConsensusParams(m.SybilProtectionEnabled, m.NetworkID))
		if m.SybilProtectionEnabled {
			// LIVE-AWARE, not static. ValidateForValueNetwork applies the STATIC tier
			// floor (mainnet K>=11) — a decentralisation TARGET for a mature set. A
			// running 5-validator mainnet cannot satisfy it, and refusing to start
			// does not add validators: it freezes the fleet on whatever code it last
			// booted. Mainnet 96369 (K=5/alpha=4) was stranded on v1.36.2 by exactly
			// this, missing every later consensus fix — strictly worse than running a
			// small set on current code.
			//
			// ValidateForLiveValueNetwork exists for precisely this and is NOT a
			// bypass: it still rejects f=0 committees (ErrKTooLowForValue) and an
			// operator under-sampling the live set (ErrKBelowLiveFloor, effective
			// floor = min(tier, liveN)). The BFT overlap bound in Valid() applies
			// either way — K=5/alpha=4 satisfies it (2*4-5 = 3 >= f+1 = 2).
			// Native/primary chains register their validators under
			// constants.PrimaryNetworkID (ids.Empty), not their own chain ID — the
			// same resolution the proposervm windower does a few lines below.
			liveN := m.Validators.Count(constants.PrimaryNetworkID)
			if err := consensusParams.ValidateForLiveValueNetwork(m.NetworkID, liveN); err != nil {
				return nil, fmt.Errorf("refusing to start multi-node chain %s with non-BFT consensus params (liveValidators=%d): %w", chainParams.ID, liveN, err)
			}
			// SAFE BUT SMALL. The decentralisation target is REPORTED, never
			// enforced — a set that satisfies the BFT overlap bound must run, but an
			// operator should still see that it is one fault away from the tier the
			// network is meant to have. Silent acceptance is how a 5-validator
			// mainnet stops being a temporary state.
			if err := consensusParams.MeetsDecentralizationTarget(m.NetworkID); err != nil {
				m.Log.Warn("consensus params are Byzantine-SAFE but below the network decentralisation target — grow the validator set",
					log.Stringer("chainID", chainParams.ID),
					log.Int("K", consensusParams.K),
					log.Int("liveValidators", liveN),
					log.Err(err),
				)
			}
		}
		// v1.36 "Nova": the round-scoped VIEW-CHANGE (prevote/POL/lock) was DELETED from the
		// consensus engine (174af3c31). Nova metastable sampling is the sole decider and the ⅔
		// Quasar attestation trails it — there is no view-change to opt into, so the former
		// LUX_CONSENSUS_VIEW_CHANGE env gate is gone. Keep the braid dead.
		_, innerIsDAGNative := vmTyped.(interface {
			Linearize(context.Context, ids.ID, chan<- vm.Message) error
		})
		wrapInProposerVM := shouldWrapInProposerVM(consensusParams.K, chainParams.ID, innerIsDAGNative)

		// Resolve the validator-set ID for this chain ONCE, here — BEFORE the
		// proposervm wrap and the consensus-engine wiring both consume it. This is
		// the SINGLE source of truth (DRY) so the proposervm windower and the cert
		// side cannot disagree (CRITICAL-3: a divergent windower/cert set breaks
		// determinism on non-native chains, and a hardcoded primary set on a
		// sovereign L1 silently defeats single-proposer):
		//   - native/primary chains (P/C/X/Q/A/B/T/Z/G/I/K) register validators under
		//     constants.PrimaryNetworkID (ids.Empty), NOT their individual chain ID.
		//   - a sovereign L1 uses its OWN validator-set ID (chainParams.ChainID ==
		//     its EVM chainID), falling back to primary ONLY if that set is empty.
		networkID := chainParams.ChainID
		isNative := ids.IsNativeChain(chainParams.ID)
		if isNative {
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

		// engineVM is the VM the consensus engine, the WaitForEvent bridge, the
		// block handler, and HTTP registration all use. For a wrapped chain it is
		// the proposervm; build / parse / verify / accept and the proposer-window
		// WaitForEvent all flow THROUGH it (proposervm forwards to the inner VM and
		// carries the proposer signature + P-chain epoch height in the block bytes).
		// For an unwrapped chain it IS the inner VM, so every call below is the
		// original behavior unchanged.
		var engineVM chain.ChainVM = vmTyped
		if wrapInProposerVM {
			proposervmReg, regErr := metrics.MakeAndRegister(m.proposervmGatherer, linearChainAlias)
			if regErr != nil {
				return nil, fmt.Errorf("failed to register proposervm metrics for chain %s: %w", chainParams.ID, regErr)
			}
			minBlkDelay := proposervm.DefaultMinBlockDelay
			if m.ProposerMinBlockDelay > 0 {
				minBlkDelay = m.ProposerMinBlockDelay
			}
			engineVM = proposervm.New(vmTyped, proposervm.Config{
				Upgrades:               m.Upgrades,
				NetworkID:              networkID, // CRITICAL-3: windower uses the SAME set ID as the cert side
				MinBlkDelay:            minBlkDelay,
				NumHistoricalBlocks:    proposervm.DefaultNumHistoricalBlocks,
				StakingLeafSigner:      m.StakingTLSSigner,
				StakingCertLeaf:        m.StakingTLSCert,
				StakingMLDSASigner:     m.StakingMLDSASigner,
				StakingMLDSAPub:        m.StakingMLDSAPub,
				ProposerWindowDuration: m.ProposerWindowDuration,
				Registerer:             proposervmReg,
			})
			m.Log.Info("wrapping chain VM in proposervm for single-proposer-per-height block production",
				log.Stringer("chainID", chainParams.ID),
				log.Int("K", consensusParams.K),
				log.Stringer("windowerNetworkID", networkID),
				log.Duration("minBlockDelay", minBlkDelay))
		}

		m.Log.Info("initializing VM", log.Stringer("chainID", chainParams.ID))
		initCtx, initCancel := context.WithTimeout(context.Background(), vmStartupTimeout)
		defer initCancel()
		// Initialize THROUGH engineVM. proposervm.Initialize builds its windower
		// from chainRuntime.ValidatorState, then initializes the inner VM with the
		// SAME vm.Init (the inner gets the real DB / genesis / ToEngine). For an
		// unwrapped chain engineVM IS the inner VM, so this is the original call.
		err = engineVM.Initialize(
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
			linCtx, linCancel := context.WithTimeout(context.Background(), vmStartupTimeout)
			if err := linearVM.Linearize(linCtx, ids.Empty, toEngine); err != nil {
				linCancel()
				m.Log.Error("failed to linearize VM",
					log.Stringer("chainID", chainParams.ID),
					log.Err(err))
				return nil, fmt.Errorf("failed to linearize VM into linear block mode: %w", err)
			}
			linCancel()
		}

		// Place the VM in BOOTSTRAPPING state (NOT normal operation) after init. The
		// node-layer initial sync (runBootstrapThenPoll → runInitialSync) drives it to the
		// network frontier; ONLY when that completes does the VM transition to Ready (see
		// blockHandler.vmReady, wired below). Going straight to Ready here was the
		// restart-freeze defect: SetState(Ready) fires the EVM's onNormalOperationsStarted
		// (block building, mempool gossip, validator dispatch) at the LOCAL last-accepted
		// height — so a restarted STALE validator served / built at its stale height while
		// the real sync ran detached and non-gating. Bootstrapping fires onBootstrapStarted
		// (snapshots only, no building) — the correct state while the node fetch+executes the
		// gap. The two "go live" transitions (VM serves + engine cert-gates) now happen
		// TOGETHER at the named frontier, never at a stale height.
		// Route SetState through engineVM. For a wrapped chain this is the
		// proposervm, which forwards the state to the inner VM AND records its own
		// consensusState — the gate proposervm.WaitForEvent reads to decide whether
		// to enforce the proposer window. If we set state on the inner VM directly,
		// proposervm.consensusState would stay Unknown and the window would never
		// engage (no single-proposer). For an unwrapped chain engineVM is the inner.
		if stateVM, ok := engineVM.(interface {
			SetState(context.Context, uint32) error
		}); ok {
			m.Log.Info("transitioning VM to bootstrapping (initial sync gates normal operation)",
				log.Stringer("chainID", chainParams.ID))
			stateCtx, stateCancel := context.WithTimeout(context.Background(), vmStartupTimeout)
			if err := stateVM.SetState(stateCtx, uint32(vm.Bootstrapping)); err != nil {
				stateCancel()
				m.Log.Error("failed to transition VM to bootstrapping",
					log.Stringer("chainID", chainParams.ID),
					log.Err(err))
				return nil, fmt.Errorf("failed to transition VM to bootstrapping: %w", err)
			}
			stateCancel()
		}

		// Create integrated consensus engine - the ONE right way to set up chain consensus
		// This consolidates: engine creation, emitter wiring, VM registration
		// blockBuilder is what the engine builds/parses through. For a wrapped chain
		// it is the proposervm (BuildBlock signs + stamps the P-chain height, the
		// block bytes the Quasar cert covers; ParseBlock verifies the proposer sig
		// of a gossiped block), so build and parse route through the SAME wrapper
		// and certs are over consistent bytes. proposervm.VM structurally satisfies
		// consensuschain.BlockBuilder because vm/chain.Block is a type alias for the
		// engine's block.Block.
		var blockBuilder consensuschain.BlockBuilder
		if bb, ok := engineVM.(consensuschain.BlockBuilder); ok {
			blockBuilder = bb
			m.Log.Info("registered VM with consensus engine for block building",
				log.Stringer("chainID", chainParams.ID))
		} else {
			m.Log.Warn("VM does not implement BlockBuilder interface, block building disabled",
				log.Stringer("chainID", chainParams.ID))
		}

		// networkID + isNative were resolved ONCE above (before the proposervm wrap)
		// so the windower and the cert side share the IDENTICAL validator-set ID
		// (CRITICAL-3). Do not re-resolve here.
		m.Log.Info("[CONSENSUS DEBUG] Creating consensus engine for chain",
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("chainParams.ChainID", chainParams.ChainID),
			log.Bool("isNativeChain", isNative),
			log.Stringer("networkIDForValidators", networkID),
			log.Stringer("PrimaryNetworkID", constants.PrimaryNetworkID),
		)

		// consensusParams was selected and value-validated above (before VM
		// Initialize) so the proposervm-wrap decision could read K. It chooses:
		//   - Single-node (--dev, sybil protection disabled): K=1, self-voting.
		//   - Multi-node (sybil-protected): a BYZANTINE-fault-tolerant set selected
		//     by network (Mainnet K=21 / Testnet K=11 / Default K=20 / local-BFT K=4).
		// CRITICAL-2: never LocalParams() (K=3/α=2, f=0, which a single Byzantine
		// validator forks); the value-net validation fails closed otherwise.

		// HIGH-4: wire the α-of-K vote/cert topology for multi-validator (K>1)
		// chains so finality is LIVE (votes broadcast to all, certs gossiped,
		// followers finalize on the cert). Without a verifier a K>1 engine refuses
		// to Start; without the gossiper Mode() reports degraded and value-DEX is
		// refused. For K==1 these stay nil (no quorum, no signatures).
		gossiper := &networkGossiper{net: m.Net, msgCreator: m.MsgCreator, networkID: networkID, log: m.Log}
		// catchup is the behind-follower self-heal wire. The engine signals through
		// it (chain.Catchup) when a gossiped child references a parent we lack; it
		// routes to the blockHandler's GetAncestors transport. The handler wraps the
		// engine built just below, so its handler ref is late-bound at chainInfo
		// construction (a no-op until then — see networkCatchup).
		catchup := &networkCatchup{log: m.Log}
		netCfg := consensuschain.NetworkConfig{
			ChainID:    chainParams.ID,
			NetworkID:  networkID,
			NodeID:     m.NodeID,
			Validators: m.Validators, // CRITICAL: Pass validator sampler for k-peer polling
			Logger:     m.Log,
			Gossiper:   gossiper,
			Catchup:    catchup, // fetch missing ancestors so a behind follower converges
			VM:         blockBuilder,
			Params:     &consensusParams,
		}
		// Keep the finality certs this node assembles on disk, next to the chain they
		// belong to. A peer that fell behind rejoins ONLY by being served the cert for
		// each block in its gap — the network will not re-vote a decided height — so a
		// memory-only cert lasts exactly as long as this process, and how far back we
		// can help a straggler ends up depending on when we last started rather than on
		// the window maxServedCerts names. Opened for EVERY chain, not just a signing
		// one: any node that finalizes can answer a catch-up request, and the more nodes
		// that can, the shallower the hole a restart leaves.
		//
		// Fails OPEN, unlike the vote guard below: an unreadable cert costs a peer one
		// fetch elsewhere, while unreadable equivocation memory risks a fork.
		if certs, certErr := consensuschain.OpenCerts(filepath.Join(chainDataDir, "certs")); certErr != nil {
			m.Log.Warn("could not open the durable finality-cert store — this node can only catch a peer up through heights it finalized since this process started",
				log.Stringer("chainID", chainParams.ID),
				log.Err(certErr))
		} else {
			netCfg.Certs = certs
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
			voteVerifier := newBLSVoteVerifier(vdrState, networkID)
			voteVerifier.log = m.Log
			netCfg.VoteVerifier = voteVerifier
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
			// HIGH-1 durable non-equivocation guard: open the per-chain vote-once store so
			// this signing validator's (height,epoch)→canonical bindings survive a
			// crash/restart. Without it, a crash between casting a vote and finalizing the
			// height would forget the binding and let the restarted node sign a conflicting
			// sibling at that height — a cross-node fork with ZERO Byzantine intent under a
			// rolling upgrade / eviction / OOM. Fail CLOSED on a corrupt store: a signer
			// must not start with equivocation memory it cannot trust.
			voteGuard, guardErr := consensuschain.OpenVoteGuard(filepath.Join(chainDataDir, "vote-guard"))
			if guardErr != nil {
				return nil, fmt.Errorf("refusing to start signing chain %s: durable vote-once guard: %w",
					chainParams.ID, guardErr)
			}
			netCfg.VoteGuard = voteGuard
			// b2: deliver the REAL P-chain epoch height to the engine. The four reads
			// above are height-pinned but the engine reads that height off the VM
			// block (pChainHeightOf) — and a bare plugin block exposes none, so the
			// engine would resolve the set at P-chain height 0 (the GENESIS set),
			// freezing the epoch and dropping every post-genesis validator's vote.
			// Wrap the BlockBuilder so the block the engine builds/parses carries the
			// proposer's live P-chain height (max(GetCurrentHeight, parentH)), stamped
			// into the gossiped bytes so every follower adopts the IDENTICAL height —
			// the set-root/stake/pubkey reads then track the LIVE set at that height.
			// Installed ONLY here (K>1) AND ONLY when this chain is NOT wrapped in
			// proposervm: when wrapInProposerVM is true the blockBuilder already IS
			// the proposervm, whose SignedBlock carries the real P-chain height
			// (selectChildPChainHeight = max(GetCurrentHeight, parentH)) and exposes
			// PChainHeight() — the SAME value the engine's pChainHeightOf reads. That
			// is precisely the proposervm mechanism newPChainHeightVM was a stand-in
			// for, so stacking both would double-stamp the height. We keep
			// newPChainHeightVM only for the unwrapped K>1 chains (P-Chain, X-Chain).
			if blockBuilder != nil && !wrapInProposerVM {
				blockBuilder = newPChainHeightVM(blockBuilder, vdrState, networkID)
				netCfg.VM = blockBuilder
				m.Log.Info("wired P-chain epoch height into consensus block builder (b2)",
					log.Stringer("chainID", chainParams.ID),
					log.Stringer("networkID", networkID))
			}
		}
		// EXPORT-FRONTIER BRIDGE (two-tier consensus, v1.36). VM.Accept now advances the
		// local NOVA (bare-majority) accept tip, which is reorgable and MUST NOT be exported.
		// Push each EXPORT (Quasar, ⅔-by-stake) frontier advance into the VM so the EVM
		// `finalized`/`safe` block tags and the warp cross-chain export gate resolve to the
		// Quasar tip, NEVER the Nova tip (the "semantic collapse" the split exists to prevent).
		//
		// Capability-gated, not just interface-gated: the C-Chain EVM runs as a SEPARATE
		// rpcchainvm plugin process, so vmTyped here is the rpcchainvm *Client, which carries
		// SetLastQuasarFinalized/LastQuasarHeight for EVERY plugin (they cross the ZAP
		// boundary). The client learns from the plugin's Initialize handshake whether the
		// concrete VM actually implements the export capability and reports it via
		// SupportsQuasarExport — false → the observer stays unwired and this chain is Nova-only
		// with no per-finalization cross-process no-op. A VM WITHOUT the probe (an in-process
		// VM whose concrete export methods we hold directly) is treated as capable, preserving
		// the direct-wire path. Push into the RAW inner VM (vmTyped) — the eth/warp backends
		// live there, not on the proposervm wrapper.
		//
		// Ordering: the observer MUST be set on netCfg BEFORE NewRuntime captures it; the boot
		// re-seed needs the constructed engine and so runs after. exportVM (nil unless capable)
		// carries the wired/not-wired decision across that split — a value, not a re-derived
		// predicate.
		var exportVM quasarExportVM
		if qvm, ok := vmTyped.(quasarExportVM); ok {
			capable := true
			if probe, hasProbe := vmTyped.(interface{ SupportsQuasarExport() bool }); hasProbe {
				capable = probe.SupportsQuasarExport()
			}
			if capable {
				exportVM = qvm
				netCfg.QuasarObserver = func(_ ids.ID, height uint64) {
					qvm.SetLastQuasarFinalized(height)
				}
				m.Log.Info("wired EXPORT-frontier (quasar) bridge into the VM (finalized/safe + warp gate track ⅔-stake finality)",
					log.Stringer("chainID", chainParams.ID))
			}
		}
		consensusEngine := consensuschain.NewRuntime(netCfg)

		// Re-seed the consensus EXPORT frontier from the VM's DURABLE Quasar height so
		// GetQuasarTip / QuasarHeight do not regress on restart (the in-memory frontier resets
		// to (Empty,0) until a fresh ⅔-stake cert re-forms; the VM persisted the export height).
		// Advance-only; the observer above refines it as new certs land this session.
		if exportVM != nil {
			if h := exportVM.LastQuasarHeight(); h > 0 {
				consensusEngine.SyncQuasarFrontier(ids.Empty, h)
				m.Log.Info("re-seeded consensus export (quasar) frontier from the VM's durable height on boot",
					log.Stringer("chainID", chainParams.ID), log.Uint64("quasarHeight", h))
			}
		}

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
			syncCtx, syncCancel := context.WithTimeout(context.Background(), vmStartupTimeout)
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

		// Bridge engineVM's WaitForEvent to the toEngine channel.
		// ForwardVMNotifications reads from toEngine; this goroutine is the sole
		// writer. For a WRAPPED chain engineVM is the proposervm, whose WaitForEvent
		// is the single-proposer gate: when the chain is Ready it returns PendingTxs
		// ONLY inside THIS node's proposer window (timeToBuild → Windower.
		// MinDelayForProposer → one scheduled leader per (height, slot)); every other
		// validator BLOCKS until its later slot, by which time the leader has built,
		// gossiped, and finalized that height, so they advance and never build it.
		// That is the invariant the bare-inner WaitForEvent dropped (everyone built
		// every height → conflicting certs → equivocation crash). For an unwrapped
		// chain engineVM is the inner VM — original behavior.
		go func() {
			ctx := context.Background()
			for {
				// Call WaitForEvent on the VM - this blocks until there are pending txs
				// or staker changes that should trigger block building
				msg, err := engineVM.WaitForEvent(ctx)
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
		// Handler parses inbound blocks through the SAME builder the engine emits
		// through (blockBuilder == the P-chain-height wrapper on K>1, the inner VM
		// on K==1), so the container bytes it parses match the bytes the engine
		// framed — one codec, no raw-vs-wrapped split.
		// engineVM is the connectable VM (the proposervm on a wrapped chain, the
		// inner VM otherwise); it carries Connected/Disconnected, which the router
		// dispatches so the P-chain uptime tracker observes validator connectivity.
		//
		// consensusParams.K > 1 is the SAME predicate that wired netCfg.VoteVerifier
		// above, and the verifier is exactly what makes the engine authenticate a
		// vote before tallying it. Passing it here tells the handler that an inbound
		// unsigned Chits can never become a counted vote on this chain, so it must
		// not be converted into one (nova_poll.go).
		bh := newBlockHandler(blockBuilder, engineVM, m.Log, consensusEngine, m.Net, m.MsgCreator, chainParams.ID, networkID, beacons, m.NodeID, expectsStakedBeacons, consensusParams.K > 1)
		// Gate this native chain's bootstrap frontier-TRUST on the P-chain having finished its
		// initial sync, so the staked beacon set (and thus the stake-majority floor denominator)
		// is the TRUE full validator set, not a partial mid-replay set. Wired ONLY for native
		// non-platform chains (expectsStakedBeacons) — the P-chain has no such dependency (it
		// anchors to its own configured CustomBeacons) and must not gate on itself. The signal is
		// m.pChainBootstrapped, published by monitorBootstrap when the P-chain completes initial sync.
		if expectsStakedBeacons {
			bh.primaryNetworkReady = m.pChainBootstrapped.Load
		}
		// Wire the gated normal-operation transition. The bootstrap driver calls this ONCE
		// initial sync has reached the named frontier (runInitialSync) — never before, so the
		// VM cannot serve / build at a stale height. nil when the VM exposes no SetState
		// (degenerate / non-EVM paths): runInitialSync then just marks ready (nothing to
		// transition). The closure captures engineVM: for a wrapped chain SetState(Ready)
		// both (a) flips proposervm.consensusState to Ready — the gate proposervm.
		// WaitForEvent reads to ENGAGE the proposer window (single-proposer turns on
		// exactly at go-live, not at a stale height) — and (b) forwards Ready to the
		// inner VM so the EVM starts building/gossiping. Routing this to the inner VM
		// instead would start the EVM but leave the window disengaged.
		if stateVM, ok := engineVM.(interface {
			SetState(context.Context, uint32) error
		}); ok {
			chainID := chainParams.ID
			bh.vmReady = func(ctx context.Context) error {
				m.Log.Info("initial sync reached the frontier — transitioning VM to normal operation",
					log.Stringer("chainID", chainID))
				return stateVM.SetState(ctx, uint32(vm.Ready))
			}
		}
		// Late-bind the handler as the engine's catch-up wire: it owns requestContext
		// (send GetAncestors) and handleContext -> Put -> engine (deliver the fetched
		// ancestors), so a follower that falls behind now self-heals instead of
		// looping on an unfinalizable orphan.
		catchup.handler = bh
		// Start the proactive frontier poller: a node that received NO gossip (the
		// stranded-validator case) has no other trigger to discover it is behind, so
		// it periodically asks peers for their accepted tip and pulls the gap (with
		// certs) through the catch-up transport. Node-lifetime goroutine (same ctx
		// model as the WaitForEvent bridge above).
		pollerCtx, pollerCancel := context.WithCancel(context.Background())
		bh.pollerCancel = pollerCancel
		// Drive INITIAL SYNC first (fetch+execute from the local tip to the network
		// frontier — the fix for a node that previously declared itself bootstrapped at
		// its local height and synced nothing), then hand off to the live frontier
		// poller. bootstrapDone is the REAL ready signal monitorBootstrap gates on. See
		// bootstrap_sync.go.
		go bh.runBootstrapThenPoll(pollerCtx)
		createdChain = &chainInfo{
			Name:    chainName,
			VMID:    chainParams.VMID,
			Runtime: chainRuntime,
			// engineVM = the proposervm for a wrapped chain (else the inner VM).
			// HTTP registration calls engineVM.CreateHandlers, and proposervm.
			// CreateHandlers forwards the inner VM's handlers (the C-Chain /rpc) and
			// adds /proposervm — so endpoints are preserved. Shutdown/health route
			// through proposervm to the inner.
			VM:      engineVM,
			Engine:  consensusEngine, // Use real consensus engine directly
			Handler: bh,
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
	initCtx, cancelInit := context.WithTimeout(context.Background(), vmStartupTimeout)
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

	// Transition the DAG VM to normal operation if initialization succeeded.
	//
	// DELIBERATE SCOPE (RED MEDIUM): this go-live is UNCONDITIONAL — unlike the LINEAR-chain path
	// (buildChain → runInitialSync), it does NOT gate on a node-layer initial sync reaching the
	// network frontier. The gated lifecycle is implemented only for native LINEAR chains
	// (C/D/B/T/Z), which use consensuschain.Runtime + the blockHandler bootstrap wiring
	// (bootstrap_sync.go). DAG chains (X/Q) run a different engine (consensusdag) with no such
	// node-layer fetch+execute loop, so the stale-go-live gate cannot simply be reused here; adding
	// it requires building the DAG equivalent of the linear bootstrap loop. That is tracked as a
	// SEPARATE task — RECOMMENDED to stay separate, NOT folded into this canary hotfix, because the
	// production native chains that froze are all linear (mainnet runs P + linear C/D/B/T/Z; X/Q
	// carry no at-risk producer state on the canary). This call is UNCHANGED by this fix — the DAG
	// path neither regresses nor improves here; it retains its prior behavior pending that task.
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
	dagHandlerCtx, dagHandlerCancel := context.WithTimeout(context.Background(), vmStartupTimeout)
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
func (m *manager) monitorBootstrap(engine Engine, h handler.Handler, sb nets.Net, chainID ids.ID) {
	// The READY signal is the handler's real initial-sync completion: the bootstrap
	// loop has fetched+executed up to the discovered network frontier. This REPLACES
	// the old engine.IsBootstrapped() poll, which returned true the instant the engine
	// started — at whatever LOCAL last-accepted height the node had (genesis for an
	// empty DB) — so a behind/empty node was declared bootstrapped before syncing a
	// single block. A handler that does not drive initial sync (no BootstrapComplete)
	// falls back to the engine's started flag (degenerate / DAG-less paths).
	type bootstrapCompleter interface{ BootstrapComplete() bool }
	type bootstrapChecker interface{ IsBootstrapped() bool }
	// bootstrapFailer surfaces a fail-SAFE RETURN from the sync loop (F5). A handler that drives
	// initial sync implements it; the watchdog then stops the chain the moment Run returns
	// (eclipse / partition / deep gap) instead of waiting out the 5-min no-progress window.
	type bootstrapFailer interface {
		BootstrapFailed() bool
		BootstrapFailure() error
	}

	var ready func() bool
	if completer, ok := h.(bootstrapCompleter); ok {
		ready = completer.BootstrapComplete
	} else if checker, ok := engine.(bootstrapChecker); ok {
		ready = checker.IsBootstrapped
	} else {
		// Cannot check readiness — assume ready (safe: if we cannot tell, do not pin
		// the chain unbootstrapped forever).
		m.Log.Info("no bootstrap-completion signal available, marking chain as bootstrapped",
			log.Stringer("chainID", chainID))
		sb.Bootstrapped(chainID)
		return
	}

	// PROGRESS signal (H2). The hard 5-minute timer that was set ONCE and never reset
	// killed a healthy-but-slow initial sync (a deep gap legitimately takes longer than
	// 5 minutes). Instead we watch the locally-accepted height: as long as it keeps
	// ADVANCING, the sync is making progress and the stall clock is reset. Only a
	// genuine NO-PROGRESS stall — no height gain for the whole window — is a real failure.
	type bootstrapHeighter interface{ BootstrapHeight() uint64 }
	var heightOf func() uint64
	if hp, ok := h.(bootstrapHeighter); ok {
		heightOf = hp.BootstrapHeight
	}

	// F5 fail-safe RETURN predicate + reason getter (nil for degenerate handlers).
	var failed func() bool
	var failure func() error
	if f, ok := h.(bootstrapFailer); ok {
		failed = f.BootstrapFailed
		failure = f.BootstrapFailure
	}

	// CONNECTING predicate (RED MEDIUM #2 self-heal). A handler that is in its bounded
	// connectivity-RETRY wait (a transient quorum-unreachable fail-safe it is re-attempting) reports
	// this true. The no-progress watchdog treats it as a deliberate quorum WAIT, not a stall: it must
	// NOT force-STOP a node that is correctly failing safe DOWN and waiting for its quorum to return
	// (the network cannot progress without the quorum, and the K8s probes — all polling the
	// always-green /v1/health/liveness — would never restart it, so a stop here is a permanent
	// brick). The node stays in Bootstrapping (serving nothing as head) and converges when the quorum
	// returns. nil for degenerate handlers (legacy behavior).
	type bootstrapConnector interface{ BootstrapConnecting() bool }
	var connecting func() bool
	if c, ok := h.(bootstrapConnector); ok {
		connecting = c.BootstrapConnecting
	}

	// Run the progress-based watchdog (decomplected into watchBootstrapProgress so the
	// loop is unit-testable), then handle the manager-side outcome.
	const bootstrapStallTimeout = 5 * time.Minute
	stallReported := false
	for {
		outcome, stalledAtHeight := watchBootstrapProgress(ready, failed, connecting, heightOf, 100*time.Millisecond, bootstrapStallTimeout, m.chainCreatorShutdownCh)
		switch outcome {
		case bootstrapReady:
			m.Log.Info("chain finished bootstrapping, notifying chain",
				log.Stringer("chainID", chainID))
			// The P-chain completing initial sync means the staked primary-network validator set is
			// now FULLY loaded (every staker tx replayed). Publish that so native non-platform chains
			// can safely begin trusting their beacon set (blockHandler.primaryNetworkReady → the canary
			// partial-set gate). Set BEFORE sb.Bootstrapped so the signal is never lagging the readiness.
			if chainID == constants.PlatformChainID {
				m.pChainBootstrapped.Store(true)
			}
			sb.Bootstrapped(chainID)
			// The M-Chain's committed state is now readable, so entitlement can be
			// decided. Drain any restricted chains parked waiting for it. This must
			// happen HERE rather than at M-Chain creation: M is an engine chain marked
			// bootstrapped asynchronously, so draining at creation would re-run the
			// check while the answer was still "not ready", re-park every chain, and
			// leave them parked with nothing left to drain them.
			if chainID == m.MChainID {
				m.retryPendingRestrictedChains()
			}
			return
		case bootstrapShutdown:
			// Manager is shutting down.
			return
		case bootstrapStalled:
			// A no-progress window says the sync did not advance during THAT WINDOW. It does
			// not say the chain cannot finish, and stopping the engine here turns a slow sync
			// into a chain that can never sync: StopWithError cancels the chain's context, so
			// every later VM.Accept returns "context canceled", the finality guard refuses to
			// advance past the VM's applied state, and the only exit is a pod restart that can
			// stall again the same way.
			//
			// Measured on lux-testnet: a node was declared stalled at height 13955 and its
			// engine stopped — then its EVM went on to reach 14154, the healthy fleet's exact
			// height. The chain had the blocks and was still refusing every incoming cert,
			// because the verdict outlived the condition that produced it.
			//
			// So report it and KEEP WATCHING. The chain stays out of Bootstrapped (it is not
			// synced, and masking that is the other failure), the health check shows exactly
			// why, and if the sync converges the next pass marks it bootstrapped by itself.
			// Only bootstrapFailed below — where the sync loop ITSELF returned "I cannot
			// proceed" — is terminal, because there the engine is reporting its own verdict
			// rather than a watchdog inferring one.
			if !stallReported {
				m.reportBootstrapStall(chainID, ready, stalledAtHeight)
				stallReported = true
			}
			continue
		case bootstrapFailed:
			// F5: the sync loop RETURNED a fail-safe error (eclipse / partition / deep gap). Surface
			// it PROMPTLY with the real reason — not after the 5-min no-progress watchdog. Same
			// manager-side handling as a stall (stop engine + failing health check), but the instant
			// Run returns and with the precise diagnostic so the operator knows to fix peering or
			// state-sync rather than wait.
			reason := errBootstrapTimeout
			if failure != nil {
				if e := failure(); e != nil {
					reason = e
				}
			}
			m.failBootstrapChain(engine, chainID, reason, stalledAtHeight, "bootstrap failed safe (eclipsed/partitioned/too-far-behind)")
			return
		}
	}
}

// reportBootstrapStall publishes a no-progress stall on the chain's health check and
// leaves the engine RUNNING, so the sync can still finish. The check re-reads ready()
// on every call rather than freezing the verdict, which means it clears itself the
// moment the chain converges — a stall that has since been falsified stops being
// reported as a failure. Compare failBootstrapChain, which stops the engine and is
// reserved for a sync loop that reported its own failure.
func (m *manager) reportBootstrapStall(chainID ids.ID, ready func() bool, atHeight uint64) {
	m.Log.Warn("chain bootstrap made no progress in the stall window - NOT marked bootstrapped, still trying",
		log.Stringer("chainID", chainID),
		log.Uint64("height", atHeight))

	chainAlias := m.PrimaryAliasOrDefault(chainID)
	if err := m.Health.RegisterHealthCheck(
		chainAlias+"-bootstrap",
		health.CheckerFunc(func(context.Context) (interface{}, error) {
			if ready != nil && ready() {
				return map[string]interface{}{"chainID": chainID.String()}, nil
			}
			return map[string]interface{}{
				"chainID": chainID.String(),
				"error":   "bootstrap stalled (no progress)",
				"height":  atHeight,
			}, errBootstrapTimeout
		}),
		health.ApplicationTag,
	); err != nil {
		m.Log.Error("failed to register bootstrap stall health check",
			log.Stringer("chainID", chainID),
			log.Err(err))
	}
}

// failBootstrapChain stops a chain whose initial sync did NOT reach the frontier — either a
// no-progress STALL (bootstrapStalled) or an explicit fail-SAFE return from the sync loop
// (bootstrapFailed, F5). It stops the engine with `reason` and registers a failing health check
// carrying it, so the operator sees the precise cause (no-progress vs eclipse/partition/deep-gap)
// and the chain is marked failed rather than silently half-started. Shared by both failure
// outcomes so the stop+health logic lives in exactly one place.
func (m *manager) failBootstrapChain(engine Engine, chainID ids.ID, reason error, atHeight uint64, summary string) {
	m.Log.Error("chain bootstrap did not complete - chain NOT marked as bootstrapped",
		log.Stringer("chainID", chainID),
		log.Uint64("height", atHeight),
		log.String("reason", summary),
		log.Err(reason))

	// Stop the engine with the real reason so the chain is marked failed.
	if err := engine.StopWithError(context.Background(), reason); err != nil {
		m.Log.Error("failed to stop engine after bootstrap failure",
			log.Stringer("chainID", chainID),
			log.Err(err))
	}

	// Register a health check that reports the bootstrap failure (carrying the real reason).
	chainAlias := m.PrimaryAliasOrDefault(chainID)
	healthErr := m.Health.RegisterHealthCheck(
		chainAlias+"-bootstrap",
		health.CheckerFunc(func(context.Context) (interface{}, error) {
			return map[string]interface{}{
				"chainID": chainID.String(),
				"error":   summary,
				"height":  atHeight,
			}, reason
		}),
		health.ApplicationTag,
	)
	if healthErr != nil {
		m.Log.Error("failed to register bootstrap failure health check",
			log.Stringer("chainID", chainID),
			log.Err(healthErr))
	}
}

// bootstrapOutcome is the result of the progress-based bootstrap watchdog.
type bootstrapOutcome int

const (
	bootstrapReady    bootstrapOutcome = iota // ready() reported initial sync complete
	bootstrapStalled                          // no height progress for the stall window
	bootstrapShutdown                         // shutdown was signaled
	bootstrapFailed                           // the sync loop RETURNED a fail-safe error — surface PROMPTLY (F5), not via the no-progress watchdog
)

// watchBootstrapProgress is the PROGRESS-BASED bootstrap watchdog (H2), decomplected from
// the manager-side success/failure handling so it can be unit-tested. It polls ready()
// every tick and returns:
//   - bootstrapReady the instant ready() is true;
//   - bootstrapStalled when heightOf() has NOT advanced for stallTimeout — so a
//     slow-but-ADVANCING sync (each tick the height climbs) resets the clock and is NEVER
//     timed out, while a genuine no-progress stall fails after the window;
//   - bootstrapShutdown when shutdown fires.
//
// It also returns the last observed height (the stall diagnostic). heightOf may be nil
// (then the window is a fixed wall-clock budget from the start — the legacy behavior).
//
// failed (F5) is the PROMPT fail-safe predicate: when the sync loop RETURNS a fail-safe error
// (deep gap / disjoint peer / exhausted attempt bound) the driver flips it, and the watchdog returns
// bootstrapFailed the next tick — instead of waiting out stallTimeout for a chain that has already
// stopped trying. nil disables it (degenerate handlers with no fail-safe signal — legacy behavior).
//
// connecting (RED MEDIUM #2 self-heal) is the deliberate-WAIT predicate: when the driver is in its
// bounded connectivity-RETRY (a transient quorum-unreachable fail-safe it is re-attempting), the
// no-progress window is NOT a stall — the node is correctly failing safe DOWN and waiting for its
// quorum to return, so the clock is reset rather than force-STOPPING it (which the K8s probes would
// never undo → a permanent brick). A genuine stall (NOT connecting, no height progress) still fails.
// nil disables it (legacy behavior).
func watchBootstrapProgress(
	ready func() bool,
	failed func() bool,
	connecting func() bool,
	heightOf func() uint64,
	tick, stallTimeout time.Duration,
	shutdown <-chan struct{},
) (bootstrapOutcome, uint64) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	var lastHeight uint64
	var heightKnown bool
	lastProgress := time.Now()

	for {
		select {
		case <-ticker.C:
			if ready() {
				return bootstrapReady, lastHeight
			}
			// F5: a fail-safe RETURN from the sync loop is surfaced immediately — no point
			// waiting out the no-progress window for a chain that has already given up.
			if failed != nil && failed() {
				return bootstrapFailed, lastHeight
			}
			if heightOf != nil {
				if cur := heightOf(); !heightKnown || cur > lastHeight {
					lastHeight, heightKnown = cur, true
					lastProgress = time.Now()
				}
			}
			if time.Since(lastProgress) >= stallTimeout {
				// A node in its connectivity-RETRY wait is NOT stalled — it is deliberately failing
				// safe DOWN and waiting for its quorum to return (self-heal). Reset the clock and keep
				// waiting; never force-stop it (the K8s probes would never restart it → brick).
				if connecting != nil && connecting() {
					lastProgress = time.Now()
					continue
				}
				return bootstrapStalled, lastHeight
			}
		case <-shutdown:
			return bootstrapShutdown, lastHeight
		}
	}
}

// IsBootstrapped reports whether [id] has ACTUALLY finished initial sync on this
// node: the chain exists AND its validation net has marked it bootstrapped —
// monitorBootstrap called sb.Bootstrapped(id) only after runInitialSync reached the
// named network frontier and transitioned the VM to normal operation (head advanced
// to the frontier, eth-RPC serving live). The old body returned true the instant the
// chain merely EXISTED in m.chains — set right after createChain launched the (async,
// possibly-stalling) bootstrap goroutine — so a C-Chain stalled at genesis (head 0x0,
// never converged) still reported info.isBootstrapped(C)=true, masking the stall from
// any readiness gate keyed on it. Keying on the SAME sb.Bootstrapped signal the health
// check (m.Nets.Bootstrapping) already uses makes info.isBootstrapped track real state,
// so a hands-off rolling upgrade's wait-for-healthy gate can trust it.
func (m *manager) IsBootstrapped(id ids.ID) bool {
	m.chainsLock.Lock()
	_, exists := m.chains[id]
	m.chainsLock.Unlock()
	if !exists {
		return false
	}
	return m.Nets.IsChainBootstrapped(id)
}

func (m *manager) GetChains() []ChainInfo {
	m.chainsLock.Lock()
	defer m.chainsLock.Unlock()

	result := make([]ChainInfo, 0, len(m.chains))
	for id, info := range m.chains {
		result = append(result, ChainInfo{
			ID:   id,
			Name: info.Name,
			VMID: info.VMID,
			// Real per-chain convergence, not mere existence (same fix as IsBootstrapped):
			// a tracked-but-still-syncing chain reports Bootstrapped=false.
			Bootstrapped: m.Nets.IsChainBootstrapped(id),
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

// stateChainIdentity publishes which chain this blockchain actually is, so every
// handshake states it and a peer running a different one is excluded from it.
//
// Everything here is creation data, and this is the one place it is all in hand
// at once: GenesisData is the record the chain was created from — for a chain
// created by a CreateChainTx, the bytes recorded in that transaction, delivered
// to the VM verbatim — and it is digested exactly as delivered, never parsed. Two
// honest nodes on one chain therefore derive identical values with no
// coordination, and two nodes handed different records cannot help but differ.
//
// The upgrade bytes are reported separately and never enforced: a difference
// there means the two nodes agree today and are scheduled to diverge, which is
// worth seeing early and is not grounds for either to exclude the other.
func (m *manager) stateChainIdentity(chainParams ChainParameters) {
	if m.Net == nil {
		return
	}
	cfg, err := m.getChainConfig(chainParams.ID)
	if err != nil {
		// A chain runs without operator config; only the rule generation is
		// unknown, and that is reported rather than enforced.
		m.Log.Debug("no chain config while stating chain identity",
			log.Stringer("chainID", chainParams.ID),
			log.Err(err),
		)
	}
	m.Net.RegisterChainIdentity(peer.ChainIdentity{
		NetworkID: m.NetworkID,
		ChainID:   chainParams.ID,
		VMID:      chainParams.VMID,
		Genesis:   peer.GenesisDigest(chainParams.GenesisData),
		Rules:     peer.RulesDigest(cfg.Upgrade),
	})
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

// chainExpectsStakedBeacons reports whether a chain must sync its bootstrap frontier against the
// STAKED primary-network validator set — the ⅔-by-stake quorum that names the network frontier
// (blockHandler.expectsStakedBeacons, the C1 forged-chain gate). When true, --skip-bootstrap does
// NOT empty the chain's beacon set (buildChain below), so a behind validator always catches up
// from its peers.
//
// It is driven by SYBIL PROTECTION — the true "this is a real staked network" signal — and NOT by
// --skip-bootstrap. Production validators set --skip-bootstrap to skip the initial bootstrap WAIT,
// but that flag must NEVER disable peer-sync on a staked network. Keying this decision off
// --skip-bootstrap (the prior `!m.SkipBootstrap && ...`) is exactly what wedged a behind native
// chain (C/X/Q...): with skip-bootstrap set, the chain got expectsStakedBeacons=false + empty
// beacons, so FrontierTip reported FrontierNoBeacons ("nothing to sync to"), the node named its
// STALE local last-accepted the network frontier, went live there, and never caught up across
// restarts until a manual chaindata wipe (tasks #66/#74). A genuine single-node / dev network runs
// sybil-protection OFF, so it still takes the empty-beacon immediate-start path. The platform chain
// anchors to its own CustomBeacons (not the staked set), so it is excluded here as before; a
// single-VALIDATOR staked net (self-only set) is handled downstream by FrontierTip's
// hasExternalBeacons rule, which reads the LOADED set.
func chainExpectsStakedBeacons(sybilProtectionEnabled, isPrimaryNetworkChain, isPlatformChain bool) bool {
	return sybilProtectionEnabled && isPrimaryNetworkChain && !isPlatformChain
}

// chainValidatesOnPrimaryNetwork reports whether a chain's validating Net IS the primary
// network — the correct discriminator for the "native" C/X/Q/... set that syncs its bootstrap
// frontier against the primary-network STAKED validator set. It keys off the validating Net
// (subnet) ID, NOT the blockchain ID: deployed C/X/Q carry HASH blockchain IDs, whereas
// ids.IsNativeChain only ever matches the symbolic 111...C alias form that NO deployed chain
// uses (only the P-chain, at PlatformChainID=111...P, has a symbolic blockchain ID — and it is
// excluded as the platform chain anyway). Keying the durable-rejoin discriminator off the
// blockchain ID (the prior ids.IsNativeChain(chainParams.ID)) therefore silently excluded every
// real C/X/Q and left the fix inert across restarts (#66/#74). The validating Net is
// PrimaryNetworkID for C/X/Q and each sovereign L1's own net ID for an L2, so this correctly
// keeps the empty-beacon single-node path for L2s while re-enabling peer-sync for C/X/Q.
func chainValidatesOnPrimaryNetwork(validatingNetID ids.ID) bool {
	return validatingNetID == constants.PrimaryNetworkID
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
	// namingProgress carries each anchor's ancestry descent ACROSS bootstrap attempts,
	// keyed by reported tip. It lives HERE, not on the BootstrapPolicy, because a fresh
	// policy is built every attempt: the per-attempt budget resets and the cursor must
	// not, or a gap wider than one attempt can never be crossed however often we retry.
	// Dropped once a frontier is named.
	namingProgress map[ids.ID]*NamingProgress

	vm         consensuschain.BlockBuilder
	logger     log.Logger
	engine     *consensuschain.Runtime    // Consensus engine for proper block handling
	net        network.Network            // Network for sending Qbit responses
	msgCreator message.OutboundMsgBuilder // Message creator for Qbit responses
	chainID    ids.ID                     // Chain ID for message routing
	networkID  ids.ID                     // Network ID for validator routing

	// connector receives peer connect/disconnect notifications routed from the
	// node's chainRouter and forwards them to this chain's VM (e.g. the P-chain
	// uptime tracker; other VMs use them for their own peer sets). connectedNodes
	// dedups delivery so a peer the router dispatches once per tracked network is
	// forwarded to the VM exactly once. A nil connector makes forwarding a no-op.
	connector      chain.ChainVM
	connMu         sync.Mutex
	connectedNodes set.Set[ids.NodeID]

	// Context sync support - when a block fails verification due to missing context,
	// we request the prerequisite blocks from the peer to catch up
	pendingContext   map[ids.ID]contextRequest // Map from blockID to pending context request

	// descentAnchor is the one block this handler walks toward while it is behind.
	// Upstream keeps a single outstanding-request registry (blkReqs, a bimap in
	// snow/engine/snowman/engine.go) so every fetch is deduped and exactly one driver
	// walks the gap. Five callers here can request context and four anchor at the
	// network tip, which asks the wrong question when we are behind: a responder
	// serves the window ENDING at the id it is asked for, so a tip anchor returns
	// blocks above us every time. The descent computes the right next anchor and kept
	// it in a local, so the walk was discarded as soon as another caller re-anchored
	// and it restarted forever.
	//
	// Liveness only. This decides which block we ASK a peer for and grants nothing:
	// finality still requires a verified quorum cert and still advances in contiguous
	// order.
	descentAnchor ids.ID
	requestIDCounter uint32                    // Counter for generating unique request IDs
	maxContextBlocks int                       // Max context blocks to request/serve (default: 256)
	contextRequestMu sync.Mutex                // Protects pendingContext and requestIDCounter

	// serving bounds how much of this node's memory a catch-up request can claim.
	// Answering one walks up to maxContextBlocks blocks out of the VM and assembles
	// them into a single multi-hundred-kilobyte message, so the number of answers
	// being built at once is a memory multiplier — and nothing about the request
	// rate is ours to choose. A node that has fallen behind asks every peer, as
	// fast as its own timer allows, and keeps asking for as long as it stays
	// behind. Unbounded, that turns one lagging peer into this node's memory
	// ceiling.
	//
	// GetAncestors is best-effort and multi-round by design, so declining a request
	// costs the asker one retry. servingPeers keeps one answer in flight per peer
	// (a second question from a peer we are already answering is that same peer
	// asking twice); servingSlots caps the total across all peers.
	servingMu    sync.Mutex
	servingPeers set.Set[ids.NodeID]
	servingSlots chan struct{}
	pollerCancel     context.CancelFunc        // Cancels runFrontierPoller when the handler stops (RED LOW: no goroutine leak on chain re-creation)
	diag             catchupDiag               // Per-outcome counters that gate the catch-up diagnostics (see catchupSample)

	// signedVotesRequired is true exactly when this chain's engine AUTHENTICATES
	// every vote before tallying it — i.e. a VoteVerifier is wired, which the node
	// does for exactly the chains with K>1. It is the Nova/Quasar boundary: on
	// such a chain an unsigned Chits-derived value must never be handed to the
	// engine as a Vote (see nova_poll.go). Deliberately NOT derived from the
	// engine's Mode(), which also demands a cert gossiper and a stake source and
	// therefore under-reports a degraded engine that drops those votes identically.
	signedVotesRequired bool
	// pollUnsignedCount is the volume gate for the unsigned-poll-response
	// diagnostic (see catchupSample); it sits on a per-message path.
	pollUnsignedCount gatomic.Uint64

	// Poll-response buffering — when a peer's preference arrives for a block we do
	// not hold yet, park it and drain when the block arrives. Only a K==1 chain
	// ever parks anything: on a signed-vote chain the response stops at the
	// Nova/Quasar boundary before it can be buffered.
	pendingPollResponses map[ids.ID][]NovaPollResponse // blockID -> parked poll responses
	pendingPollMu        sync.Mutex                    // Protects pendingPollResponses

	// INITIAL SYNC (bootstrap). The handler is BOTH the fetch transport (BlockSource)
	// and the execute sink (Chain) the engine/chain/bootstrap loop drives — see
	// bootstrap_sync.go. While bsActive, inbound frontier/ancestor replies route to the
	// correlation channels below (so the synchronous loop can await them) instead of
	// the live cert/vote path; bootstrapDone is the REAL ready signal monitorBootstrap
	// gates on. bsActive false ⇒ every method behaves exactly as before this change.
	bootstrapDone gatomic.Bool // true once initial sync reached the frontier
	// bootstrapFailed is non-nil once initial sync RETURNED A FAIL-SAFE error (eclipse /
	// partition / deep gap) instead of reaching the frontier (F5). monitorBootstrap polls it so
	// the chain is stopped PROMPTLY with the real reason — not left in the dead window between
	// Run returning and the 5-min no-progress watchdog (where the chain was neither syncing nor
	// stopped). bootstrapDone XOR bootstrapFailed XOR still-syncing.
	bootstrapFailed gatomic.Pointer[bsFailure]
	// bootstrapConnecting is true while runInitialSync is in its bounded connectivity-RETRY wait —
	// a transient beacon-quorum-unreachable fail-safe it is re-attempting (the SELF-HEAL path). It
	// tells monitorBootstrap's no-progress watchdog this is a deliberate WAIT for the quorum to
	// return (the network cannot make progress without it), NOT a stall — so the watchdog does not
	// force-STOP a node that is correctly failing safe and waiting, which (given the K8s probes only
	// poll the always-green /v1/health/liveness) would otherwise be a permanent brick.
	bootstrapConnecting gatomic.Bool
	bsActive            gatomic.Bool             // true while the bootstrap loop is driving
	bsMu                sync.Mutex               // guards bsFrontierCh + bsAncestorCh + bsAheadBeacons
	bsFrontierCh        chan bsFrontierReply     // weighted frontier replies for the current FrontierTip
	bsAncestorCh        map[uint32]chan [][]byte // requestID -> ancestors reply for the current Ancestors
	bsRotor             gatomic.Uint32           // round-robins the Ancestors peer sample (M1: no monopoly)
	// bsAheadBeacons is the set of beacons whose accepted tip, reported in the most recent
	// FrontierTip round, is a block this node does NOT hold — i.e. beacons genuinely AHEAD that
	// therefore HAVE the ancestry the descent needs. sampleAncestorBeacons prefers them so a
	// re-bootstrapping node asks GetAncestors of peers that can serve, never wasting the sample on
	// peers still at genesis (the ava PeerTracker "ask a prover" bias, without a full tracker).
	// Recorded in collectFrontierReplies; empty ⇒ sampleAncestorBeacons falls back to the full
	// rotated set (identical to prior behavior). Guarded by bsMu.
	bsAheadBeacons set.Set[ids.NodeID]

	// vmReady transitions the VM to NORMAL OPERATION (vm.Ready → the EVM's
	// onNormalOperationsStarted: block building, mempool gossip, validator dispatch).
	// runInitialSync calls it EXACTLY ONCE, and ONLY after initial sync has fetch+executed
	// up to the named network frontier — never at the local (possibly stale) height. That
	// ordering is the whole fix: a restarted stale validator stays in Bootstrapping (serving
	// nothing as head) until it has converged, so it can never go live at a stale height.
	// nil for handlers over a VM with no SetState (degenerate / test) → runInitialSync just
	// marks ready. Set in buildChain right after construction.
	vmReady func(context.Context) error

	// beacons is the BEACON / staked-validator set the accepted-frontier quorum is
	// anchored to (m.Validators for native chains, CustomBeacons for the P-chain). The
	// C1 forged-chain gate: ONLY a node in this set, weighted by stake, can contribute to
	// naming the frontier an empty/behind node syncs to — an arbitrary connected peer
	// cannot. nil ⇒ no beacon quorum (single-node / skip-bootstrap), bootstrap is inert.
	beacons validators.Manager

	// selfNodeID is THIS node's own NodeID (m.NodeID). The node is itself a beacon (a
	// validator in `beacons`), and it KNOWS its own accepted frontier with certainty — it
	// cannot, and need not, ask itself over the network. FrontierTip counts this SELF-VOTE in
	// the caught-up determination ONLY under FULL beacon connectivity (the fresh-net fix), so a
	// node whose own stake makes the PEER-ONLY responder weight fall below the stake-majority
	// floor still concludes caught-up at genesis instead of hanging in FrontierConnecting. Empty
	// for degenerate / single-node handlers (no self in the beacon set ⇒ the self-vote is inert).
	selfNodeID ids.NodeID

	// expectsStakedBeacons is true when this chain syncs against the STAKED primary-network
	// validator set (m.Validators) — i.e. a native non-platform chain (C/X/Q/...) on a
	// sybil-protected network. That set is populated by the already-bootstrapped P-chain, so
	// an EMPTY beacon set here means the staked set is not yet loaded or is misconfigured,
	// NOT a single-node network. In that case FrontierTip must WAIT (→ ConnectDeadline → fail
	// safe), never false-complete at the stale height and never fall back to unweighted /
	// endpoint trust (which would reopen C1). False for the P-chain (its CustomBeacons may be
	// legitimately empty under endpoint-only --bootstrap-nodes) and for single-node mode,
	// where an empty beacon set legitimately means "nothing to sync to".
	expectsStakedBeacons bool

	// primaryNetworkReady reports whether the STAKED primary-network validator set this native
	// chain anchors its beacon quorum to is FULLY LOADED — i.e. the P-chain has finished its own
	// initial sync (replaying genesis stakers + every AddValidatorTx). nil ⇒ no gate (the P-chain
	// itself, single-node / skip-bootstrap, and unit tests that wire a static beacon set).
	//
	// THE CANARY ROOT-CAUSE GATE. A native non-platform chain's runBootstrapThenPoll starts right
	// after the chain's VM.Initialize (dispatchChainCreator), NOT after the P-chain has finished
	// BOOTSTRAPPING — so FrontierTip can run while m.Validators.GetMap(PrimaryNetworkID) is still a
	// PARTIAL set. The MinResponseWeight stake-majority floor (total/2+1) is fail-secure ONLY when
	// `total` is the TRUE full-set stake; under a partial denominator a degenerate at-or-below
	// responder subset (the genuinely-ahead producers not yet loaded / not yet connected) clears the
	// floor and the frontier decision false-completes the node at its stale local height — the
	// mainnet luxd-2 freeze. Gating frontier-trust on a fully-loaded set makes the floor's
	// denominator the true full validator set, so the existing fail-secure logic holds. Deadlock-free:
	// the P-chain bootstraps INDEPENDENTLY from its own configured (full-from-config) CustomBeacons,
	// so a native chain only waits a bounded extra moment for the P-chain to converge.
	primaryNetworkReady func() bool

	// wsCheckpointID + wsCheckpointHeight are an OPTIONAL operator-supplied weak-
	// subjectivity anchor (a recent finalized block id at height) the α-agreed frontier
	// must descend from — defense-in-depth for an empty-genesis node. Zero ⇒ disabled.
	wsCheckpointID     ids.ID
	wsCheckpointHeight uint64

	// bootstrapMinResponses / bootstrapAgreement / bootstrapCheckpoint configure the
	// BootstrapTrust policy (bootstrap_trust.go) — the SEPARATE object that decides whether a
	// fetched frontier is safe to BEGIN SYNC FROM, distinct from consensus finality. They are the
	// fix for the mass-recovery deadlock: the acceptance gate is a response FLOOR + a
	// ⅔-of-RESPONDERS agreement, NOT a ⅔-of-CURRENT-total-stake quorum (unsatisfiable when the
	// recovery targets are themselves down validators). All default when zero (bootstrapPolicy):
	//   - bootstrapMinResponses: minimum distinct authenticated configured beacons that must
	//     respond before any frontier is named (INVARIANT 2's partition-capture floor). Zero ⇒ a
	//     MAJORITY of the configured beacon set. For the 5-validator mainnet this is 3, so the 3
	//     reachable producers recover the network while a 2-beacon partition is rejected.
	//   - bootstrapAgreement: the fraction of the RESPONDER weight a named frontier must exceed.
	//     Zero ⇒ ⅔. An operator may tighten to ¾ (Ratio{3,4}).
	//   - bootstrapCheckpoint: an OPTIONAL operator-pinned (id,height) to anchor from when fewer
	//     than the floor respond (the explicit override for the "1 of N reachable" case). nil ⇒
	//     reject below the floor rather than trust a captured minority.
	bootstrapMinResponses int
	bootstrapAgreement    Ratio
	bootstrapCheckpoint   *Checkpoint

	// bootstrapConnectDeadline / bootstrapRetryInterval tune the initial-sync loop
	// (chainbootstrap.Config): how long ONE bs.Run attempt WAITS for beacon connectivity before
	// failing safe (ConnectDeadline — the in-attempt retry that self-heals when beacons return),
	// and the re-sample pause between rounds (RetryInterval, also the inter-attempt backoff). Zero ⇒
	// library defaults (3m / 1s). Operator-overridable; the bootstrap tests set them small to bound
	// the fail-safe path.
	bootstrapConnectDeadline time.Duration
	bootstrapRetryInterval   time.Duration
	// bootstrapMaxAttempts bounds how many times runInitialSync RE-ATTEMPTS bootstrap after a
	// TRANSIENT connectivity fail-safe (the beacon quorum unreachable — eclipse / partition /
	// majority co-restart still in flight). Each attempt is itself bounded (ConnectDeadline) with a
	// backoff between, so this is the OUTER self-heal: the node stays in Bootstrapping (failing safe
	// DOWN — never live at a stale height) and CONVERGES the instant the quorum returns. ≤0 ⇒
	// UNLIMITED (retry until the quorum returns or shutdown) — the production default, because a
	// node without a quorum must keep trying to rejoin and the K8s liveness probe does NOT restart
	// it (all luxd probes poll the always-green /v1/health/liveness). A STRUCTURAL failure (deep
	// gap → state-sync) is never retried regardless. Tests pin it to 1 to assert the single-attempt
	// terminal fail-safe in isolation.
	bootstrapMaxAttempts int
}

// bsFrontierReply is one beacon's accepted-frontier answer, tagged with the responder so
// FrontierTip can weight it by that beacon's stake. The C1 fix turns the frontier from
// "first peer to answer" into "the tip a ⅔-by-stake supermajority of beacons name."
type bsFrontierReply struct {
	nodeID ids.NodeID
	tip    ids.ID
}

// contextRequest tracks a pending context request (wire: GetAncestors)
type contextRequest struct {
	nodeID    ids.NodeID
	requestID uint32
	blockID   ids.ID
	timestamp time.Time
}

// newBlockHandler builds the per-chain inbound handler. signedVotesRequired is a
// CONSTRUCTOR parameter, not a field set afterwards, because its zero value is
// the permissive K==1 legacy path: a site that forgot to state it would silently
// resurrect the unsigned-Chits-to-Vote braid on a quorum chain.
func newBlockHandler(vm consensuschain.BlockBuilder, connector chain.ChainVM, logger log.Logger, engine *consensuschain.Runtime, net network.Network, msgCreator message.OutboundMsgBuilder, chainID ids.ID, networkID ids.ID, beacons validators.Manager, selfNodeID ids.NodeID, expectsStakedBeacons bool, signedVotesRequired bool) *blockHandler {
	return &blockHandler{
		vm:                   vm,
		connector:            connector,
		logger:               logger,
		engine:               engine,
		net:                  net,
		msgCreator:           msgCreator,
		chainID:              chainID,
		networkID:            networkID,
		beacons:              beacons,
		selfNodeID:           selfNodeID,
		expectsStakedBeacons: expectsStakedBeacons,
		signedVotesRequired:  signedVotesRequired,
		pendingContext:       make(map[ids.ID]contextRequest),
		maxContextBlocks:     256, // Default max context blocks to request/serve
		pendingPollResponses: make(map[ids.ID][]NovaPollResponse),
		connectedNodes:       set.NewSet[ids.NodeID](16),
		bsAncestorCh:         make(map[uint32]chan [][]byte),
		servingPeers:         set.NewSet[ids.NodeID](16),
		servingSlots:         make(chan struct{}, maxConcurrentServes),
	}
}

// maxConcurrentServes is how many catch-up answers this node will assemble at once,
// across every peer. Each one holds up to maxContextBlocks blocks plus the message
// built from them, so this number times the message cap is the memory this path can
// claim — a few tens of megabytes, rather than however much peers ask for.
const maxConcurrentServes = 8

// beginServe claims the right to answer nodeID, or reports false when this node is
// already answering that peer or is at its overall limit. A false is not an error:
// the asker retries, and it is better to answer a few peers well than to run every
// peer out of memory.
func (b *blockHandler) beginServe(nodeID ids.NodeID) bool {
	b.servingMu.Lock()
	// Self-arming. A handler assembled as a struct literal rather than through
	// newBlockHandler still gets the bound: the limit protects memory, so it must not
	// be something a construction path can be missing.
	if b.servingSlots == nil {
		b.servingSlots = make(chan struct{}, maxConcurrentServes)
	}
	if b.servingPeers == nil {
		b.servingPeers = set.NewSet[ids.NodeID](16)
	}
	slots := b.servingSlots
	if b.servingPeers.Contains(nodeID) {
		b.servingMu.Unlock()
		return false
	}
	b.servingPeers.Add(nodeID)
	b.servingMu.Unlock()

	select {
	case slots <- struct{}{}:
		return true
	default:
		b.servingMu.Lock()
		b.servingPeers.Remove(nodeID)
		b.servingMu.Unlock()
		return false
	}
}

func (b *blockHandler) endServe(nodeID ids.NodeID) {
	b.servingMu.Lock()
	slots := b.servingSlots
	b.servingPeers.Remove(nodeID)
	b.servingMu.Unlock()
	if slots != nil {
		<-slots
	}
}

// hasBlock returns true if the block is available (either in consensus pendingBlocks or VM storage).
// This is critical for poll-response handling: when a peer names a block we've built or received,
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

// maxPendingContext hard-bounds the in-flight catch-up request map. A Byzantine
// peer can stream AcceptedFrontier replies naming distinct random tips, each
// landing in requestContext; without a bound pendingContext grows without limit
// and OOMs the node (RED HIGH). pendingContextTTL reaps a request once its
// deadline has well passed, so a peer that takes the request then withholds
// Context no longer pins the slot — the block becomes re-requestable from an
// honest peer (RED MEDIUM re-strand). The GetAncestors deadline below is 10s; a
// 30s TTL is comfortably past it.
const (
	maxPendingContext = 4096
	pendingContextTTL = 30 * time.Second
)

// catchupDiag counts each OUTCOME on the cert-gap fetch path — request, serve,
// deliver, apply — one counter per outcome, the shape quorum.go's vote verifier
// already uses. Nothing reads these but the log gate below; they exist so that each
// outcome is sampled against its OWN rate and a common one cannot bury a rare one.
type catchupDiag struct {
	// request side (requestContext)
	noWire, dedup, reaped, evicted, noPeer, unsent gatomic.Uint64
	// serve side (GetContext — the inbound GetAncestors)
	serveNoWire, serveAsked, serveEmpty, serveBusy gatomic.Uint64
	// deliver side (handleContext — the Ancestors reply)
	replyIn, replyNoVM, replyEmpty, replyBootstrap gatomic.Uint64
	// frontier decision (AcceptedFrontier) — the branch that decides whether we fetch
	frontierBootstrap, frontierCurrent, frontierCertGap gatomic.Uint64
	frontierPending, frontierAbsent, frontierNoEngine   gatomic.Uint64
}

// catchupSample is the volume gate for the catch-up diagnostics: the first few of an
// outcome, then every 500th. Same shape as quorum.go's vote-verifier sampler, for the
// same reason — these sites sit on per-message paths, and the fleet runs a cert storm
// (hundreds of signature verifications a second, one node re-gossiping the same certs
// 763 times in 2000 lines), so an unconditional Info line here buries its own signal.
// Sampling still names a systematic failure on its FIRST occurrence and keeps naming
// it while it persists, which is what a single roll needs.
func catchupSample(c *gatomic.Uint64) (uint64, bool) {
	n := c.Add(1)
	return n, n <= 3 || n%500 == 0
}

// requestContext sends a context request (wire: GetAncestors) to fetch missing blocks from a peer
func (b *blockHandler) requestContext(ctx context.Context, nodeID ids.NodeID, blockID ids.ID) {
	// FOLLOW THE WALK. While a descent is under way, every caller asks for the block
	// the descent is walking toward rather than whatever it happened to see. Upstream
	// keeps one outstanding-request registry and one driver for exactly this reason;
	// here five callers can request context and four of them see the network tip. A
	// responder serves the window ENDING at the id it is asked for, so a tip-anchored
	// request returns blocks above a behind node every time — and replaces the anchor
	// the descent had just earned. Measured: four interleaved walks whose oldest
	// heights bounced (24171, 24366, 25288, 26201), none reaching the node's own next
	// height.
	//
	// The anchor clears when the descent connects, so a caught-up node asks for
	// exactly what its caller named. Liveness only: this chooses the question, never
	// the answer, and nothing finalizes without a verified cert.
	b.contextRequestMu.Lock()
	if anchor := b.descentAnchor; anchor != ids.Empty && anchor != blockID {
		blockID = anchor
	}
	b.contextRequestMu.Unlock()

	if b.net == nil || b.msgCreator == nil {
		// The catch-up DECISION was made and there is no wire to carry it. Silence here
		// reads exactly like a node that was never asked to fetch anything.
		if n, loud := catchupSample(&b.diag.noWire); loud {
			b.logger.Warn("catch-up fetch skipped — no network transport on this handler",
				log.Stringer("blockID", blockID),
				log.Uint64("count", n))
		}
		return
	}

	now := time.Now()

	b.contextRequestMu.Lock()
	// Check if we already have a pending request for this block -- but only an
	// UNEXPIRED one suppresses. "A request exists" and "that request is still
	// live" are two different facts; keying suppression on the first alone is
	// what wedged the fleet.
	//
	// The sweep below expires stale slots, but it is reachable ONLY on a cache
	// MISS -- this early return runs first. A node that is behind asks for
	// exactly one block, the one it is missing, so it produces nothing BUT
	// cache hits: the sweep never runs, the slot never expires, and the one
	// block it needs becomes the one block it can never ask for again. Measured
	// on testnet luxd-3: a single slot held 10.9 HOURS past a 30s TTL, 38k
	// suppressions deep, while the node sat 1738 blocks behind a live chain.
	if prior, exists := b.pendingContext[blockID]; exists {
		if age := now.Sub(prior.timestamp); age <= pendingContextTTL {
			b.contextRequestMu.Unlock()
			// Genuinely in flight -- the storm re-asking for a fetch already sent.
			if n, loud := catchupSample(&b.diag.dedup); loud {
				b.logger.Info("catch-up fetch suppressed — a request for this block is already in flight",
					log.Stringer("blockID", blockID),
					log.Stringer("askedOf", prior.nodeID),
					log.Duration("inFlight", age),
					log.Uint64("count", n))
			}
			return
		}
		// Past TTL: the peer took the request and never answered. Drop the slot and
		// fall through to re-request, exactly as the sweep would have. Same policy,
		// now applied to the block actually being asked for.
		delete(b.pendingContext, blockID)
		if n, loud := catchupSample(&b.diag.reaped); loud {
			b.logger.Warn("catch-up request expired in place — re-requesting the block it was holding",
				log.Stringer("blockID", blockID),
				log.Stringer("askedOf", prior.nodeID),
				log.Duration("age", now.Sub(prior.timestamp)),
				log.Uint64("repliesSeen", b.diag.replyIn.Load()),
				log.Uint64("count", n))
		}
	}

	// BOUND the map (RED HIGH + MEDIUM). Reap requests past their TTL first: a
	// withheld Context no longer pins the slot, so the block is re-requestable.
	// Carried out of the critical section — the log lines are emitted after the unlock,
	// never under it.
	reaped := 0
	var reapedID ids.ID
	var reapedOf ids.NodeID
	var reapedAge time.Duration
	for id, req := range b.pendingContext {
		if age := now.Sub(req.timestamp); age > pendingContextTTL {
			delete(b.pendingContext, id)
			reaped++
			if age > reapedAge {
				reapedID, reapedOf, reapedAge = id, req.nodeID, age
			}
		}
	}
	// Then hard-cap: if still at the bound (a genuine flood inside one TTL window),
	// evict the oldest so the map can NEVER exceed maxPendingContext.
	evicted := false
	var evictedID ids.ID
	if len(b.pendingContext) >= maxPendingContext {
		var oldestID ids.ID
		var oldestT time.Time
		first := true
		for id, req := range b.pendingContext {
			if first || req.timestamp.Before(oldestT) {
				oldestID, oldestT, first = id, req.timestamp, false
			}
		}
		delete(b.pendingContext, oldestID)
		evicted, evictedID = true, oldestID
	}

	// Generate a new request ID
	b.requestIDCounter++
	requestID := b.requestIDCounter

	// Record the pending request
	b.pendingContext[blockID] = contextRequest{
		nodeID:    nodeID,
		requestID: requestID,
		blockID:   blockID,
		timestamp: now,
	}
	b.contextRequestMu.Unlock()

	// A slot cleared past its TTL is a request that was never resolved by anything but
	// the reaper — and pendingContext is ONLY cleared here, at the cap, and on the
	// no-peer release, never on a reply. So this line is not by itself proof that no
	// reply came; repliesSeen is. Zero replies across the whole run, with slots expiring,
	// is the Ancestors leg failing, and nothing else on this path says so.
	if reaped > 0 {
		if n, loud := catchupSample(&b.diag.reaped); loud {
			b.logger.Warn("catch-up requests cleared past their TTL — those blocks are requestable again",
				log.Int("reaped", reaped),
				log.Stringer("oldestBlockID", reapedID),
				log.Stringer("oldestAskedOf", reapedOf),
				log.Duration("oldestAge", reapedAge),
				log.Uint64("repliesSeen", b.diag.replyIn.Load()),
				log.Uint64("count", n))
		}
	}
	if evicted {
		if n, loud := catchupSample(&b.diag.evicted); loud {
			b.logger.Warn("catch-up request evicted at the pending cap — distinct missing blocks are arriving faster than one TTL window",
				log.Stringer("blockID", evictedID),
				log.Int("cap", maxPendingContext),
				log.Uint64("count", n))
		}
	}

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

	// PEER SELECTION (defect #3). The consensus layer signals "I hold a VERIFIED cert
	// for a block I don't track — fetch it" by passing ids.EmptyNodeID (topology.go:
	// requestCatchup(cert.Position.BlockID, ids.EmptyNodeID)); picking a real peer is
	// the node layer's job. The prior code blindly Add(EmptyNodeID) + Send, so
	// GetAncestors went to ZERO peers (the "sentTo=0" spam on the frozen fleet) and the
	// certified-but-untracked block was NEVER fetched — the node saw the cert, could not
	// finalize, and never recovered. When nodeID is Empty, sample real connected peers
	// that track this chain's network (the SAME selection pollFrontierOnce uses); a valid
	// cert already gated this request, so asking any network peer is sound (the served
	// gap is cert-verified on accept).
	nodeSet := set.NewSet[ids.NodeID](frontierPollSample)
	// Hoisted out of the range so the peer count is available to the diagnostics below
	// without a second PeerInfo call; range evaluates the expression exactly once either
	// way, so this is the same walk it always was.
	connected := 0
	if nodeID == ids.EmptyNodeID {
		peers := b.net.PeerInfo(nil)
		connected = len(peers)
		for _, p := range peers {
			if p.TrackedChains.Contains(b.networkID) {
				nodeSet.Add(p.ID)
				if nodeSet.Len() >= frontierPollSample {
					break
				}
			}
		}
	} else {
		nodeSet.Add(nodeID)
	}
	if nodeSet.Len() == 0 {
		// No reachable peer to serve the block. Release the pending slot so a later tick
		// (frontier poll → AcceptedFrontier, or a re-gossiped cert) can retry — otherwise
		// the block stays pinned unrequestable until the TTL reap.
		b.contextRequestMu.Lock()
		delete(b.pendingContext, blockID)
		b.contextRequestMu.Unlock()
		// connected > 0 with an empty sample is the TrackedChains filter matching nothing —
		// the same networkID-vs-chainID confusion that once made this filter reject every
		// real peer. connected == 0 is simply an unconnected node.
		if n, loud := catchupSample(&b.diag.noPeer); loud {
			b.logger.Warn("catch-up fetch has NO peer to ask — no GetAncestors is sent",
				log.Stringer("blockID", blockID),
				log.Stringer("advertisedBy", nodeID),
				log.Stringer("networkID", b.networkID),
				log.Int("connectedPeers", connected),
				log.Uint64("count", n))
		}
		return
	}

	sentTo := b.net.Send(msg, nodeSet, b.networkID, 0)
	if sentTo.Len() == 0 {
		// GetAncestors reached ZERO peers: the node holds a verified cert for a block it
		// cannot fetch, and until this line nothing on the wire said so. This is the known
		// historical failure of this exact call, so it is Warn — sampled only so a storm
		// cannot bury its first occurrence.
		if n, loud := catchupSample(&b.diag.unsent); loud {
			b.logger.Warn("catch-up GetAncestors reached ZERO peers — the gap cannot be fetched",
				log.Stringer("blockID", blockID),
				log.Stringer("advertisedBy", nodeID),
				log.Uint32("requestID", requestID),
				log.Int("asked", nodeSet.Len()),
				log.Uint64("count", n))
		}
		return
	}
	b.logger.Info("requested context for missing prerequisites",
		log.Stringer("from", nodeID),
		log.Stringer("blockID", blockID),
		log.Uint32("requestID", requestID),
		log.Int("asked", nodeSet.Len()),
		log.Int("sentTo", sentTo.Len()))
}

func (b *blockHandler) Runtime() *runtime.Runtime                     { return nil }
func (b *blockHandler) Start(ctx context.Context, startReqID uint32)  {}
func (b *blockHandler) Push(ctx context.Context, msg handler.Message) {}
func (b *blockHandler) Len() int                                      { return 0 }
func (b *blockHandler) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}

// catchupEntryMagic tags a CERT-CARRYING catch-up container so a requester can
// tell it apart from a legacy raw-block container. "LCU2" = Lux Catch-Up wire v2.
// A real block's bytes never start with this: an EVM/RLP block opens with an RLP
// list header (0xf8/0xf9/0xfa) and a P/X-chain block with a codec version (0x0000),
// neither of which is 0x4C ('L'). So the discriminator is unambiguous AND a
// cross-version exchange fails CLEANLY: a legacy decoder reads these 4 magic bytes
// as a uint32 length (0x4C435532 ≈ 1.28 GB) which exceeds the buffer, so it rejects
// the frame (0 blocks processed) — no panic, no misparse, no bad accept.
var catchupEntryMagic = [4]byte{'L', 'C', 'U', '2'}

// encodeCatchupEntry frames ONE catch-up container pairing a block with its
// finality cert:
//
//	magic:4 | blockLen:4 | block | certLen:4 | cert            (all big-endian)
//
// certLen == 0 means "no finality cert available for this block" — e.g. a still
// pending (unfinalized) block served on the live missing-parent path; the requester
// then votes on it (the legacy path) instead of cert-accepting. The Ancestors
// flattener wraps this whole entry again as one [entryLen:4][entry] container, so
// the existing outer framing is reused unchanged.
func encodeCatchupEntry(blockBytes, certBytes []byte) []byte {
	out := make([]byte, 0, 4+4+len(blockBytes)+4+len(certBytes))
	out = append(out, catchupEntryMagic[:]...)
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], uint32(len(blockBytes)))
	out = append(out, u32[:]...)
	out = append(out, blockBytes...)
	binary.BigEndian.PutUint32(u32[:], uint32(len(certBytes)))
	out = append(out, u32[:]...)
	out = append(out, certBytes...)
	return out
}

// decodeCatchupEntry is the inverse of encodeCatchupEntry. ok is false (NOT an
// error) when entry is not a v2 cert-carrying frame — no magic, or a structurally
// invalid one (lengths that do not consume the entry EXACTLY). The caller then
// treats entry as a legacy raw block. Strict: any overflow or trailing bytes ⇒ not
// a v2 entry (fail to the legacy interpretation, never a partial parse).
func decodeCatchupEntry(entry []byte) (blockBytes, certBytes []byte, ok bool) {
	if len(entry) < 4+4 || [4]byte{entry[0], entry[1], entry[2], entry[3]} != catchupEntryMagic {
		return nil, nil, false
	}
	rest := entry[4:]
	blockLen := binary.BigEndian.Uint32(rest[:4])
	rest = rest[4:]
	if uint64(blockLen) > uint64(len(rest)) {
		return nil, nil, false
	}
	blockBytes = rest[:blockLen]
	rest = rest[blockLen:]
	if len(rest) < 4 {
		return nil, nil, false
	}
	certLen := binary.BigEndian.Uint32(rest[:4])
	rest = rest[4:]
	if uint64(certLen) != uint64(len(rest)) { // must consume the entry EXACTLY
		return nil, nil, false
	}
	return blockBytes, rest, true
}

// GetContext responds to a request for verification context (parent chain blocks)
// starting from containerID. We respond with up to maxAncestors blocks in
// chronological order (oldest first) so the requester can attach the missing
// context. Each block is paired with its finality cert (encodeCatchupEntry) when we
// have one (CertForBlock) so a BEHIND requester can finalize the gap via the cert
// path; a still-pending block is served with an empty cert (the requester votes on
// it, the legacy live missing-parent behaviour).
func (b *blockHandler) GetContext(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, containerID ids.ID) error {
	if b.vm == nil || b.net == nil || b.msgCreator == nil {
		// A peer asked us to serve a gap and this handler has nothing to serve it with.
		// Unnamed, that is indistinguishable from the request never arriving.
		if n, loud := catchupSample(&b.diag.serveNoWire); loud {
			b.logger.Warn("catch-up request NOT served — no VM or network transport on this handler",
				log.Stringer("from", nodeID),
				log.Stringer("containerID", containerID),
				log.Uint64("count", n))
		}
		return nil
	}

	// Claim a serving slot BEFORE touching the VM. Everything below this line — the
	// block walk, the assembled containers, the wire message — is memory a peer asked
	// us to spend, and a peer that has fallen behind asks continuously, of everyone.
	// Declining costs the asker one retry on a protocol that already retries.
	if !b.beginServe(nodeID) {
		if n, loud := catchupSample(&b.diag.serveBusy); loud {
			b.logger.Info("catch-up request declined — already answering this peer, or at the serving limit",
				log.Stringer("from", nodeID),
				log.Stringer("containerID", containerID),
				log.Uint64("count", n))
		}
		return nil
	}
	defer b.endServe(nodeID)

	if n, loud := catchupSample(&b.diag.serveAsked); loud {
		b.logger.Info("serving a catch-up request",
			log.Stringer("from", nodeID),
			log.Stringer("containerID", containerID),
			log.Uint32("requestID", requestID),
			log.Uint64("count", n))
	}

	// Collect context blocks (walk parent chain).
	//
	// SIZE-CHUNKING (the heavy-DEX-block self-heal fix). The response is the Ancestors
	// wire message, whose UNCOMPRESSED size the peer compressor refuses above the message
	// cap (constants.DefaultMaxMessageSize; the zstd compressor bounds its input to prevent a
	// decompression bomb). The old loop bounded ONLY by COUNT (maxContextBlocks=256), so under
	// heavy DEX load 256 blocks summed to 3.4-5.7 MB > the 2 MB cap, msgCreator.Ancestors FAILED
	// to build, and the behind validator got NOTHING — it could never resync and fell
	// permanently behind (the benchmark-proven stall). We now ALSO bound by serialized size:
	// stop before the accumulated payload would exceed the budget, but ALWAYS include at least
	// one block so a behind node makes progress every round; the requester re-requests for the
	// remaining gap (GetAncestors/context is already a multi-round, oldest-first fill). A single
	// block that alone exceeds the budget is still served (best-effort) so the walk never
	// deadlocks — the trust-tiered validator cap (peer layer) gives such a block the headroom to
	// actually send; for a stranger it will be refused by the tight cap, which is correct.
	var containers [][]byte
	currentID := containerID

	// Leave margin under the cap for the p2p envelope (chainID, requestID, per-container length
	// prefixes, compression framing) so the assembled message stays comfortably below the limit.
	const contextResponseMargin = 128 * 1024 // 128 KiB
	byteBudget := constants.DefaultMaxMessageSize - contextResponseMargin
	accumulated := 0
	truncatedForSize := false

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

		// Pair the block with its finality cert (empty if we have none — a still
		// pending block, or one whose cert aged out of the served window). The
		// requester cert-accepts when a cert is present and votes otherwise. Prepend
		// to keep oldest-first.
		var certBytes []byte
		if b.engine != nil {
			certBytes, _ = b.engine.CertForBlock(blk.ID())
		}
		entry := encodeCatchupEntry(blk.Bytes(), certBytes)

		// SIZE GATE: stop before exceeding the budget — but never drop the FIRST block, so a
		// behind node always receives at least one block per request and cannot deadlock.
		if len(containers) > 0 && accumulated+len(entry) > byteBudget {
			truncatedForSize = true
			break
		}
		accumulated += len(entry)
		containers = append([][]byte{entry}, containers...)

		// Get parent ID for next iteration
		parentID := blk.Parent()
		if parentID == ids.Empty {
			// Reached genesis, stop
			break
		}
		currentID = parentID
	}

	if len(containers) == 0 {
		// We hold neither the requested block nor any ancestor of it, so NOTHING goes back
		// on the wire — the requester gets silence, not an empty batch. This is the serving
		// half of the distinction the requester cannot make on its own.
		if n, loud := catchupSample(&b.diag.serveEmpty); loud {
			b.logger.Warn("catch-up request served NOTHING — we do not hold the requested block, and NO reply is sent",
				log.Stringer("from", nodeID),
				log.Stringer("containerID", containerID),
				log.Uint32("requestID", requestID),
				log.Uint64("count", n))
		}
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
		log.Uint32("requestID", requestID),
		log.Int("numBlocks", len(containers)),
		log.Int("payloadBytes", accumulated),
		log.Bool("truncatedForSize", truncatedForSize),
		log.Int("sentTo", sentTo.Len()))

	return nil
}

// handleContext processes an incoming context response (wire: Ancestors message).
// This is called when we previously requested context for a block we couldn't verify.
// Each block in the context is processed via Put to add it to our state.
// After processing, we drain any buffered Qbits that were waiting for context.
// shouldDescend is the whole descent rule, as one question about values.
//
// A batch that accepted nothing is answering the wrong question: the responder
// serves a window ending at the id it was asked for, so a node further behind than
// that window sees every entry above its tip and refuses all of them. Asking the
// same peer for the same tip returns the same window forever, so we ask instead for
// the oldest entry's PARENT — the next window down — and walk toward our own tip.
//
// The walk ends at genesis. Genesis has height 0 and the empty id for a parent, so a
// batch that starts there has nothing below it; asking a peer for the empty id asks
// for a block that cannot exist. The peer answers "not found" or the plugin answers
// with a zero-length parent, the response carries nothing, and we arrive back here to
// ask the same impossible question of the next peer. Nothing ends that on its own:
// with no finalized ledger every window reads as "above" a tip of zero.
func shouldDescend(certAccepted int, haveOldest bool, oldestHeight uint64, oldestParent ids.ID, finalized uint64, finalizedSet bool) bool {
	if certAccepted > 0 || !haveOldest {
		return false
	}
	if oldestHeight == 0 || oldestParent == ids.Empty {
		return false
	}
	return !finalizedSet || oldestHeight > finalized+1
}

func (b *blockHandler) handleContext(ctx context.Context, nodeID ids.NodeID, requestID uint32, data []byte) error {
	// Count EVERY inbound Ancestors reply, whatever becomes of it: this counter is the
	// answer to "did a reply EVER arrive", which the TTL-expiry line reports and which
	// nothing else on this path carries.
	if n, loud := catchupSample(&b.diag.replyIn); loud {
		b.logger.Info("catch-up response received",
			log.Stringer("from", nodeID),
			log.Uint32("requestID", requestID),
			log.Int("dataLen", len(data)),
			log.Uint64("count", n))
	}
	if b.vm == nil {
		if n, loud := catchupSample(&b.diag.replyNoVM); loud {
			b.logger.Warn("catch-up response DROPPED — no VM on this handler",
				log.Stringer("from", nodeID),
				log.Uint32("requestID", requestID),
				log.Uint64("count", n))
		}
		return nil
	}
	if len(data) == 0 {
		// An EMPTY reply and NO reply are the same silence downstream and mean opposite
		// things: the peer answered and had nothing, or the peer never answered at all.
		// This is the only place the two are distinguishable on the receiving side.
		if n, loud := catchupSample(&b.diag.replyEmpty); loud {
			b.logger.Warn("catch-up response arrived EMPTY — the peer replied with zero bytes (this is NOT a missing reply)",
				log.Stringer("from", nodeID),
				log.Uint32("requestID", requestID),
				log.Uint64("count", n))
		}
		return nil
	}

	// INITIAL SYNC: while the bootstrap loop is driving, this Ancestors reply is the
	// oldest-first batch the loop's Ancestors() requested — hand the raw blocks to that
	// waiting call (correlated by requestID); the loop decides ordering + re-execution.
	// See bootstrap_sync.go.
	if b.deliverBootstrapAncestors(requestID, data) {
		// Consumed by the bootstrap lane: the live cert path never sees these blocks, so
		// a reply landing here explains an unchanged live height.
		if n, loud := catchupSample(&b.diag.replyBootstrap); loud {
			b.logger.Info("catch-up response consumed by initial sync — the live cert path did not see it",
				log.Stringer("from", nodeID),
				log.Uint32("requestID", requestID),
				log.Int("dataLen", len(data)),
				log.Uint64("count", n))
		}
		return nil
	}

	// The outer framing (added by the Ancestors flattener) is
	// [entryLen:4][entry][entryLen:4][entry]..., oldest-first. Each entry is either a
	// v2 cert-carrying frame (magic|blockLen|block|certLen|cert) or a legacy raw
	// block. We decode the outer length, then dispatch the entry:
	//   - cert present → AcceptCatchupBlock: parse → Verify → finalize via the SAME
	//     verified-cert predicate as live finality (the network already decided this
	//     height; we cannot re-vote it). A bad cert is REJECTED here, never finalized.
	//   - no cert / legacy block → Put: the voting path (a fresh near-tip block on the
	//     live missing-parent path, where the network is still voting).
	// Entries are applied in order; a rejected entry (out-of-order, already-have, or
	// bad cert) is skipped and the loop CONTINUES — the next entry may be the right
	// contiguous one, and a behind node re-polls the frontier on its next tick.
	processed := 0
	remaining := data
	// Per-response disposition. processed counts what LANDED; the rest count what did
	// not, and firstReason carries the FIRST refusal verbatim. Held as locals and
	// reported on the one summary line below: the per-entry detail is up to 256 lines a
	// response, which is exactly the volume that buries the summary.
	certAccepted, certRejected, voted, voteFailed, badFrame := 0, 0, 0, 0, 0
	// Where this batch starts, kept so a batch that lands NOTHING can still walk the
	// request window down toward our tip. See the descent after the loop.
	var oldestHeight uint64
	var oldestParent ids.ID
	haveOldest := false
	firstReason := ""
	note := func(reason string) {
		if firstReason == "" {
			firstReason = reason
		}
	}

	for len(remaining) >= 4 {
		entryLen := int(binary.BigEndian.Uint32(remaining[:4]))
		remaining = remaining[4:]
		if entryLen <= 0 || entryLen > len(remaining) {
			badFrame++
			note(fmt.Sprintf("bad frame: entryLen %d with %d bytes remaining", entryLen, len(remaining)))
			b.logger.Debug("invalid context entry length",
				log.Stringer("from", nodeID),
				log.Int("processed", processed),
				log.Int("entryLen", entryLen),
				log.Int("remaining", len(remaining)))
			break
		}

		entry := remaining[:entryLen]
		remaining = remaining[entryLen:]

		// One decode decides the route. A v2 frame yields (block, cert); a legacy raw
		// container yields !isV2, and the whole entry IS the block.
		blockBytes, certBytes, isV2 := decodeCatchupEntry(entry)

		// Remember where this batch STARTS. The responder serves a window ending at the
		// id it was asked for, so on a node further behind than that window the oldest
		// entry still sits above our tip and every entry is refused. The parent of that
		// oldest entry is the next window down, and asking for it is the only thing that
		// moves the window toward us.
		if !haveOldest {
			raw := blockBytes
			if !isV2 {
				raw = entry
			}
			if parsed, perr := b.vm.ParseBlock(ctx, raw); perr == nil {
				oldestHeight, oldestParent, haveOldest = parsed.Height(), parsed.Parent(), true
			}
		}

		if isV2 && len(certBytes) > 0 {
			// CERT path: finalize the gap block on its verified cert (no re-vote).
			if err := b.engine.AcceptCatchupBlock(ctx, blockBytes, certBytes); err != nil {
				certRejected++
				note("cert-accept: " + err.Error())
				b.logger.Debug("catch-up cert-accept rejected (skipping entry)",
					log.Stringer("from", nodeID),
					log.Int("processed", processed),
					log.Err(err))
				continue // not finalized; the cert-gate held — try the next entry
			}
			certAccepted++
			processed++
			continue
		}

		// An entry that arrives WITHOUT its own cert. This is ancestry we asked for,
		// not live gossip: a responder attaches a cert only for blocks inside its
		// served window, so a node behind by more than that window receives a whole
		// response of blocks carrying none.
		//
		// EVERY BLOCK STILL REQUIRES A CERT TO FINALIZE. Nothing is accepted on a
		// peer's say-so. The entry is parsed and locally Verified, then TRACKED —
		// tracking is not finalizing, and AcceptCatchupBlock returns an error for it
		// precisely because it did not finalize. Finality happens in exactly one
		// place, HandleIncomingCert, behind the same α-floor, set-root and
		// VerifyWeighted ⅔-of-stake checks as every other path.
		//
		// What a tracked block buys is that a LATER verified cert can finalize it.
		// That is sound because the chain is hash-linked: a block commits to its
		// parent by hash, so a verified quorum cert on a descendant certifies its
		// entire ancestry — no block can be substituted into that path without
		// breaking the commitment. The fold walks only that certified ancestry, so an
		// uncertified branch is never reachable no matter what a peer sends us.
		//
		// They must NOT go through Put. Put is the live missing-parent path and
		// self-fetches context for each block it cannot place, so routing a 256-entry
		// response through it turned ONE answer into 256 new requests — measured on
		// lux-testnet at 2,614 requests per three minutes against a node that applied
		// nothing. Voting is also the wrong instrument: the network does not re-vote a
		// height it has already decided.
		if !isV2 {
			blockBytes = entry
		}
		if err := b.engine.AcceptCatchupBlock(ctx, blockBytes, nil); err != nil {
			voteFailed++
			note("track: " + err.Error())
			b.logger.Debug("catch-up entry verified and tracked, awaiting a cert (or refused)",
				log.Stringer("from", nodeID),
				log.Int("processed", processed),
				log.Err(err))
			continue // never finalized without a cert — keep draining the batch
		}
		voted++
		processed++
	}

	// One line per response, carrying the whole disposition: what the peer served, how
	// much of it LANDED, how much was refused, and the first reason it was refused. A
	// response of 250 blocks that finalizes none reads identically to a response of one
	// that finalizes it, until this line separates them.
	b.logger.Info("processed context entries",
		log.Stringer("from", nodeID),
		log.Uint32("requestID", requestID),
		log.Int("processed", processed),
		log.Int("certAccepted", certAccepted),
		log.Int("certRejected", certRejected),
		log.Int("voted", voted),
		log.Int("voteFailed", voteFailed),
		log.Int("badFrame", badFrame),
		log.String("firstReason", firstReason))

	// DESCENT. A batch that landed nothing because every entry sits above our next
	// contiguous height is not a failure to be logged — it is the answer to the wrong
	// question. The responder serves a window ending at the id it was asked for, and we
	// asked for the peer's TIP, so on a node further behind than that window the whole
	// batch is above us and AcceptCatchupBlock refuses all of it. Asking again for the
	// same tip gets the same window forever.
	//
	// So ask for the oldest entry's PARENT: that is the next window down. Each response
	// walks the request one window closer until it reaches finalized+1, at which point
	// the batch connects and the normal path drains the rest. One request per response,
	// so this descends rather than amplifies, and requestContext's own TTL gate bounds
	// it.
	//
	// Without this a node between "further behind than one served window" and "far
	// enough behind to bootstrap" has no way home at all: runtime catch-up refuses to
	// descend and the bootstrapper refuses the gap. lux-mainnet luxd-2 and luxd-3 sat
	// in exactly that hole at 1,159,050, ~2,400 blocks behind a live fleet, holding
	// every block they needed.
	// The test is whether the batch ADVANCED us, which is certAccepted — the only
	// counter that means a block became ours. `processed` counts entries handled, and
	// a batch of 250 blocks that finalizes none still reports processed=250, so gating
	// on processed==0 meant the descent almost never fired. Requiring certRejected>0
	// narrowed it further to batches that carried certs at all: a responder serves an
	// empty cert for any block outside its window, so the common batch — every entry
	// above us, none of them certified — reported certRejected=0 and was read as
	// nothing to descend from. Measured on lux-testnet: 2,614 requests, 5 descents.
	// The walk connected — finality advanced — so the anchor has served its purpose
	// and callers go back to naming their own blocks.
	if certAccepted > 0 {
		b.contextRequestMu.Lock()
		b.descentAnchor = ids.Empty
		b.contextRequestMu.Unlock()
	}

	if _, fh, set := b.engine.FinalizedLedger(); shouldDescend(certAccepted, haveOldest, oldestHeight, oldestParent, fh, set) {
		b.logger.Info("catch-up batch was entirely above our tip — descending to its parent",
			log.Stringer("from", nodeID),
			log.Uint64("oldestHeight", oldestHeight),
			log.Uint64("finalized", fh),
			log.Stringer("requesting", oldestParent))
		b.contextRequestMu.Lock()
		b.descentAnchor = oldestParent
		b.contextRequestMu.Unlock()
		b.requestContext(ctx, nodeID, oldestParent)
	}

	return nil
}

// GetAcceptedFrontier answers a peer asking "what is your accepted tip?" with our
// last-accepted block id. This MUST NOT be a no-op: it is how a behind node learns
// the network tip. A no-op here meant every node received an empty frontier and
// assumed its own last-accepted WAS the frontier — so a node that fell behind could
// never discover it was behind (and bootstrap stopped at its own height instead of
// the network's). Same dead-handler class as the GossipOp/catch-up gaps.
func (b *blockHandler) GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time) error {
	if b.vm == nil || b.net == nil || b.msgCreator == nil {
		return nil
	}
	acceptedBlkID, err := b.vm.LastAccepted(ctx)
	if err != nil {
		return nil
	}
	msg, err := b.msgCreator.AcceptedFrontier(b.chainID, requestID, acceptedBlkID)
	if err != nil {
		return nil
	}
	nodeSet := set.NewSet[ids.NodeID](1)
	nodeSet.Add(nodeID)
	b.net.Send(msg, nodeSet, b.networkID, 0)
	return nil
}

// AcceptedFrontier handles a peer's reply to GetAcceptedFrontier: the peer's
// accepted tip. If we do not have that block, we are BEHIND — pull the chain
// ending at it through the catch-up transport (requestContext → GetAncestors;
// the missing-parent path walks down the rest of the gap and the blocks arrive
// via handleContext → Put → engine). This is the proactive half that lets a node
// which missed the block gossip recover without a restart.
func (b *blockHandler) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	if b.vm == nil || containerID == ids.Empty {
		return nil
	}
	// INITIAL SYNC: while the bootstrap loop is driving, this reply is a frontier vote the
	// loop asked for — hand it to FrontierTip TAGGED with the sender (so it can be weighted
	// by the beacon's stake in the ⅔ quorum) and do NOT auto-fetch (the loop owns the
	// descent). See bootstrap_sync.go.
	if b.deliverBootstrapFrontier(nodeID, containerID) {
		b.logFrontierDecision(&b.diag.frontierBootstrap, "consumed by initial sync — the loop owns the descent", nodeID, containerID)
		return nil
	}
	if blk, err := b.vm.GetBlock(ctx, containerID); err == nil {
		// HAVE-BLOCK, LACK-FINALIZATION (defect #5). Holding the peer's tip block does NOT
		// mean we are caught up. A verified-but-unfinalized block (we voted for it, but the
		// α-of-K cert never reached us) leaves us behind on the CERT, not the block bytes.
		// The prior check returned "not behind" on GetBlock success, so such a node NEVER
		// fetched the missing cert and sat stuck at its unfinalized height forever (the exact
		// "verified 288 but no cert" condition). Only "have the block AND it is finalized
		// here" is truly not-behind; otherwise fall through to fetch the cert-carrying gap so
		// AcceptCatchupBlock can finalize it on its verified cert (no re-vote).
		if b.engine == nil {
			// No engine: finalized and merely-held cannot be told apart, so nothing is
			// fetched. That is a degraded handler, not a trace — Warn, as with the mode gate.
			if n, loud := catchupSample(&b.diag.frontierNoEngine); loud {
				b.logger.Warn("frontier reply cannot be judged — no consensus engine on this handler; NOT fetching",
					log.Stringer("from", nodeID),
					log.Stringer("containerID", containerID),
					log.Uint64("count", n))
			}
			return nil
		}
		if fin, ok := b.engine.FinalizedBlockAtHeight(blk.Height()); ok && fin == containerID {
			b.logFrontierDecision(&b.diag.frontierCurrent, "have the block and finalized it — not behind", nodeID, containerID)
			return nil // have the block AND finalized it — truly not behind
		}
		// have the block but not finalized here → behind on the cert → fetch below
		b.logFrontierDecision(&b.diag.frontierCertGap, "have the block but NOT finalized — behind on the CERT, fetching the gap", nodeID, containerID)
	} else if b.engine != nil {
		if _, found := b.engine.GetPendingBlock(containerID); found {
			b.logFrontierDecision(&b.diag.frontierPending, "already tracked as pending by the live path — NOT fetching", nodeID, containerID)
			return nil // already tracked (pending) — the live path is handling it
		}
		b.logFrontierDecision(&b.diag.frontierAbsent, "block absent — fetching the gap", nodeID, containerID)
	} else {
		b.logFrontierDecision(&b.diag.frontierAbsent, "block absent and no engine — fetching the gap", nodeID, containerID)
	}
	b.requestContext(ctx, nodeID, containerID) // behind (missing block OR its cert) → fetch the gap
	return nil
}

// logFrontierDecision names WHICH branch of the frontier decision ran, because that
// branch IS whether we fetch. Every one of them used to be silent, so a node that
// concluded "not behind" and a node that received no reply at all produced byte-
// identical output — the same unanswerable question the Chits sites had.
//
// Sampled per branch: each outcome carries its own counter, so a common decision
// cannot bury a rare one and the first occurrence of every branch is visible.
// ourAcceptedHeight is read from the IN-PROCESS finalized ledger only — never a VM
// round trip, which over ZAP would put an RPC on an inbound message path.
func (b *blockHandler) logFrontierDecision(c *gatomic.Uint64, decision string, nodeID ids.NodeID, containerID ids.ID) {
	n, loud := catchupSample(c)
	if !loud {
		return
	}
	var ourHeight uint64
	if b.engine != nil {
		if _, h, set := b.engine.FinalizedLedger(); set {
			ourHeight = h
		}
	}
	b.logger.Info("frontier reply decision",
		log.String("decision", decision),
		log.Stringer("from", nodeID),
		log.Stringer("containerID", containerID),
		log.Uint64("ourAcceptedHeight", ourHeight),
		log.Uint64("count", n))
}

// frontierPollInterval is the heartbeat at which a node proactively asks a small
// sample of peers "what is your accepted tip?" so it discovers it has fallen behind
// the finalized frontier WITHOUT a restart (the producers do not re-gossip old
// blocks to a behind node, so nothing else tells it). It is a SLOW heartbeat, not a
// hot loop: a reply naming a tip we lack drives AcceptedFrontier → requestContext,
// which is itself throttled downstream (pendingContext per-block dedup + the
// engine's claimCatchupLocked cooldown), so discovery cannot become a fetch storm.
const frontierPollInterval = 15 * time.Second

// frontierPollSample is how many peers each tick is asked. A SMALL sample (not a
// broadcast) keeps the heartbeat cheap; because PeerInfo's order varies per call,
// successive ticks reach different peers, so a behind node that drew a peer which
// cannot serve certs (un-upgraded, or aged-out window) reaches a serving peer on a
// later tick — no single-peer dependency and no hot retry (GAP-4 backoff).
const frontierPollSample = 4

// runFrontierPoller is the proactive half of frontier-sync: it periodically sends
// GetAcceptedFrontier to a few peers tracking this chain. Started once when the
// chain handler is created; exits when ctx is done (node shutdown).
func (b *blockHandler) runFrontierPoller(ctx context.Context) {
	if b.net == nil || b.msgCreator == nil || b.engine == nil {
		return
	}
	ticker := time.NewTicker(frontierPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.pollFrontierOnce(ctx)
		}
	}
}

// pollFrontierOnce asks up to frontierPollSample connected peers (that track this
// chain) for their accepted tip. The reply lands in AcceptedFrontier, which fetches
// the gap if we are behind. Safe no-op when there are no peers (e.g. a single-node
// dev chain): nothing is sent, nothing is fetched.
func (b *blockHandler) pollFrontierOnce(ctx context.Context) {
	peers := b.net.PeerInfo(nil)
	if len(peers) == 0 {
		// Nothing to ask. Per-event: this is the 15s heartbeat, four lines a minute at
		// worst, and its absence is exactly as informative as its presence.
		b.logger.Info("frontier poll skipped — no connected peers",
			log.Stringer("chainID", b.chainID))
		return
	}
	// Filter on the NETWORK id, not the chain id. A peer advertises in its handshake the NETS it
	// tracks (network/peer/peer.go: constants.PrimaryNetworkID plus every tracked L1 net id) — it
	// NEVER advertises an individual native chain id like the C-Chain's. Filtering on b.chainID
	// therefore matched ZERO real peers, so the sample was always empty, no GetAcceptedFrontier was
	// ever sent, and a behind-but-Ready validator NEVER discovered it was behind (it sat frozen at a
	// stale height with no self-heal — the second half of the luxd-2 freeze). This is the IDENTICAL
	// fix already applied to connectedBeacons (bootstrap_sync.go); the NormalOp poller had the same
	// latent chainID/networkID confusion. Matching on b.networkID reaches exactly the peers
	// participating in this chain's validation network. Catch-up safety is unchanged: the reply
	// drives requestContext, which pulls the gap WITH CERTS (cert-verified accept), so asking any
	// network peer is sound.
	sample := set.NewSet[ids.NodeID](frontierPollSample)
	for _, p := range peers {
		if p.TrackedChains.Contains(b.networkID) {
			sample.Add(p.ID)
			if sample.Len() >= frontierPollSample {
				break
			}
		}
	}
	if sample.Len() == 0 {
		// Peers ARE connected and none of them matched the filter above. That is the exact
		// shape of the failure this filter has already caused once: matching on the wrong
		// id emptied every sample, no GetAcceptedFrontier was ever sent, and a behind node
		// never learned it was behind — with no line saying so.
		b.logger.Warn("frontier poll skipped — no connected peer tracks this network; the tip is never asked for",
			log.Stringer("chainID", b.chainID),
			log.Stringer("networkID", b.networkID),
			log.Int("connectedPeers", len(peers)))
		return
	}

	b.contextRequestMu.Lock()
	b.requestIDCounter++
	requestID := b.requestIDCounter
	b.contextRequestMu.Unlock()

	msg, err := b.msgCreator.GetAcceptedFrontier(b.chainID, requestID, 10*time.Second)
	if err != nil {
		b.logger.Warn("frontier poll: failed to build GetAcceptedFrontier", log.Err(err))
		return
	}
	sentTo := b.net.Send(msg, sample, b.networkID, 0)
	b.logger.Info("frontier poll sent",
		log.Stringer("chainID", b.chainID),
		log.Uint32("requestID", requestID),
		log.Int("asked", sample.Len()),
		log.Int("sentTo", sentTo.Len()))
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
	b.logger.Info("responded with Chits",
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
				if b.logger != nil {
					// handleVote returns BEFORE the signature check when the block is
					// not in pendingBlocks — it buffers the vote and fires a fetch
					// instead. So "known" here is what decides whether this vote can
					// EVER be counted, and it is the difference between an ID-space
					// mismatch (vote keyed differently from pendingBlocks) and an
					// eviction race (block dropped before peers' votes land). Also
					// report whether the VM holds the block at all, which separates
					// those two: unknown to both = wrong ID, known to the VM but not
					// pending = evicted.
					known := b.engine.HasPendingBlock(blockID)
					inVM := false
					if b.vm != nil {
						if _, err := b.vm.GetBlock(ctx, blockID); err == nil {
							inVM = true
						}
					}
					b.logger.Info("quorum: vote RECEIVED",
						log.Stringer("from", nodeID), log.Stringer("blockID", blockID),
						log.Bool("inPendingBlocks", known), log.Bool("inVM", inVM))
					if !known {
						b.logger.Warn("quorum: vote is for a block NOT in pendingBlocks — it will be buffered, never counted",
							log.Stringer("from", nodeID), log.Stringer("blockID", blockID),
							log.Bool("inVM", inVM))
					}
				}
				b.engine.HandleIncomingVote(blockID, payload)
			case quorumKindCert:
				if b.logger != nil {
					b.logger.Info("quorum: CERT received",
						log.Stringer("from", nodeID), log.Stringer("blockID", blockID))
				}
				b.engine.HandleIncomingCert(payload)
			}
			return nil
		}
	} else if b.engine != nil && b.logger != nil {
		// The gate is silent by construction: outside quorum-finality mode every
		// inbound vote and cert falls through to the plain-block path and is
		// discarded, so a degraded engine looks exactly like a network with
		// nothing to say. Name it once per gossip so the mode is visible.
		b.logger.Warn("quorum: engine NOT in quorum-finality mode — vote/cert demux skipped",
			log.Stringer("from", nodeID), log.Int("mode", int(b.engine.Mode())))
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

// Connected satisfies handler.Handler, whose interface carries no peer version.
// The real delivery path is ConnectedWithVersion (chainRouter uses it via the
// versionedConnector capability); this nil-version entry exists only for the
// interface contract and any non-router caller.
func (b *blockHandler) Connected(ctx context.Context, nodeID ids.NodeID) error {
	return b.connect(ctx, nodeID, nil)
}

// ConnectedWithVersion forwards a peer connection WITH its real application
// version to this chain's VM. chainRouter invokes this (detecting the
// versionedConnector capability) so the version survives to the inner VM:
// proposervm promotes Connected to the C-Chain (coreth), whose state-sync peer
// tracker compares peer versions — a nil version there dereferences nil and
// panics a state-syncing node (a fresh join OR a validator rejoining after
// falling behind). This mirrors avalanchego, which delivers msg.NodeVersion to
// engine.Connected.
func (b *blockHandler) ConnectedWithVersion(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	return b.connect(ctx, nodeID, nodeVersion)
}

// connect forwards a peer connection to this chain's VM exactly once, carrying
// nodeVersion (nil only on the interface path). The P-chain VM records it in its
// uptime tracker; the C-Chain/X-Chain VMs store the version for their peer sets.
// A nil connector makes this a no-op. On a forwarding error the dedup entry is
// rolled back so a subsequent dispatch of the same connection retries.
func (b *blockHandler) connect(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	if b.connector == nil {
		return nil
	}
	b.connMu.Lock()
	if b.connectedNodes.Contains(nodeID) {
		b.connMu.Unlock()
		return nil
	}
	b.connectedNodes.Add(nodeID)
	b.connMu.Unlock()

	if err := b.connector.Connected(ctx, nodeID, nodeVersion); err != nil {
		b.connMu.Lock()
		b.connectedNodes.Remove(nodeID)
		b.connMu.Unlock()
		return err
	}
	return nil
}

// Disconnected forwards a peer disconnection to this chain's VM exactly once.
func (b *blockHandler) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if b.connector == nil {
		return nil
	}
	b.connMu.Lock()
	if !b.connectedNodes.Contains(nodeID) {
		b.connMu.Unlock()
		return nil
	}
	b.connectedNodes.Remove(nodeID)
	b.connMu.Unlock()

	return b.connector.Disconnected(ctx, nodeID)
}
func (b *blockHandler) HealthCheck(ctx context.Context) (interface{}, error) { return nil, nil }
func (b *blockHandler) Stop(ctx context.Context) {
	if b.pollerCancel != nil {
		b.pollerCancel()
	}
}
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
		// Wire op "Vote" is a MISNOMER carried over from the router: the message is
		// an UNSIGNED Chits, i.e. a peer's PREFERENCE. It becomes a NovaPollResponse
		// — a type with no Signature and no Accept field — and can no longer be
		// widened into a consensuschain.Vote. See nova_poll.go.
		//
		// msg.Message already holds the extracted PreferredId (chain_router.go ->
		// GetContainerBytes returns m.GetPreferredId()); AcceptedId/AcceptedHeight
		// are discarded there and never reach this handler.
		b.logger.Info("received Chits message",
			log.Stringer("from", msg.NodeID),
			log.Uint32("requestID", msg.RequestID),
			log.Int("msgLen", len(msg.Message)))
		if len(msg.Message) >= 32 {
			var preferredID ids.ID
			copy(preferredID[:], msg.Message[:32])

			b.logger.Debug("extracted preferredID",
				log.Stringer("from", msg.NodeID),
				log.Stringer("preferredID", preferredID))

			b.receivePollResponse(ctx, NovaPollResponse{
				From:        msg.NodeID,
				PreferredID: preferredID,
				RequestID:   msg.RequestID,
				ReceivedAt:  time.Now(),
			})
		} else {
			b.logger.Warn("message too short for preferredID",
				log.Stringer("from", msg.NodeID),
				log.Int("msgLen", len(msg.Message)))
		}
	case handler.GetAcceptedFrontier:
		// A peer asks "what is your accepted tip?" — answer with our last-accepted
		// block id (the frontier responder). This is the SERVE half of frontier-sync:
		// it is how a behind node learns the network tip. Without this case the op
		// reaches HandleInbound and falls through (dropped), so a node that fell
		// behind could never discover it (the GAP-1 dead-handler, same class as the
		// GossipOp/catch-up gaps).
		return b.GetAcceptedFrontier(ctx, msg.NodeID, msg.RequestID, time.Now().Add(10*time.Second))
	case handler.AcceptedFrontier:
		// A peer's reply naming ITS accepted tip (a 32-byte container id). If we lack
		// that block we are BEHIND → pull the gap through the catch-up transport. This
		// is the LEARN half of frontier-sync; parse the id like the Context case.
		if len(msg.Message) >= 32 {
			var containerID ids.ID
			copy(containerID[:], msg.Message[:32])
			return b.AcceptedFrontier(ctx, msg.NodeID, msg.RequestID, containerID)
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
// ancestorRequester is the one capability networkCatchup needs from the inbound
// handler: ask a peer for the chain of blocks ending at a missing block.
// *blockHandler provides it via requestContext (GetAncestors out; the Ancestors
// response flows handleContext -> Put -> engine). Depending on this one-method
// interface (not the concrete handler) keeps the bridge testable and narrow.
type ancestorRequester interface {
	requestContext(ctx context.Context, from ids.NodeID, blockID ids.ID)
}

// networkCatchup is the node-side wire for the consensus engine's chain.Catchup
// interface. The engine DECIDES when to catch up — followVerifiedBlock sees a
// gossiped child whose parent it lacks and calls requestCatchup; networkCatchup
// ROUTES that to the handler's GetAncestors transport. Splitting "decide" (engine,
// no network types) from "transport" (handler, no consensus policy) keeps each in
// its lane. Without this wire a follower that falls behind during consensus loops
// on an unfinalizable orphan forever — the stranded-follower bug.
//
// The handler wraps the engine and is built after it, so `handler` is late-bound
// once both exist. Until then RequestAncestors is a harmless no-op: no block can
// be missing before the engine has even started.
type networkCatchup struct {
	handler ancestorRequester
	log     log.Logger     // nil before late-binding and in tests — every use is guarded
	nWired  gatomic.Uint64 // catch-up signals routed to the wire
	nNoWire gatomic.Uint64 // catch-up signals that found no wire to route to
}

// RequestAncestors satisfies chain.Catchup. chainID/networkID are already fixed
// per-handler (one handler per chain), so the wire needs only the missing block
// and the peer that advertised its child.
func (c *networkCatchup) RequestAncestors(_ ids.ID, _ ids.ID, missingBlockID ids.ID, from ids.NodeID) error {
	if c.handler == nil {
		// The engine holds a VERIFIED cert for a block it does not track and asked for the
		// gap, and there is no wire to ask over. That is the stranded-follower bug with its
		// fix present but unbound, and it is INVISIBLE: a node that never fetches looks
		// exactly like a node that was never asked to. Warn, not trace.
		if n, loud := catchupSample(&c.nNoWire); loud && c.log != nil {
			c.log.Warn("catch-up requested but the wire is NOT bound — no GetAncestors will be sent",
				log.Stringer("missingBlockID", missingBlockID),
				log.Stringer("from", from),
				log.Uint64("count", n))
		}
		return nil
	}
	// from is EMPTY when the engine holds a verified cert for an untracked block and
	// leaves peer selection to the node layer (requestContext samples), and a real peer
	// when a gossiped child named its missing parent. Which one it is decides which
	// selection path runs, so it is on the line.
	if n, loud := catchupSample(&c.nWired); loud && c.log != nil {
		c.log.Info("catch-up requested for a block we do not track",
			log.Stringer("missingBlockID", missingBlockID),
			log.Stringer("from", from),
			log.Uint64("count", n))
	}
	c.handler.requestContext(context.Background(), from, missingBlockID)
	return nil
}

type networkGossiper struct {
	net        network.Network
	msgCreator message.OutboundMsgBuilder
	networkID  ids.ID
	log        log.Logger
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
	// Every return of 0 here is a vote that never left this node, and all three
	// paths used to be silent — which is indistinguishable from "no vote was due"
	// when a chain stops finalizing.
	if g.net == nil || g.msgCreator == nil {
		if g.log != nil {
			g.log.Warn("quorum: vote NOT broadcast — nil transport",
				log.Stringer("blockID", blockID))
		}
		return 0
	}
	envelope := encodeQuorumGossip(quorumKindVote, blockID, voteBytes)
	msg, err := g.msgCreator.Gossip(chainID, envelope)
	if err != nil {
		if g.log != nil {
			g.log.Warn("quorum: vote NOT broadcast — gossip build failed",
				log.Stringer("blockID", blockID), log.Err(err))
		}
		return 0
	}
	sent := g.net.Gossip(msg, nil, g.networkID, -1, 0, 0).Len()
	if g.log != nil {
		g.log.Info("quorum: vote broadcast",
			log.Stringer("blockID", blockID),
			log.Stringer("gossipNetworkID", g.networkID),
			log.Int("sentToPeers", sent),
			log.Int("voteBytes", len(voteBytes)))
	}
	return sent
}

// v1.36 "Nova": BroadcastPrevote was DELETED — the round-scoped view-change (prevote/POL/lock)
// it fed no longer exists in the consensus engine (174af3c31). Nova sampling decides; the ⅔
// Quasar attestation (a plain accept-vote, gossiped via BroadcastVote) trails it. Keep the braid dead.

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
		if g.log != nil {
			g.log.Warn("quorum: CERT assembled but not gossiped — build failed",
				log.Stringer("blockID", blockID), log.Err(err))
		}
		return 0
	}
	sent := g.net.Gossip(msg, nil, g.networkID, -1, 0, 0).Len()
	if g.log != nil {
		g.log.Info("quorum: CERT assembled and gossiped",
			log.Stringer("blockID", blockID), log.Int("sentToPeers", sent))
	}
	return sent
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

// The outbound poll-response transport (SendPollResponse, and the SendVote
// misnomer the consensuschain.Gossiper interface still demands) lives with the
// rest of the Nova poll path in nova_poll.go. SendQbit was a byte-identical
// duplicate of it with zero callers and is gone — one implementation, one name.
