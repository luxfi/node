// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package bvm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/engine/core/common"
	consensusctx "github.com/luxfi/consensus/context"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/version"
	"github.com/luxfi/threshold/pkg/party"
	"github.com/luxfi/threshold/pkg/pool"
	"github.com/luxfi/threshold/protocols/cmp/config"
)

var (
	_ block.ChainVM = (*VM)(nil)

	Version = &version.Semantic{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	errNotImplemented = errors.New("not implemented")
)

// Silence unused variable warning
var _ = errNotImplemented

// BridgeConfig contains VM configuration
type BridgeConfig struct {
	// MPC configuration for secure cross-chain operations
	MPCThreshold    int `json:"mpcThreshold"`    // t: Threshold (t+1 parties needed)
	MPCTotalParties int `json:"mpcTotalParties"` // n: Total number of MPC nodes

	// Bridge parameters
	MinConfirmations uint32 `json:"minConfirmations"` // Confirmations required before bridging
	BridgeFee        uint64 `json:"bridgeFee"`        // Fee in LUX for bridge operations

	// Supported chains
	SupportedChains []string `json:"supportedChains"` // Chain IDs that can be bridged

	// Security settings
	MaxBridgeAmount       uint64 `json:"maxBridgeAmount"`       // Maximum amount per bridge transaction
	DailyBridgeLimit      uint64 `json:"dailyBridgeLimit"`      // Daily limit for bridge operations
	RequireValidatorStake uint64 `json:"requireValidatorStake"` // 100M LUX required
}

// VM implements the Bridge VM for cross-chain interoperability
type VM struct {
	ctx      *consensusctx.Context
	db       database.Database
	config   BridgeConfig
	toEngine chan<- common.Message
	log      log.Logger

	// MPC components using threshold CMP protocol
	mpcConfig   *config.Config // CMP config for this party (after keygen)
	mpcPartyID  party.ID       // This party's ID in MPC protocol
	mpcPartyIDs []party.ID     // All party IDs in the MPC group
	mpcPool     *pool.Pool     // Worker pool for MPC operations

	// Bridge state
	pendingBridges map[ids.ID]*BridgeRequest
	bridgeRegistry *BridgeRegistry

	// Chain connectivity
	chainClients map[string]ChainClient

	// Block management
	preferred      ids.ID
	lastAcceptedID ids.ID
	pendingBlocks  map[ids.ID]*Block

	mu sync.RWMutex
}

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
	Status        string    `json:"status"` // pending, signing, completed, failed
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

// BridgeRegistry tracks bridge operations and validators
type BridgeRegistry struct {
	Validators       map[ids.NodeID]*BridgeValidator
	CompletedBridges map[ids.ID]*CompletedBridge
	DailyVolume      map[string]uint64 // chainID -> volume
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

// Initialize implements the block.ChainVM interface
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx interface{},
	db interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	msgChan interface{},
	fxs []interface{},
	appSender interface{},
) error {
	// Type assertions
	var ok bool
	vm.ctx, ok = chainCtx.(*consensusctx.Context)
	if !ok {
		return errors.New("invalid chain context type")
	}

	vm.db, ok = db.(database.Database)
	if !ok {
		return errors.New("invalid database type")
	}

	vm.toEngine, ok = msgChan.(chan<- common.Message)
	if !ok {
		return errors.New("invalid message channel type")
	}

	if logger, ok := vm.ctx.Log.(log.Logger); ok {
		vm.log = logger
	} else {
		return errors.New("invalid logger type")
	}

	vm.pendingBlocks = make(map[ids.ID]*Block)
	vm.pendingBridges = make(map[ids.ID]*BridgeRequest)
	vm.chainClients = make(map[string]ChainClient)

	// Parse configuration
	if _, err := Codec.Unmarshal(configBytes, &vm.config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate configuration
	if vm.config.RequireValidatorStake < 100_000_000*1e9 { // 100M LUX
		return errors.New("B-chain requires 100M LUX minimum stake")
	}

	// Initialize MPC components using threshold protocol
	// Party ID is derived from node ID
	vm.mpcPartyID = party.ID(vm.ctx.NodeID.String())

	// Create worker pool for MPC operations (8 workers)
	vm.mpcPool = pool.NewPool(8)

	// Note: mpcConfig and mpcPartyIDs will be populated during keygen
	// which happens when validators join the bridge network

	// Initialize bridge registry
	vm.bridgeRegistry = &BridgeRegistry{
		Validators:       make(map[ids.NodeID]*BridgeValidator),
		CompletedBridges: make(map[ids.ID]*CompletedBridge),
		DailyVolume:      make(map[string]uint64),
	}

	// Initialize chain clients for supported chains
	for _, chainID := range vm.config.SupportedChains {
		// Initialize appropriate client based on chain type
		// This would be implemented based on specific chain requirements
		vm.log.Info("initializing chain client",
			log.String("chainID", chainID),
		)
	}

	// Parse genesis
	genesis := &Genesis{}
	if _, err := Codec.Unmarshal(genesisBytes, genesis); err != nil {
		return fmt.Errorf("failed to parse genesis: %w", err)
	}

	// Create genesis block
	genesisBlock := &Block{
		BlockHeight:    0,
		BlockTimestamp: genesis.Timestamp,
		ParentID_:      ids.Empty,
		BridgeRequests: []*BridgeRequest{},
		vm:             vm,
	}

	genesisBlock.ID_ = genesisBlock.computeID()
	vm.lastAcceptedID = genesisBlock.ID()

	return vm.putBlock(genesisBlock)
}

// BuildBlock implements the block.ChainVM interface
func (vm *VM) BuildBlock(ctx context.Context) (block.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Check if we have pending bridge requests
	if len(vm.pendingBridges) == 0 {
		return nil, errors.New("no pending bridge requests")
	}

	// Get parent block
	parentID := vm.preferred
	if parentID == ids.Empty {
		parentID = vm.lastAcceptedID
	}

	parent, err := vm.getBlock(parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent block: %w", err)
	}

	// Collect bridge requests that are ready
	var requests []*BridgeRequest
	for _, req := range vm.pendingBridges {
		// Check if request has enough confirmations
		if req.Confirmations >= vm.config.MinConfirmations {
			requests = append(requests, req)
		}

		// Limit block size
		if len(requests) >= 100 {
			break
		}
	}

	if len(requests) == 0 {
		return nil, errors.New("no ready bridge requests")
	}

	// Create new block
	blk := &Block{
		ParentID_:      parentID,
		BlockHeight:    parent.Height() + 1,
		BlockTimestamp: time.Now().Unix(),
		BridgeRequests: requests,
		vm:             vm,
	}

	blk.ID_ = blk.computeID()

	// Store pending block
	vm.pendingBlocks[blk.ID()] = blk

	vm.log.Info("built bridge block",
		log.Stringer("blockID", blk.ID()),
		log.Int("numRequests", len(requests)),
	)

	return blk, nil
}

// GetBlock implements the block.ChainVM interface
func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (block.Block, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	// Check pending blocks first
	if blk, exists := vm.pendingBlocks[id]; exists {
		return blk, nil
	}

	return vm.getBlock(id)
}

// ParseBlock implements the block.ChainVM interface
func (vm *VM) ParseBlock(ctx context.Context, bytes []byte) (block.Block, error) {
	blk := &Block{vm: vm}
	if _, err := Codec.Unmarshal(bytes, blk); err != nil {
		return nil, err
	}

	blk.ID_ = blk.computeID()
	return blk, nil
}

// SetPreference implements the chain.ChainVM interface
func (vm *VM) SetPreference(ctx context.Context, id ids.ID) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	vm.preferred = id
	return nil
}

// LastAccepted implements the chain.ChainVM interface
func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	return vm.lastAcceptedID, nil
}

