// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package teleportvm implements the Teleport Virtual Machine (T-Chain) for the Lux network.
// TeleportVM is the unified cross-chain data movement VM that handles:
//   - Bridge: deposit/mint/burn/release proofs with MPC threshold signing
//   - Relay: cross-chain message relay with channels and receipts
//   - Oracle: price feeds and data attestation
//
// One VM, one signer set, one bond, one chain.
package teleportvm

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/artifacts"
	"github.com/luxfi/runtime"
	"github.com/luxfi/threshold/pkg/party"
	"github.com/luxfi/threshold/pkg/pool"
	"github.com/luxfi/threshold/protocols/cmp/config"
	"github.com/luxfi/vm/chain"
	vmcore "github.com/luxfi/vm"
	"github.com/luxfi/warp"
)

var (
	_ chain.ChainVM = (*VM)(nil)

	Version = &version.Semantic{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	// DB keys
	lastAcceptedKey = []byte("lastAccepted")
	messagePrefix   = []byte("msg:")
	channelPrefix   = []byte("chan:")
)

// =========================================================================
// Configuration
// =========================================================================

// Config contains unified TeleportVM configuration
type Config struct {
	// Bridge settings
	MinConfirmations uint32   `json:"minConfirmations"`
	BridgeFee        uint64   `json:"bridgeFee"`
	SupportedChains  []string `json:"supportedChains"`
	MaxBridgeAmount  uint64   `json:"maxBridgeAmount"`
	DailyBridgeLimit uint64   `json:"dailyBridgeLimit"`

	// Signer set (LP-333)
	RequireValidatorBond uint64  `json:"requireValidatorBond"` // 1M LUX bond (slashable)
	MaxSigners           int     `json:"maxSigners"`
	ThresholdRatio       float64 `json:"thresholdRatio"`

	// MPC
	MPCThreshold    int `json:"mpcThreshold"`
	MPCTotalParties int `json:"mpcTotalParties"`

	// Relay settings
	MaxMessageSize    int      `json:"maxMessageSize"`
	ConfirmationDepth int      `json:"confirmationDepth"`
	RelayTimeout      int      `json:"relayTimeout"`
	TrustedRelayers   []string `json:"trustedRelayers"`

	// Oracle settings
	MaxFeedsPerBlock    int    `json:"maxFeedsPerBlock"`
	ObservationWindow   string `json:"observationWindow"`
	MinObservers        int    `json:"minObservers"`
	AggregationMethod   string `json:"aggregationMethod"`
	DeviationThreshold  uint64 `json:"deviationThreshold"`
	EnableZKAggregation bool   `json:"enableZkAggregation"`
	ZKProofSystem       string `json:"zkProofSystem"`
	RequireQuorumCert   bool   `json:"requireQuorumCert"`
	QuorumThreshold     int    `json:"quorumThreshold"`
}

// DefaultConfig returns sane defaults
func DefaultConfig() Config {
	return Config{
		// Bridge defaults
		MinConfirmations:     6,
		RequireValidatorBond: 1_000_000 * 1e9, // 1M LUX bond
		MaxSigners:           100,
		ThresholdRatio:       0.67,

		// Relay defaults
		MaxMessageSize:    1024 * 1024,
		ConfirmationDepth: 6,
		RelayTimeout:      300,

		// Oracle defaults
		MaxFeedsPerBlock:    100,
		ObservationWindow:   "1m",
		MinObservers:        3,
		AggregationMethod:   "median",
		DeviationThreshold:  500,
		EnableZKAggregation: false,
		ZKProofSystem:       "groth16",
		RequireQuorumCert:   false,
		QuorumThreshold:     2,
	}
}

// =========================================================================
// Shared types
// =========================================================================

// SignerSet tracks the current MPC signer set (LP-333)
// First 100 validators opt-in without reshare. Reshare ONLY on slot replacement.
type SignerSet struct {
	Signers      []*SignerInfo `json:"signers"`
	Waitlist     []ids.NodeID  `json:"waitlist"`
	CurrentEpoch uint64        `json:"currentEpoch"`
	SetFrozen    bool          `json:"setFrozen"`
	ThresholdT   int           `json:"thresholdT"`
	PublicKey    []byte        `json:"publicKey"`
}

// SignerInfo contains information about a signer in the set
type SignerInfo struct {
	NodeID     ids.NodeID `json:"nodeId"`
	PartyID    party.ID   `json:"partyId"`
	BondAmount uint64     `json:"bondAmount"` // 1M LUX bond (slashable, NOT staked)
	MPCPubKey  []byte     `json:"mpcPubKey"`
	Active     bool       `json:"active"`
	JoinedAt   time.Time  `json:"joinedAt"`
	SlotIndex  int        `json:"slotIndex"`
	Slashed    bool       `json:"slashed"`
	SlashCount int        `json:"slashCount"`
}

// RegisterValidatorInput is the input for registering as a signer
type RegisterValidatorInput struct {
	NodeID     string `json:"nodeId"`
	BondAmount string `json:"bondAmount,omitempty"`
	MPCPubKey  string `json:"mpcPubKey,omitempty"`
}

// RegisterValidatorResult is the result of registering
type RegisterValidatorResult struct {
	Success        bool   `json:"success"`
	NodeID         string `json:"nodeId"`
	Registered     bool   `json:"registered"`
	Waitlisted     bool   `json:"waitlisted"`
	SignerIndex    int    `json:"signerIndex"`
	WaitlistIndex  int    `json:"waitlistIndex,omitempty"`
	TotalSigners   int    `json:"totalSigners"`
	Threshold      int    `json:"threshold"`
	ReshareNeeded  bool   `json:"reshareNeeded"`
	CurrentEpoch   uint64 `json:"currentEpoch"`
	SetFrozen      bool   `json:"setFrozen"`
	RemainingSlots int    `json:"remainingSlots"`
	Message        string `json:"message"`
}

// SignerSetInfo is the result of getting signer set information
type SignerSetInfo struct {
	TotalSigners   int           `json:"totalSigners"`
	Threshold      int           `json:"threshold"`
	MaxSigners     int           `json:"maxSigners"`
	CurrentEpoch   uint64        `json:"currentEpoch"`
	SetFrozen      bool          `json:"setFrozen"`
	RemainingSlots int           `json:"remainingSlots"`
	WaitlistSize   int           `json:"waitlistSize"`
	Signers        []*SignerInfo `json:"signers"`
	PublicKey      string        `json:"publicKey,omitempty"`
}

// SignerReplacementResult is the result of replacing a failed signer
type SignerReplacementResult struct {
	Success           bool   `json:"success"`
	RemovedNodeID     string `json:"removedNodeId,omitempty"`
	ReplacementNodeID string `json:"replacementNodeId,omitempty"`
	ReshareSession    string `json:"reshareSession,omitempty"`
	NewEpoch          uint64 `json:"newEpoch"`
	ActiveSigners     int    `json:"activeSigners"`
	Threshold         int    `json:"threshold"`
	Message           string `json:"message"`
}

// SlashSignerInput is the input for slashing a bridge signer
type SlashSignerInput struct {
	NodeID       ids.NodeID `json:"nodeId"`
	Reason       string     `json:"reason"`
	SlashPercent int        `json:"slashPercent"`
	Evidence     []byte     `json:"evidence"`
}

// SlashSignerResult is the result of slashing
type SlashSignerResult struct {
	Success         bool   `json:"success"`
	NodeID          string `json:"nodeId"`
	SlashedAmount   uint64 `json:"slashedAmount"`
	RemainingBond   uint64 `json:"remainingBond"`
	TotalSlashCount int    `json:"totalSlashCount"`
	RemovedFromSet  bool   `json:"removedFromSet"`
	Message         string `json:"message"`
}

// CrossChainMPCRequest represents a cross-chain request for MPC operations
type CrossChainMPCRequest struct {
	Type          MPCRequestType `json:"type"`
	SessionID     string         `json:"sessionId"`
	Epoch         uint64         `json:"epoch"`
	OldPartyIDs   []party.ID     `json:"oldPartyIds"`
	NewPartyIDs   []party.ID     `json:"newPartyIds"`
	Threshold     int            `json:"threshold"`
	SourceChainID []byte         `json:"sourceChainId"`
	Timestamp     int64          `json:"timestamp"`
}

// MPCRequestType defines the type of MPC cross-chain request
type MPCRequestType uint8

const (
	MPCRequestReshare MPCRequestType = iota
	MPCRequestSign
	MPCRequestRefresh
)

// =========================================================================
// Bridge types
// =========================================================================

// BridgeRequest represents a cross-chain bridge request
type BridgeRequest struct {
	ID            ids.ID    `json:"id"`
	SourceChain   string    `json:"sourceChain"`
	DestChain     string    `json:"destChain"`
	Asset         ids.ID    `json:"asset"`
	Amount        uint64    `json:"amount"`
	Recipient     []byte    `json:"recipient"`
	SourceTxID    ids.ID    `json:"sourceTxId"`
	Confirmations uint32    `json:"confirmations"`
	Status        string    `json:"status"`
	MPCSignatures [][]byte  `json:"mpcSignatures"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ChainClient interface for interacting with different chains
type ChainClient interface {
	GetTransaction(ctx context.Context, txID ids.ID) (interface{}, error)
	GetConfirmations(ctx context.Context, txID ids.ID) (uint32, error)
	SendTransaction(ctx context.Context, tx interface{}) (ids.ID, error)
	ValidateAddress(address []byte) error
}

// BridgeRegistry tracks bridge operations
type BridgeRegistry struct {
	Validators       map[ids.NodeID]*BridgeValidator
	CompletedBridges map[ids.ID]*CompletedBridge
	DailyVolume      map[string]uint64
	mu               sync.RWMutex
}

// BridgeValidator represents a bridge validator node
type BridgeValidator struct {
	NodeID       ids.NodeID
	StakeAmount  uint64
	MPCPublicKey []byte
	Active       bool
	TotalBridged uint64
	SuccessRate  float64
}

// CompletedBridge represents a completed bridge operation
type CompletedBridge struct {
	RequestID    ids.ID
	SourceTxID   ids.ID
	DestTxID     ids.ID
	CompletedAt  time.Time
	MPCSignature []byte
}

// =========================================================================
// Relay types
// =========================================================================

const (
	// Message states
	MessagePending   = "pending"
	MessageVerified  = "verified"
	MessageDelivered = "delivered"
	MessageFailed    = "failed"
)

var (
	errUnknownMessage  = errors.New("unknown message")
	errUnknownChannel  = errors.New("unknown channel")
	errMessageTooLarge = errors.New("message too large")
	errChannelClosed   = errors.New("channel closed")
)

// Channel represents a cross-chain communication channel
type Channel struct {
	ID          ids.ID            `json:"id"`
	SourceChain ids.ID            `json:"sourceChain"`
	DestChain   ids.ID            `json:"destChain"`
	Ordering    string            `json:"ordering"`
	Version     string            `json:"version"`
	State       string            `json:"state"`
	CreatedAt   time.Time         `json:"createdAt"`
	Metadata    map[string]string `json:"metadata"`
}

// Message represents a cross-chain message
type Message struct {
	ID           ids.ID     `json:"id"`
	ChannelID    ids.ID     `json:"channelId"`
	SourceChain  ids.ID     `json:"sourceChain"`
	DestChain    ids.ID     `json:"destChain"`
	Sequence     uint64     `json:"sequence"`
	Payload      []byte     `json:"payload"`
	Proof        []byte     `json:"proof"`
	SourceHeight uint64     `json:"sourceHeight"`
	Sender       []byte     `json:"sender"`
	Receiver     []byte     `json:"receiver"`
	Timeout      int64      `json:"timeout"`
	State        string     `json:"state"`
	RelayedBy    ids.NodeID `json:"relayedBy,omitempty"`
	RelayedAt    int64      `json:"relayedAt,omitempty"`
	ConfirmedAt  int64      `json:"confirmedAt,omitempty"`
}

// MessageReceipt is generated when a message is verified
type MessageReceipt struct {
	MessageID   ids.ID `json:"messageId"`
	ChannelID   ids.ID `json:"channelId"`
	Success     bool   `json:"success"`
	ResultHash  []byte `json:"resultHash"`
	BlockHeight uint64 `json:"blockHeight"`
	Timestamp   int64  `json:"timestamp"`
}

// SignedReceipt represents a node's signed acknowledgment of message receipt
type SignedReceipt struct {
	MessageID   ids.ID     `json:"messageId"`
	SessionID   [32]byte   `json:"sessionId"`
	NodeID      ids.NodeID `json:"nodeId"`
	Timestamp   uint64     `json:"timestamp"`
	ContentHash [32]byte   `json:"contentHash"`
	Signature   []byte     `json:"signature"`
}

// ReceiptCommit represents a Merkle root commitment over a set of receipts
type ReceiptCommit struct {
	CommitID     [32]byte  `json:"commitId"`
	SessionID    [32]byte  `json:"sessionId,omitempty"`
	Root         [32]byte  `json:"root"`
	ReceiptCount uint32    `json:"receiptCount"`
	BlockHeight  uint64    `json:"blockHeight"`
	Window       struct {
		Start uint64 `json:"start"`
		End   uint64 `json:"end"`
	} `json:"window"`
	CommittedAt time.Time `json:"committedAt"`
}

// =========================================================================
// Oracle types
// =========================================================================

// Feed represents an oracle data feed
type Feed struct {
	ID          ids.ID            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Sources     []string          `json:"sources"`
	UpdateFreq  time.Duration     `json:"updateFreq"`
	PolicyHash  [32]byte          `json:"policyHash"`
	Operators   []ids.NodeID      `json:"operators"`
	CreatedAt   time.Time         `json:"createdAt"`
	Status      string            `json:"status"`
	Metadata    map[string]string `json:"metadata"`
}

// Observation represents a signed observation from an operator
type Observation struct {
	FeedID     ids.ID     `json:"feedId"`
	Value      []byte     `json:"value"`
	Timestamp  time.Time  `json:"timestamp"`
	SourceMeta [32]byte   `json:"sourceMetaHash"`
	OperatorID ids.NodeID `json:"operatorId"`
	Signature  []byte     `json:"signature"`
}

// AggregatedValue represents the canonical output for a feed
type AggregatedValue struct {
	FeedID       ids.ID    `json:"feedId"`
	Epoch        uint64    `json:"epoch"`
	Value        []byte    `json:"value"`
	Timestamp    time.Time `json:"timestamp"`
	Observations int       `json:"observationCount"`
	AggProof     []byte    `json:"aggProof,omitempty"`
	QuorumCert   []byte    `json:"quorumCert,omitempty"`
}

// OracleRequest represents a deterministic request from PlatformVM
type OracleRequest struct {
	RequestID      [32]byte      `json:"requestId"`
	ServiceID      ids.ID        `json:"serviceId"`
	SessionID      ids.ID        `json:"sessionId"`
	Step           uint32        `json:"step"`
	Retry          uint32        `json:"retry"`
	TxID           ids.ID        `json:"txId"`
	Kind           RequestKind   `json:"kind"`
	Target         []byte        `json:"target"`
	PayloadHash    [32]byte      `json:"payloadHash"`
	SchemaHash     [32]byte      `json:"schemaHash"`
	DeadlineHeight uint64        `json:"deadlineHeight"`
	Executors      []ids.NodeID  `json:"executors"`
	CreatedAt      time.Time     `json:"createdAt"`
	Status         RequestStatus `json:"status"`
}

// RequestKind indicates whether this is a write or read request
type RequestKind uint8

const (
	RequestKindWrite RequestKind = iota
	RequestKindRead
)

// RequestStatus tracks the lifecycle of an oracle request
type RequestStatus uint8

const (
	RequestStatusPending RequestStatus = iota
	RequestStatusExecuting
	RequestStatusCommitted
	RequestStatusExpired
	RequestStatusFailed
)

// OracleRecord represents a single execution record from an executor
type OracleRecord struct {
	RequestID   [32]byte   `json:"requestId"`
	Executor    ids.NodeID `json:"executor"`
	Timestamp   uint64     `json:"timestamp"`
	Endpoint    string     `json:"endpoint"`
	BodyHash    [32]byte   `json:"bodyHash"`
	ResultCode  uint32     `json:"resultCode"`
	ExternalRef []byte     `json:"externalRef"`
	Signature   []byte     `json:"signature"`
}

// OracleCommit represents a Merkle root commitment for a request
type OracleCommit struct {
	RequestID   [32]byte    `json:"requestId"`
	Kind        RequestKind `json:"kind"`
	Root        [32]byte    `json:"root"`
	RecordCount uint32      `json:"recordCount"`
	Window      struct {
		Start uint64 `json:"start"`
		End   uint64 `json:"end"`
	} `json:"window"`
	CommittedAt time.Time `json:"committedAt"`
}

// =========================================================================
// VM
// =========================================================================

// VM implements the unified Teleport Virtual Machine for cross-chain operations.
type VM struct {
	rt       *runtime.Runtime
	db       database.Database
	config   Config
	toEngine chan<- vmcore.Message
	log      log.Logger

	// MPC components (shared across bridge/relay/oracle)
	mpcKeyManager    *MPCKeyManager
	mpcCoordinator   *MPCCoordinator
	bridgeSigner     *BridgeSigner
	deliverySigner   *DeliveryConfirmationSigner
	messageValidator *BridgeMessageValidator

	// Legacy MPC fields (kept for CMP protocol integration)
	mpcConfig   *config.Config
	mpcPartyID  party.ID
	mpcPartyIDs []party.ID
	mpcPool     *pool.Pool

	// LP-333: Unified signer set
	signerSet *SignerSet

	// Bridge state
	pendingBridges map[ids.ID]*BridgeRequest
	bridgeRegistry *BridgeRegistry
	chainClients   map[string]ChainClient

	// Relay state
	channels        map[ids.ID]*Channel
	messages        map[ids.ID]*Message
	pendingMsgs     map[ids.ID][]*Message
	sequences       map[ids.ID]uint64
	sessionReceipts map[[32]byte][]*SignedReceipt
	receiptCommits  map[[32]byte]*ReceiptCommit
	nodePublicKeys  map[ids.NodeID]ed25519.PublicKey

	// Oracle state
	feeds          map[ids.ID]*Feed
	feedsByName    map[string]ids.ID
	pendingObs     map[ids.ID][]*Observation
	values         map[ids.ID]map[uint64]*AggregatedValue
	requests       map[[32]byte]*OracleRequest
	requestRecords map[[32]byte][]*OracleRecord
	commits        map[[32]byte]*OracleCommit

	// Block management
	preferred      ids.ID
	lastAcceptedID ids.ID
	lastAccepted   *Block
	pendingBlocks  map[ids.ID]*Block

	running bool
	mu      sync.RWMutex
}

// Genesis represents the genesis state
type Genesis struct {
	Timestamp int64      `json:"timestamp"`
	Message   string     `json:"message,omitempty"`
	Config    *Config    `json:"config,omitempty"`
	Channels  []*Channel `json:"channels,omitempty"`
	Feeds     []*Feed    `json:"initialFeeds,omitempty"`
}

// =========================================================================
// ChainVM Interface
// =========================================================================

// Initialize implements chain.ChainVM
func (vm *VM) Initialize(ctx context.Context, init vmcore.Init) error {
	vm.rt = init.Runtime
	vm.db = init.DB
	vm.toEngine = init.ToEngine

	if logger, ok := vm.rt.Log.(log.Logger); ok {
		vm.log = logger
	} else {
		return errors.New("invalid logger type")
	}

	// Initialize all maps
	vm.pendingBlocks = make(map[ids.ID]*Block)
	vm.pendingBridges = make(map[ids.ID]*BridgeRequest)
	vm.chainClients = make(map[string]ChainClient)
	vm.channels = make(map[ids.ID]*Channel)
	vm.messages = make(map[ids.ID]*Message)
	vm.pendingMsgs = make(map[ids.ID][]*Message)
	vm.sequences = make(map[ids.ID]uint64)
	vm.sessionReceipts = make(map[[32]byte][]*SignedReceipt)
	vm.receiptCommits = make(map[[32]byte]*ReceiptCommit)
	vm.nodePublicKeys = make(map[ids.NodeID]ed25519.PublicKey)
	vm.feeds = make(map[ids.ID]*Feed)
	vm.feedsByName = make(map[string]ids.ID)
	vm.pendingObs = make(map[ids.ID][]*Observation)
	vm.values = make(map[ids.ID]map[uint64]*AggregatedValue)
	vm.requests = make(map[[32]byte]*OracleRequest)
	vm.requestRecords = make(map[[32]byte][]*OracleRecord)
	vm.commits = make(map[[32]byte]*OracleCommit)

	// Parse config
	vm.config = DefaultConfig()
	if len(init.Config) > 0 {
		if err := json.Unmarshal(init.Config, &vm.config); err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}
	}

	// LP-333 defaults
	if vm.config.MaxSigners == 0 {
		vm.config.MaxSigners = 100
	}
	if vm.config.ThresholdRatio == 0 {
		vm.config.ThresholdRatio = 0.67
	}
	if vm.config.RequireValidatorBond == 0 {
		vm.config.RequireValidatorBond = 1_000_000 * 1e9 // 1M LUX
	}
	if vm.config.RequireValidatorBond < 1_000_000*1e9 {
		return errors.New("T-chain requires 1M LUX bond (slashable)")
	}

	// Relay defaults
	if vm.config.MaxMessageSize == 0 {
		vm.config.MaxMessageSize = 1024 * 1024
	}
	if vm.config.ConfirmationDepth == 0 {
		vm.config.ConfirmationDepth = 6
	}

	// Oracle defaults
	if vm.config.ObservationWindow == "" {
		vm.config.ObservationWindow = "1m"
	}
	if vm.config.AggregationMethod == "" {
		vm.config.AggregationMethod = "median"
	}

	// Initialize LP-333 signer set
	vm.signerSet = &SignerSet{
		Signers:      make([]*SignerInfo, 0, vm.config.MaxSigners),
		Waitlist:     make([]ids.NodeID, 0),
		CurrentEpoch: 0,
		SetFrozen:    false,
		ThresholdT:   0,
	}

	// Initialize MPC components
	vm.mpcPartyID = party.ID(vm.rt.NodeID.String())
	vm.mpcPool = pool.NewPool(8)

	keyManager, err := NewMPCKeyManager(vm.log)
	if err != nil {
		return fmt.Errorf("failed to create MPC key manager: %w", err)
	}
	vm.mpcKeyManager = keyManager
	vm.mpcCoordinator = NewMPCCoordinator(vm.mpcKeyManager, vm.log)
	vm.bridgeSigner = NewBridgeSigner(vm.mpcKeyManager, vm.mpcCoordinator, vm.log)
	vm.deliverySigner = NewDeliveryConfirmationSigner(vm.mpcKeyManager, vm.mpcCoordinator, vm.log)
	vm.messageValidator = NewBridgeMessageValidator(
		vm.bridgeSigner,
		vm.deliverySigner,
		vm.config.MinConfirmations,
		true,
		vm.log,
	)

	// Initialize bridge registry
	vm.bridgeRegistry = &BridgeRegistry{
		Validators:       make(map[ids.NodeID]*BridgeValidator),
		CompletedBridges: make(map[ids.ID]*CompletedBridge),
		DailyVolume:      make(map[string]uint64),
	}

	// Parse genesis
	genesis := &Genesis{}
	if len(init.Genesis) > 0 {
		if err := json.Unmarshal(init.Genesis, genesis); err != nil {
			return fmt.Errorf("failed to parse genesis: %w", err)
		}
	}

	// Initialize genesis channels
	for _, ch := range genesis.Channels {
		vm.channels[ch.ID] = ch
		vm.sequences[ch.ID] = 0
	}

	// Initialize genesis feeds
	for _, feed := range genesis.Feeds {
		vm.feeds[feed.ID] = feed
		vm.feedsByName[feed.Name] = feed.ID
		vm.values[feed.ID] = make(map[uint64]*AggregatedValue)
	}

	// Create genesis block
	genesisBlock := &Block{
		BlockHeight:    0,
		BlockTimestamp: genesis.Timestamp,
		ParentID_:     ids.Empty,
		vm:            vm,
	}
	if genesisBlock.BlockTimestamp == 0 {
		genesisBlock.BlockTimestamp = time.Now().Unix()
	}
	genesisBlock.ID_ = genesisBlock.computeID()
	vm.lastAcceptedID = genesisBlock.ID()
	vm.lastAccepted = genesisBlock

	vm.running = true

	vm.log.Info("TeleportVM initialized",
		log.Int("channels", len(vm.channels)),
		log.Int("feeds", len(vm.feeds)),
		log.Int("supportedChains", len(vm.config.SupportedChains)),
	)

	return vm.putBlock(genesisBlock)
}

// SetState implements chain.ChainVM
func (vm *VM) SetState(ctx context.Context, state uint32) error {
	return nil
}

// Shutdown implements chain.ChainVM
func (vm *VM) Shutdown(ctx context.Context) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.running = false
	vm.log.Info("TeleportVM shutting down")
	return nil
}

// Version implements chain.ChainVM
func (vm *VM) Version(ctx context.Context) (string, error) {
	return Version.String(), nil
}

// CreateHandlers implements chain.ChainVM
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return map[string]http.Handler{
		"/rpc": newRPCHandler(vm),
	}, nil
}

// NewHTTPHandler returns HTTP handlers for the VM
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

// HealthCheck implements chain.ChainVM
func (vm *VM) HealthCheck(ctx context.Context) (chain.HealthResult, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	return chain.HealthResult{
		Healthy: vm.running,
		Details: map[string]string{
			"channels":        fmt.Sprintf("%d", len(vm.channels)),
			"feeds":           fmt.Sprintf("%d", len(vm.feeds)),
			"pendingBridges":  fmt.Sprintf("%d", len(vm.pendingBridges)),
			"pendingMessages": fmt.Sprintf("%d", vm.countPendingMessages()),
		},
	}, nil
}

// Connected implements chain.ChainVM
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *chain.VersionInfo) error {
	return nil
}

// Disconnected implements chain.ChainVM
func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return nil
}

// WaitForEvent blocks until context is cancelled
func (vm *VM) WaitForEvent(ctx context.Context) (vmcore.Message, error) {
	<-ctx.Done()
	return vmcore.Message{}, ctx.Err()
}

// =========================================================================
// Block Management
// =========================================================================

// BuildBlock implements chain.ChainVM
func (vm *VM) BuildBlock(ctx context.Context) (chain.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	parent := vm.lastAccepted
	if parent == nil {
		return nil, errors.New("no parent block")
	}

	// Collect bridge requests
	var bridgeReqs []*BridgeRequest
	for _, req := range vm.pendingBridges {
		if req.Confirmations >= vm.config.MinConfirmations {
			bridgeReqs = append(bridgeReqs, req)
		}
		if len(bridgeReqs) >= 100 {
			break
		}
	}

	// Collect relay messages
	var relayMsgs []*Message
	for _, msgs := range vm.pendingMsgs {
		relayMsgs = append(relayMsgs, msgs...)
	}

	// Collect oracle observations
	var observations []*Observation
	for _, obs := range vm.pendingObs {
		observations = append(observations, obs...)
	}

	blk := &Block{
		ParentID_:      vm.lastAcceptedID,
		BlockHeight:    parent.BlockHeight + 1,
		BlockTimestamp: time.Now().Unix(),
		BridgeRequests: bridgeReqs,
		RelayMessages:  relayMsgs,
		Observations:   observations,
		vm:             vm,
	}
	blk.ID_ = blk.computeID()

	vm.pendingBlocks[blk.ID()] = blk
	return blk, nil
}

// ParseBlock implements chain.ChainVM
func (vm *VM) ParseBlock(ctx context.Context, bytes []byte) (chain.Block, error) {
	blk := &Block{vm: vm}
	if _, err := Codec.Unmarshal(bytes, blk); err != nil {
		// Fallback to JSON for relay/oracle data
		if jsonErr := json.Unmarshal(bytes, blk); jsonErr != nil {
			return nil, err
		}
	}
	blk.vm = vm
	blk.ID_ = blk.computeID()
	return blk, nil
}

// GetBlock implements chain.ChainVM
func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (chain.Block, error) {
	vm.mu.RLock()
	if vm.pendingBlocks != nil {
		if blk, exists := vm.pendingBlocks[id]; exists {
			vm.mu.RUnlock()
			return blk, nil
		}
	}
	vm.mu.RUnlock()

	if vm.lastAccepted != nil && vm.lastAccepted.ID() == id {
		return vm.lastAccepted, nil
	}

	return vm.getBlock(id)
}

// SetPreference implements chain.ChainVM
func (vm *VM) SetPreference(ctx context.Context, id ids.ID) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.preferred = id
	return nil
}

// LastAccepted implements chain.ChainVM
func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.lastAcceptedID, nil
}

// GetBlockIDAtHeight implements chain.HeightIndexedChainVM
func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return ids.Empty, errors.New("height index not implemented")
}

// =========================================================================
// Bridge Operations
// =========================================================================

// RegisterValidator registers a new validator as a teleport signer (LP-333)
func (vm *VM) RegisterValidator(input *RegisterValidatorInput) (*RegisterValidatorResult, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	nodeID, err := ids.NodeIDFromString(input.NodeID)
	if err != nil {
		return nil, fmt.Errorf("invalid node ID: %w", err)
	}

	// Check if already a signer
	for _, signer := range vm.signerSet.Signers {
		if signer.NodeID == nodeID {
			return &RegisterValidatorResult{
				Success:      false,
				NodeID:       input.NodeID,
				Message:      "already registered as signer",
				TotalSigners: len(vm.signerSet.Signers),
				Threshold:    vm.signerSet.ThresholdT,
				CurrentEpoch: vm.signerSet.CurrentEpoch,
				SetFrozen:    vm.signerSet.SetFrozen,
			}, nil
		}
	}

	// Check waitlist
	for _, wl := range vm.signerSet.Waitlist {
		if wl == nodeID {
			return &RegisterValidatorResult{
				Success:    false,
				NodeID:     input.NodeID,
				Message:    "already on waitlist",
				Waitlisted: true,
			}, nil
		}
	}

	var bondAmount uint64
	if input.BondAmount != "" {
		fmt.Sscanf(input.BondAmount, "%d", &bondAmount)
	}

	// If set is NOT frozen, add directly
	if !vm.signerSet.SetFrozen && len(vm.signerSet.Signers) < vm.config.MaxSigners {
		signerInfo := &SignerInfo{
			NodeID:     nodeID,
			PartyID:    party.ID(nodeID.String()),
			BondAmount: bondAmount,
			Active:     true,
			JoinedAt:   time.Now(),
			SlotIndex:  len(vm.signerSet.Signers),
			Slashed:    false,
			SlashCount: 0,
		}
		if input.MPCPubKey != "" {
			signerInfo.MPCPubKey = []byte(input.MPCPubKey)
		}

		vm.signerSet.Signers = append(vm.signerSet.Signers, signerInfo)

		vm.signerSet.ThresholdT = int(float64(len(vm.signerSet.Signers)) * vm.config.ThresholdRatio)
		if vm.signerSet.ThresholdT < 1 {
			vm.signerSet.ThresholdT = 1
		}

		if len(vm.signerSet.Signers) >= vm.config.MaxSigners {
			vm.signerSet.SetFrozen = true
		}

		remainingSlots := vm.config.MaxSigners - len(vm.signerSet.Signers)

		if vm.log != nil && !vm.log.IsZero() {
			vm.log.Info("validator registered as teleport signer (LP-333 opt-in)",
				log.Stringer("nodeID", nodeID),
				log.Int("signerIndex", signerInfo.SlotIndex),
				log.Int("totalSigners", len(vm.signerSet.Signers)),
				log.Int("threshold", vm.signerSet.ThresholdT),
			)
		}

		return &RegisterValidatorResult{
			Success:        true,
			NodeID:         input.NodeID,
			Registered:     true,
			Waitlisted:     false,
			SignerIndex:    signerInfo.SlotIndex,
			TotalSigners:   len(vm.signerSet.Signers),
			Threshold:      vm.signerSet.ThresholdT,
			ReshareNeeded:  false,
			CurrentEpoch:   vm.signerSet.CurrentEpoch,
			SetFrozen:      vm.signerSet.SetFrozen,
			RemainingSlots: remainingSlots,
			Message:        "registered as teleport signer",
		}, nil
	}

	// Set is frozen -- add to waitlist
	vm.signerSet.Waitlist = append(vm.signerSet.Waitlist, nodeID)
	waitlistIndex := len(vm.signerSet.Waitlist) - 1

	return &RegisterValidatorResult{
		Success:        true,
		NodeID:         input.NodeID,
		Registered:     false,
		Waitlisted:     true,
		WaitlistIndex:  waitlistIndex,
		TotalSigners:   len(vm.signerSet.Signers),
		Threshold:      vm.signerSet.ThresholdT,
		ReshareNeeded:  false,
		CurrentEpoch:   vm.signerSet.CurrentEpoch,
		SetFrozen:      vm.signerSet.SetFrozen,
		RemainingSlots: 0,
		Message:        "added to waitlist (signer set frozen at 100)",
	}, nil
}

// GetSignerSetInfo returns information about the current signer set
func (vm *VM) GetSignerSetInfo() *SignerSetInfo {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	remainingSlots := vm.config.MaxSigners - len(vm.signerSet.Signers)
	if remainingSlots < 0 {
		remainingSlots = 0
	}

	info := &SignerSetInfo{
		TotalSigners:   len(vm.signerSet.Signers),
		Threshold:      vm.signerSet.ThresholdT,
		MaxSigners:     vm.config.MaxSigners,
		CurrentEpoch:   vm.signerSet.CurrentEpoch,
		SetFrozen:      vm.signerSet.SetFrozen,
		RemainingSlots: remainingSlots,
		WaitlistSize:   len(vm.signerSet.Waitlist),
		Signers:        vm.signerSet.Signers,
	}

	if len(vm.signerSet.PublicKey) > 0 {
		info.PublicKey = fmt.Sprintf("%x", vm.signerSet.PublicKey)
	}

	return info
}

// RemoveSigner removes a failed signer and triggers reshare (LP-333)
func (vm *VM) RemoveSigner(nodeID ids.NodeID, replacementNodeID *ids.NodeID) (*SignerReplacementResult, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	found := false
	var removedSigner *SignerInfo
	for i, signer := range vm.signerSet.Signers {
		if signer.NodeID == nodeID {
			removedSigner = signer
			vm.signerSet.Signers = append(vm.signerSet.Signers[:i], vm.signerSet.Signers[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return &SignerReplacementResult{
			Success: false,
			Message: fmt.Sprintf("signer %s not found in active set", nodeID),
		}, nil
	}

	var replacement ids.NodeID
	var replacementSource string
	if replacementNodeID != nil && *replacementNodeID != ids.EmptyNodeID {
		replacement = *replacementNodeID
		replacementSource = "explicit"
	} else if len(vm.signerSet.Waitlist) > 0 {
		replacement = vm.signerSet.Waitlist[0]
		vm.signerSet.Waitlist = vm.signerSet.Waitlist[1:]
		replacementSource = "waitlist"
	}

	if replacement != ids.EmptyNodeID {
		newSigner := &SignerInfo{
			NodeID:     replacement,
			PartyID:    party.ID(replacement.String()),
			BondAmount: 0,
			Active:     true,
			JoinedAt:   time.Now(),
			SlotIndex:  removedSigner.SlotIndex,
			Slashed:    false,
			SlashCount: 0,
		}
		vm.signerSet.Signers = append(vm.signerSet.Signers, newSigner)
	}

	vm.signerSet.ThresholdT = int(float64(len(vm.signerSet.Signers)) * vm.config.ThresholdRatio)
	if vm.signerSet.ThresholdT < 1 && len(vm.signerSet.Signers) > 0 {
		vm.signerSet.ThresholdT = 1
	}

	// INCREMENT EPOCH - only reshare trigger (LP-333)
	vm.signerSet.CurrentEpoch++

	reshareSession := fmt.Sprintf("reshare-epoch-%d-%s", vm.signerSet.CurrentEpoch, time.Now().Format("20060102150405"))

	if vm.log != nil && !vm.log.IsZero() {
		vm.log.Info("signer removed and reshare triggered (LP-333)",
			log.Stringer("removedNodeID", nodeID),
			log.Stringer("replacementNodeID", replacement),
			log.Uint64("newEpoch", vm.signerSet.CurrentEpoch),
		)
	}

	if err := vm.triggerReshareProtocol(reshareSession, nodeID, replacement); err != nil {
		if vm.log != nil && !vm.log.IsZero() {
			vm.log.Warn("failed to trigger reshare protocol",
				log.String("reshareSession", reshareSession),
				log.String("error", err.Error()),
			)
		}
	}

	result := &SignerReplacementResult{
		Success:       true,
		RemovedNodeID: nodeID.String(),
		NewEpoch:      vm.signerSet.CurrentEpoch,
		ActiveSigners: len(vm.signerSet.Signers),
		Threshold:     vm.signerSet.ThresholdT,
		Message:       "signer removed, reshare initiated",
	}

	if replacement != ids.EmptyNodeID {
		result.ReplacementNodeID = replacement.String()
		result.ReshareSession = reshareSession
		result.Message = fmt.Sprintf("signer replaced from %s, reshare initiated", replacementSource)
	}

	return result, nil
}

// HasSigner checks if a node ID is in the active signer set
func (vm *VM) HasSigner(nodeID ids.NodeID) bool {
	for _, signer := range vm.signerSet.Signers {
		if signer.NodeID == nodeID {
			return true
		}
	}
	return false
}

// SlashSigner slashes a misbehaving signer's bond
func (vm *VM) SlashSigner(input *SlashSignerInput) (*SlashSignerResult, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if input.SlashPercent < 1 || input.SlashPercent > 100 {
		return nil, errors.New("slash percent must be between 1 and 100")
	}

	var signer *SignerInfo
	var signerIndex int
	for i, s := range vm.signerSet.Signers {
		if s.NodeID == input.NodeID {
			signer = s
			signerIndex = i
			break
		}
	}

	if signer == nil {
		return &SlashSignerResult{
			Success: false,
			NodeID:  input.NodeID.String(),
			Message: "signer not found in active set",
		}, nil
	}

	slashAmount := (signer.BondAmount * uint64(input.SlashPercent)) / 100
	remainingBond := signer.BondAmount - slashAmount

	signer.BondAmount = remainingBond
	signer.Slashed = true
	signer.SlashCount++

	result := &SlashSignerResult{
		Success:         true,
		NodeID:          input.NodeID.String(),
		SlashedAmount:   slashAmount,
		RemainingBond:   remainingBond,
		TotalSlashCount: signer.SlashCount,
		RemovedFromSet:  false,
		Message:         fmt.Sprintf("slashed %d%% of bond (%d LUX)", input.SlashPercent, slashAmount/1e9),
	}

	// If bond drops below 1M LUX, remove
	minBond := uint64(1_000_000 * 1e9)
	if remainingBond < minBond {
		vm.signerSet.Signers = append(vm.signerSet.Signers[:signerIndex], vm.signerSet.Signers[signerIndex+1:]...)

		vm.signerSet.ThresholdT = int(float64(len(vm.signerSet.Signers)) * vm.config.ThresholdRatio)
		if vm.signerSet.ThresholdT < 1 && len(vm.signerSet.Signers) > 0 {
			vm.signerSet.ThresholdT = 1
		}

		vm.signerSet.CurrentEpoch++

		result.RemovedFromSet = true
		result.Message = fmt.Sprintf("slashed %d%% of bond, signer removed (bond below 1M LUX minimum)", input.SlashPercent)
	}

	return result, nil
}

// triggerReshareProtocol sends a cross-chain request to ThresholdVM
func (vm *VM) triggerReshareProtocol(sessionID string, removedNodeID ids.NodeID, newNodeID ids.NodeID) error {
	if vm.rt == nil {
		return nil
	}
	if vm.rt.WarpSigner == nil || vm.rt.Sender == nil {
		return nil
	}

	oldPartyIDs := make([]party.ID, 0, len(vm.signerSet.Signers))
	for _, signer := range vm.signerSet.Signers {
		if signer.NodeID != removedNodeID && signer.NodeID != newNodeID {
			oldPartyIDs = append(oldPartyIDs, signer.PartyID)
		}
	}

	newPartyIDs := make([]party.ID, 0, len(vm.signerSet.Signers))
	for _, signer := range vm.signerSet.Signers {
		newPartyIDs = append(newPartyIDs, signer.PartyID)
	}

	mpcRequest := &CrossChainMPCRequest{
		Type:          MPCRequestReshare,
		SessionID:     sessionID,
		Epoch:         vm.signerSet.CurrentEpoch,
		OldPartyIDs:   oldPartyIDs,
		NewPartyIDs:   newPartyIDs,
		Threshold:     vm.signerSet.ThresholdT,
		SourceChainID: vm.rt.ChainID[:],
		Timestamp:     time.Now().Unix(),
	}

	requestBytes, err := json.Marshal(mpcRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal MPC request: %w", err)
	}

	unsignedMsg, err := warp.NewUnsignedMessage(vm.rt.NetworkID, vm.rt.ChainID, requestBytes)
	if err != nil {
		return fmt.Errorf("failed to create unsigned warp message: %w", err)
	}

	sigBytes, err := vm.rt.WarpSigner.Sign(unsignedMsg)
	if err != nil {
		return fmt.Errorf("failed to sign warp message: %w", err)
	}

	var sigArray [96]byte
	copy(sigArray[:], sigBytes)

	signers := warp.NewBitSet()
	signers.Add(0)

	signature := warp.NewBitSetSignature(signers, sigArray)

	signedMsg, err := warp.NewMessage(unsignedMsg, signature)
	if err != nil {
		return fmt.Errorf("failed to create signed warp message: %w", err)
	}

	msgBytes := signedMsg.Bytes()
	sendConfig := warp.SendConfig{
		Validators: len(vm.signerSet.Signers),
		Peers:      0,
	}

	return vm.rt.Sender.SendGossip(context.Background(), sendConfig, msgBytes)
}

// InitializeMPCKeys performs threshold key generation
func (vm *VM) InitializeMPCKeys(ctx context.Context) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	numSigners := len(vm.signerSet.Signers)
	if numSigners == 0 {
		return fmt.Errorf("no signers in set")
	}

	threshold := vm.signerSet.ThresholdT
	if threshold == 0 {
		return fmt.Errorf("threshold not set")
	}

	if err := vm.mpcKeyManager.GenerateKeys(ctx, threshold, numSigners); err != nil {
		return fmt.Errorf("failed to generate keys: %w", err)
	}

	vm.signerSet.PublicKey = vm.mpcKeyManager.GetGroupPublicKey()
	return nil
}

// GetMPCStatus returns the current MPC status
func (vm *VM) GetMPCStatus() map[string]interface{} {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	groupKey := vm.mpcKeyManager.GetGroupPublicKey()

	status := map[string]interface{}{
		"initialized":  len(groupKey) > 0,
		"groupKeyLen":  len(groupKey),
		"numSigners":   len(vm.signerSet.Signers),
		"threshold":    vm.signerSet.ThresholdT,
		"currentEpoch": vm.signerSet.CurrentEpoch,
		"setFrozen":    vm.signerSet.SetFrozen,
	}

	if vm.mpcKeyManager.keyShare != nil {
		status["hasKeyShare"] = true
		status["keyShareIndex"] = vm.mpcKeyManager.keyShare.Index()
	} else {
		status["hasKeyShare"] = false
	}

	return status
}

// =========================================================================
// Relay Operations
// =========================================================================

// OpenChannel opens a new cross-chain channel
func (vm *VM) OpenChannel(sourceChain, destChain ids.ID, ordering, version string) (*Channel, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	h := sha256.New()
	h.Write(sourceChain[:])
	h.Write(destChain[:])
	binary.Write(h, binary.BigEndian, time.Now().UnixNano())
	channelID := ids.ID(h.Sum(nil))

	channel := &Channel{
		ID:          channelID,
		SourceChain: sourceChain,
		DestChain:   destChain,
		Ordering:    ordering,
		Version:     version,
		State:       "open",
		CreatedAt:   time.Now(),
		Metadata:    make(map[string]string),
	}

	vm.channels[channelID] = channel
	vm.sequences[channelID] = 0

	channelBytes, _ := json.Marshal(channel)
	key := append(channelPrefix, channelID[:]...)
	if err := vm.db.Put(key, channelBytes); err != nil {
		return nil, err
	}

	return channel, nil
}

// GetChannel returns a channel by ID
func (vm *VM) GetChannel(channelID ids.ID) (*Channel, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	channel, ok := vm.channels[channelID]
	if !ok {
		return nil, errUnknownChannel
	}
	return channel, nil
}

// CloseChannel closes a channel
func (vm *VM) CloseChannel(channelID ids.ID) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	channel, ok := vm.channels[channelID]
	if !ok {
		return errUnknownChannel
	}

	channel.State = "closed"

	channelBytes, _ := json.Marshal(channel)
	key := append(channelPrefix, channelID[:]...)
	return vm.db.Put(key, channelBytes)
}

// SendMessage queues a message for relay
func (vm *VM) SendMessage(channelID ids.ID, payload, sender, receiver []byte, timeout int64) (*Message, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	channel, ok := vm.channels[channelID]
	if !ok {
		return nil, errUnknownChannel
	}
	if channel.State != "open" {
		return nil, errChannelClosed
	}
	if len(payload) > vm.config.MaxMessageSize {
		return nil, errMessageTooLarge
	}

	seq := vm.sequences[channelID]
	vm.sequences[channelID] = seq + 1

	h := sha256.New()
	h.Write(channelID[:])
	binary.Write(h, binary.BigEndian, seq)
	h.Write(payload)
	msgID := ids.ID(h.Sum(nil))

	msg := &Message{
		ID:          msgID,
		ChannelID:   channelID,
		SourceChain: channel.SourceChain,
		DestChain:   channel.DestChain,
		Sequence:    seq,
		Payload:     payload,
		Sender:      sender,
		Receiver:    receiver,
		Timeout:     timeout,
		State:       MessagePending,
	}

	vm.messages[msgID] = msg
	vm.pendingMsgs[channel.DestChain] = append(vm.pendingMsgs[channel.DestChain], msg)

	return msg, nil
}

// ReceiveMessage processes an incoming message with proof
func (vm *VM) ReceiveMessage(msgID ids.ID, proof []byte, sourceHeight uint64) (*MessageReceipt, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	msg, ok := vm.messages[msgID]
	if !ok {
		return nil, errUnknownMessage
	}

	msg.Proof = proof
	msg.SourceHeight = sourceHeight

	msg.State = MessageVerified
	msg.ConfirmedAt = time.Now().Unix()

	receipt := &MessageReceipt{
		MessageID:   msgID,
		ChannelID:   msg.ChannelID,
		Success:     true,
		ResultHash:  sha256Hash(msg.Payload),
		BlockHeight: vm.lastAccepted.BlockHeight,
		Timestamp:   time.Now().Unix(),
	}

	return receipt, nil
}

// GetMessage returns a message by ID
func (vm *VM) GetMessage(msgID ids.ID) (*Message, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	msg, ok := vm.messages[msgID]
	if !ok {
		return nil, errUnknownMessage
	}
	return msg, nil
}

// CreateVerifiedMessage creates a VerifiedMessage artifact
func (vm *VM) CreateVerifiedMessage(msg *Message) (*artifacts.VerifiedMessage, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	if msg.State != MessageVerified && msg.State != MessageDelivered {
		return nil, errors.New("message not yet verified")
	}

	return &artifacts.VerifiedMessage{
		SrcDomain:        msg.SourceChain,
		DstDomain:        msg.DestChain,
		Nonce:            msg.Sequence,
		Payload:          msg.Payload,
		SrcFinalityProof: msg.Proof,
		Mode:             artifacts.LCMode,
	}, nil
}

// RegisterNodePublicKey registers a node's Ed25519 public key
func (vm *VM) RegisterNodePublicKey(nodeID ids.NodeID, publicKey ed25519.PublicKey) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("invalid public key size")
	}

	vm.nodePublicKeys[nodeID] = publicKey
	return nil
}

// SubmitSignedReceipt records a signed receipt from a node
func (vm *VM) SubmitSignedReceipt(receipt *SignedReceipt) error {
	if receipt == nil {
		return errors.New("nil receipt")
	}
	if receipt.MessageID == ids.Empty {
		return errors.New("receipt missing message ID")
	}
	if receipt.NodeID == ids.EmptyNodeID {
		return errors.New("receipt missing node ID")
	}
	if len(receipt.Signature) == 0 {
		return errors.New("receipt missing signature")
	}

	if err := vm.verifyReceiptSignature(receipt); err != nil {
		return fmt.Errorf("receipt signature verification failed: %w", err)
	}

	vm.mu.Lock()
	defer vm.mu.Unlock()

	sessionID := receipt.SessionID
	vm.sessionReceipts[sessionID] = append(vm.sessionReceipts[sessionID], receipt)
	return nil
}

// CommitSessionReceipts creates a Merkle root commitment for receipts in a session
func (vm *VM) CommitSessionReceipts(sessionID [32]byte) (*ReceiptCommit, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	receipts, ok := vm.sessionReceipts[sessionID]
	if !ok || len(receipts) == 0 {
		return nil, errors.New("no receipts found for session")
	}

	root := computeReceiptsMerkleRoot(receipts)

	var minTime, maxTime uint64 = ^uint64(0), 0
	for _, r := range receipts {
		if r.Timestamp < minTime {
			minTime = r.Timestamp
		}
		if r.Timestamp > maxTime {
			maxTime = r.Timestamp
		}
	}

	h := sha256.New()
	h.Write([]byte("LUX:ReceiptCommit:v1"))
	h.Write(sessionID[:])
	h.Write(root[:])
	var commitID [32]byte
	copy(commitID[:], h.Sum(nil))

	commit := &ReceiptCommit{
		CommitID:     commitID,
		SessionID:    sessionID,
		Root:         root,
		ReceiptCount: uint32(len(receipts)),
		BlockHeight:  vm.lastAccepted.BlockHeight,
		CommittedAt:  time.Now(),
	}
	commit.Window.Start = minTime
	commit.Window.End = maxTime

	vm.receiptCommits[sessionID] = commit
	return commit, nil
}

// =========================================================================
// Oracle Operations
// =========================================================================

// RegisterFeed registers a new oracle feed
func (vm *VM) RegisterFeed(feed *Feed) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.running {
		return errors.New("vm not initialized")
	}

	if _, exists := vm.feedsByName[feed.Name]; exists {
		return fmt.Errorf("feed %s already exists", feed.Name)
	}

	feed.CreatedAt = time.Now()
	feed.Status = "active"
	vm.feeds[feed.ID] = feed
	vm.feedsByName[feed.Name] = feed.ID
	vm.values[feed.ID] = make(map[uint64]*AggregatedValue)
	return nil
}

// SubmitObservation submits an observation for a feed
func (vm *VM) SubmitObservation(obs *Observation) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.running {
		return errors.New("vm not initialized")
	}

	feed, exists := vm.feeds[obs.FeedID]
	if !exists {
		return errors.New("feed not found")
	}

	window, _ := time.ParseDuration(vm.config.ObservationWindow)
	if time.Since(obs.Timestamp) > window {
		return errors.New("stale observation")
	}

	authorized := false
	for _, op := range feed.Operators {
		if op == obs.OperatorID {
			authorized = true
			break
		}
	}
	if !authorized {
		return fmt.Errorf("operator %s not authorized for feed %s", obs.OperatorID, feed.Name)
	}

	vm.pendingObs[obs.FeedID] = append(vm.pendingObs[obs.FeedID], obs)
	return nil
}

// GetFeed returns a feed by ID
func (vm *VM) GetFeed(feedID ids.ID) (*Feed, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	feed, exists := vm.feeds[feedID]
	if !exists {
		return nil, errors.New("feed not found")
	}
	return feed, nil
}

// GetLatestValue returns the latest aggregated value for a feed
func (vm *VM) GetLatestValue(feedID ids.ID) (*AggregatedValue, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	epochs, exists := vm.values[feedID]
	if !exists || len(epochs) == 0 {
		return nil, errors.New("feed not found")
	}

	var latest *AggregatedValue
	var latestEpoch uint64
	for epoch, val := range epochs {
		if epoch > latestEpoch {
			latestEpoch = epoch
			latest = val
		}
	}
	return latest, nil
}

// CreateAttestation creates an OracleAttestation artifact
func (vm *VM) CreateAttestation(feedID ids.ID, epoch uint64) (*artifacts.OracleAttestation, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	epochs, exists := vm.values[feedID]
	if !exists {
		return nil, errors.New("feed not found")
	}

	val, exists := epochs[epoch]
	if !exists {
		return nil, fmt.Errorf("no value for epoch %d", epoch)
	}

	feed := vm.feeds[feedID]

	return &artifacts.OracleAttestation{
		Version_:   1,
		SigSuite_:  artifacts.SuiteHybrid,
		DomainID_:  vm.rt.ChainID,
		FeedID:     feedID,
		Epoch:      epoch,
		Value:      val.Value,
		AggProof:   val.AggProof,
		QuorumCert: val.QuorumCert,
		ValidFrom:  val.Timestamp,
		ValidTo:    val.Timestamp.Add(feed.UpdateFreq * 2),
		PolicyHash: feed.PolicyHash,
	}, nil
}

// RegisterRequest registers a new oracle request
func (vm *VM) RegisterRequest(req *OracleRequest) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.running {
		return errors.New("vm not initialized")
	}

	expectedID := ComputeRequestID(req.ServiceID, req.SessionID, req.TxID, req.Step, req.Retry)
	if expectedID != req.RequestID {
		return fmt.Errorf("invalid request_id: expected %x, got %x", expectedID, req.RequestID)
	}

	if _, exists := vm.requests[req.RequestID]; exists {
		return fmt.Errorf("request %x already exists", req.RequestID)
	}

	req.CreatedAt = time.Now()
	req.Status = RequestStatusPending
	vm.requests[req.RequestID] = req
	vm.requestRecords[req.RequestID] = make([]*OracleRecord, 0)
	return nil
}

// SubmitRecord submits an execution record from an assigned executor
func (vm *VM) SubmitRecord(record *OracleRecord) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.running {
		return errors.New("vm not initialized")
	}

	req, exists := vm.requests[record.RequestID]
	if !exists {
		return fmt.Errorf("request %x not found", record.RequestID)
	}

	authorized := false
	for _, ex := range req.Executors {
		if ex == record.Executor {
			authorized = true
			break
		}
	}
	if !authorized {
		return fmt.Errorf("executor %s not authorized for request %x", record.Executor, record.RequestID)
	}

	if vm.lastAccepted != nil && vm.lastAccepted.BlockHeight > req.DeadlineHeight {
		req.Status = RequestStatusExpired
		return fmt.Errorf("request %x has expired", record.RequestID)
	}

	if req.Status == RequestStatusPending {
		req.Status = RequestStatusExecuting
	}

	vm.requestRecords[record.RequestID] = append(vm.requestRecords[record.RequestID], record)
	return nil
}

// ComputeRequestID computes the deterministic request ID
func ComputeRequestID(serviceID, sessionID, txID ids.ID, step, retry uint32) [32]byte {
	h := sha256.New()
	h.Write([]byte("LUX:OracleRequest:v1"))
	h.Write(serviceID[:])
	h.Write(sessionID[:])
	var buf [4]byte
	buf[0] = byte(step >> 24)
	buf[1] = byte(step >> 16)
	buf[2] = byte(step >> 8)
	buf[3] = byte(step)
	h.Write(buf[:])
	buf[0] = byte(retry >> 24)
	buf[1] = byte(retry >> 16)
	buf[2] = byte(retry >> 8)
	buf[3] = byte(retry)
	h.Write(buf[:])
	h.Write(txID[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// =========================================================================
// Internal helpers
// =========================================================================

func (vm *VM) putBlock(blk *Block) error {
	bytes, err := Codec.Marshal(codecVersion, blk)
	if err != nil {
		// Fallback to JSON
		bytes, err = json.Marshal(blk)
		if err != nil {
			return err
		}
	}
	id := blk.ID()
	return vm.db.Put(id[:], bytes)
}

func (vm *VM) getBlock(id ids.ID) (*Block, error) {
	bytes, err := vm.db.Get(id[:])
	if err != nil {
		return nil, err
	}

	blk := &Block{vm: vm}
	if _, codecErr := Codec.Unmarshal(bytes, blk); codecErr != nil {
		if jsonErr := json.Unmarshal(bytes, blk); jsonErr != nil {
			return nil, codecErr
		}
	}

	blk.vm = vm
	blk.ID_ = id
	return blk, nil
}

func (vm *VM) countPendingMessages() int {
	count := 0
	for _, msgs := range vm.pendingMsgs {
		count += len(msgs)
	}
	return count
}

func (vm *VM) verifyReceiptSignature(receipt *SignedReceipt) error {
	vm.mu.RLock()
	publicKey, exists := vm.nodePublicKeys[receipt.NodeID]
	vm.mu.RUnlock()

	if !exists {
		return nil // Accept without verification if key not registered
	}

	h := sha256.New()
	h.Write(receipt.MessageID[:])
	h.Write(receipt.SessionID[:])
	h.Write(receipt.NodeID[:])
	timestampBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBytes, receipt.Timestamp)
	h.Write(timestampBytes)
	h.Write(receipt.ContentHash[:])
	message := h.Sum(nil)

	if !ed25519.Verify(publicKey, message, receipt.Signature) {
		return errors.New("invalid signature")
	}
	return nil
}

func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func computeReceiptsMerkleRoot(receipts []*SignedReceipt) [32]byte {
	if len(receipts) == 0 {
		return [32]byte{}
	}

	leaves := make([][32]byte, len(receipts))
	for i, r := range receipts {
		h := sha256.New()
		h.Write(r.MessageID[:])
		h.Write(r.NodeID[:])
		h.Write(r.ContentHash[:])
		h.Write(r.Signature)
		ts := make([]byte, 8)
		binary.BigEndian.PutUint64(ts, r.Timestamp)
		h.Write(ts)
		copy(leaves[i][:], h.Sum(nil))
	}

	return buildMerkleRoot(leaves)
}

func buildMerkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	for len(leaves)&(len(leaves)-1) != 0 {
		leaves = append(leaves, leaves[len(leaves)-1])
	}

	for len(leaves) > 1 {
		var nextLevel [][32]byte
		for i := 0; i < len(leaves); i += 2 {
			h := sha256.New()
			h.Write(leaves[i][:])
			h.Write(leaves[i+1][:])
			var parent [32]byte
			copy(parent[:], h.Sum(nil))
			nextLevel = append(nextLevel, parent)
		}
		leaves = nextLevel
	}

	return leaves[0]
}