// CreateHandlers implements the common.VM interface
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	handlers := map[string]http.Handler{
		"/bridge":     http.HandlerFunc(vm.handleBridgeRequest),
		"/status":     http.HandlerFunc(vm.handleStatus),
		"/validators": http.HandlerFunc(vm.handleValidators),
	}
	return handlers, nil
}

// HealthCheck implements the common.VM interface
func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	return map[string]string{"status": "healthy"}, nil
}

// Shutdown implements the common.VM interface
func (vm *VM) Shutdown(ctx context.Context) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Clean up resources
	return nil
}

// CreateStaticHandlers implements the common.VM interface
func (vm *VM) CreateStaticHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return nil, nil
}

// Connected implements the common.VM interface
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion interface{}) error {
	return nil
}

// Disconnected implements the common.VM interface
func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return nil
}

// AppRequest implements the common.VM interface
func (vm *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	// Bridge VMs may use this for cross-chain communication
	return nil
}

// AppResponse implements the common.VM interface
func (vm *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return nil
}

// AppRequestFailed implements the common.VM interface
func (vm *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	return nil
}

// AppGossip implements the common.VM interface
func (vm *VM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return nil
}

// Version implements the common.VM interface
func (vm *VM) Version(ctx context.Context) (string, error) {
	return Version.String(), nil
}

// CrossChainAppRequest implements the common.VM interface
func (vm *VM) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	// Bridge VMs handle cross-chain requests
	return nil
}

// CrossChainAppResponse implements the common.VM interface
func (vm *VM) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	return nil
}

// CrossChainAppRequestFailed implements the common.VM interface
func (vm *VM) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32, appErr *common.AppError) error {
	return nil
}

// GetBlockIDAtHeight implements the consensusman.HeightIndexedChainVM interface
func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	// For now, return not implemented
	// In production, maintain a height index
	return ids.Empty, errors.New("height index not implemented")
}

// SetState implements the common.VM interface
func (vm *VM) SetState(ctx context.Context, state uint32) error {
	// For now, no-op
	// In production, handle state transitions
	return nil
}

// NewHTTPHandler returns HTTP handlers for the VM
func (vm *VM) NewHTTPHandler(ctx context.Context) (interface{}, error) {
	return vm.CreateHandlers(ctx)
}

// WaitForEvent blocks until an event occurs that should trigger block building
func (vm *VM) WaitForEvent(ctx context.Context) (interface{}, error) {
	// For now, return nil indicating no events to wait for
	// In production, this would wait for bridge requests, etc.
	return nil, nil
}

// Helper methods

func (vm *VM) putBlock(blk *Block) error {
	bytes, err := Codec.Marshal(codecVersion, blk)
	if err != nil {
		return err
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
	if _, err := Codec.Unmarshal(bytes, blk); err != nil {
		return nil, err
	}

	blk.ID_ = id
	return blk, nil
}

// HTTP handler methods

func (vm *VM) handleBridgeRequest(w http.ResponseWriter, r *http.Request) {
	// Handle bridge request
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "bridge request handler"}`))
}

func (vm *VM) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Handle status request
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "operational"}`))
}

func (vm *VM) handleValidators(w http.ResponseWriter, r *http.Request) {
	// Handle validators request
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"validators": []}`))
}

// Genesis represents the genesis state
type Genesis struct {
	Timestamp int64 `json:"timestamp"`
}
