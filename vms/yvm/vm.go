// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package yvm implements the Y-Chain (Yield-Curve/Years-Proof) Quantum State Ledger
// A minimal-footprint chain for quantum-safe checkpointing and cross-version asset migration
// Supports multiple network versions simultaneously via quantum state superposition
package yvm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/database"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/consensus/snowman"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/snow/engine/snowman/block"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/version"
)

var (
	_ block.ChainVM = (*VM)(nil)

	Version = &version.Semantic{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	errNotImplemented      = errors.New("not implemented")
	errNoUserTransactions  = errors.New("Y-Chain does not accept user transactions")
	errInvalidEpochRoot    = errors.New("invalid epoch root")
	errDuplicateEpochRoot  = errors.New("duplicate epoch root for epoch")
)

const (
	// epochDuration is approximately 1 hour
	epochDuration = 60 * 60 * time.Second
)

// YConfig contains VM configuration
type YConfig struct {
	// Epoch configuration
	EpochDuration   time.Duration `json:"epochDuration"`
	
	// Chains to checkpoint
	TrackedChains   []string      `json:"trackedChains"`
	
	// Bitcoin anchoring (optional)
	BitcoinEnabled  bool          `json:"bitcoinEnabled"`
	BitcoinRPC      string        `json:"bitcoinRPC,omitempty"`
	
	// IPFS archival (optional)
	IPFSEnabled     bool          `json:"ipfsEnabled"`
	IPFSGateway     string        `json:"ipfsGateway,omitempty"`
	
	// Slashing configuration
	EnableSlashing  bool          `json:"enableSlashing"`
	SlashingAmount  uint64        `json:"slashingAmount"`
	
	// Fork management
	EnableForkManagement bool     `json:"enableForkManagement"`
	SupportedVersions    []uint32 `json:"supportedVersions"`
	CurrentVersion       uint32   `json:"currentVersion"`
}

// VM implements the Y-Chain Quantum Checkpoint Ledger
type VM struct {
	ctx      *snow.Context
	db       database.Database
	config   YConfig
	toEngine chan<- common.Message
	log      logging.Logger

	// Epoch tracking
	currentEpoch    uint64
	epochStartTime  time.Time
	chainRoots      map[string][]byte // chainID -> latest root
	
	// SPHINCS+ integration
	sphincsAggregator *SPHINCSAggregator
	
	// Block management
	preferred      ids.ID
	lastAcceptedID ids.ID
	pendingBlocks  map[ids.ID]*Block
	
	// Checkpoint history
	epochCheckpoints map[uint64]*EpochCheckpoint
	
	// Fork management
	forkManager *ForkManager
	
	mu sync.RWMutex
}

// EpochCheckpoint represents a checkpoint for one epoch
type EpochCheckpoint struct {
	Epoch           uint64             `json:"epoch"`
	Timestamp       int64              `json:"timestamp"`
	ChainRoots      map[string][]byte  `json:"chainRoots"`
	EpochRootHash   []byte             `json:"epochRootHash"`
	SPHINCSSignature []byte            `json:"sphincsSignature"`
	BitcoinTxID     string             `json:"bitcoinTxId,omitempty"`
	IPFSHash        string             `json:"ipfsHash,omitempty"`
}

// SPHINCSAggregator handles SPHINCS+ signature aggregation
type SPHINCSAggregator struct {
	publicKeys      map[ids.NodeID][]byte
	pendingSigs     map[uint64]map[ids.NodeID][]byte // epoch -> nodeID -> signature
	mu              sync.RWMutex
}

// EpochRootTx is the only transaction type on Y-Chain
type EpochRootTx struct {
	Epoch       uint64             `json:"epoch"`
	ChainRoots  map[string][]byte  `json:"chainRoots"`
	NodeID      ids.NodeID         `json:"nodeId"`
	SPHINCSig   []byte             `json:"sphincsSig"`
}

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	vm.ctx = chainCtx
	vm.db = db
	vm.toEngine = toEngine
	vm.log = chainCtx.Log
	vm.pendingBlocks = make(map[ids.ID]*Block)
	vm.chainRoots = make(map[string][]byte)
	vm.epochCheckpoints = make(map[uint64]*EpochCheckpoint)
	
	// Initialize codec if not already done
	if Codec == nil {
		Codec = codec.NewDefaultManager()
	}

	// Parse configuration
	if _, err := Codec.Unmarshal(configBytes, &vm.config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	
	// Set defaults
	if vm.config.EpochDuration == 0 {
		vm.config.EpochDuration = epochDuration
	}
	
	// Initialize SPHINCS+ aggregator
	vm.sphincsAggregator = &SPHINCSAggregator{
		publicKeys:   make(map[ids.NodeID][]byte),
		pendingSigs:  make(map[uint64]map[ids.NodeID][]byte),
	}
	
	// Initialize fork manager if enabled
	if vm.config.EnableForkManagement {
		vm.forkManager = NewForkManager(vm.log)
		
		// Register initial version
		genesisVersion := &NetworkVersion{
			VersionID:       1,
			Name:            "Genesis",
			ActivationEpoch: 0,
			ParentVersion:   0,
			Features:        []string{"base", "checkpoint"},
		}
		if err := vm.forkManager.RegisterVersion(genesisVersion); err != nil {
			return fmt.Errorf("failed to register genesis version: %w", err)
		}
		
		// Register configured versions
		for _, versionID := range vm.config.SupportedVersions {
			if versionID > 1 {
				version := &NetworkVersion{
					VersionID:       versionID,
					Name:            fmt.Sprintf("v%d", versionID),
					ActivationEpoch: uint64(versionID * 1000), // Placeholder
					ParentVersion:   versionID - 1,
					Features:        []string{},
				}
				if err := vm.forkManager.RegisterVersion(version); err != nil {
					vm.log.Warn("failed to register version",
						zap.Uint32("versionID", versionID),
						zap.Error(err),
					)
				}
			}
		}
		
		vm.forkManager.currentVersion = vm.config.CurrentVersion
	}
	
	// Parse genesis
	genesis := &Genesis{}
	if _, err := Codec.Unmarshal(genesisBytes, genesis); err != nil {
		return fmt.Errorf("failed to parse genesis: %w", err)
	}
	
	// Set initial epoch
	vm.currentEpoch = 0
	vm.epochStartTime = time.Unix(genesis.Timestamp, 0)
	
	// Create genesis block
	genesisBlock := &Block{
		BlockHeight:    0,
		BlockTimestamp: genesis.Timestamp,
		ParentID:       ids.Empty,
		EpochRoots:     []*EpochRootTx{},
		vm:             vm,
	}
	
	genesisBlock.ID_ = genesisBlock.computeID()
	vm.lastAcceptedID = genesisBlock.ID()
	
	vm.log.Info("initialized Y-Chain quantum checkpoint ledger",
		zap.Duration("epochDuration", vm.config.EpochDuration),
		zap.Int("trackedChains", len(vm.config.TrackedChains)),
		zap.Bool("bitcoinEnabled", vm.config.BitcoinEnabled),
		zap.Bool("ipfsEnabled", vm.config.IPFSEnabled),
	)
	
	return vm.putBlock(genesisBlock)
}

// BuildBlock implements the snowman.ChainVM interface
func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	
	// Check if we're at epoch boundary
	now := time.Now()
	currentEpoch := uint64(now.Sub(vm.epochStartTime) / vm.config.EpochDuration)
	
	if currentEpoch <= vm.currentEpoch {
		return nil, errors.New("not at epoch boundary yet")
	}
	
	// Collect epoch roots from all chains
	epochRoots := make([]*EpochRootTx, 0)
	
	// Get pending signatures for this epoch
	vm.sphincsAggregator.mu.RLock()
	sigs, exists := vm.sphincsAggregator.pendingSigs[currentEpoch]
	vm.sphincsAggregator.mu.RUnlock()
	
	if !exists || len(sigs) == 0 {
		return nil, errors.New("no epoch root signatures available")
	}
	
	// Select the lexicographically smallest signature (deterministic)
	var selectedTx *EpochRootTx
	var smallestSig []byte
	
	for nodeID, sig := range sigs {
		if smallestSig == nil || bytes.Compare(sig, smallestSig) < 0 {
			smallestSig = sig
			selectedTx = &EpochRootTx{
				Epoch:      currentEpoch,
				ChainRoots: vm.chainRoots,
				NodeID:     nodeID,
				SPHINCSig:  sig,
			}
		}
	}
	
	if selectedTx == nil {
		return nil, errors.New("no valid epoch root transaction")
	}
	
	epochRoots = append(epochRoots, selectedTx)
	
	// Get parent block
	parentID := vm.preferred
	if parentID == ids.Empty {
		parentID = vm.lastAcceptedID
	}
	
	parent, err := vm.getBlock(parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent block: %w", err)
	}
	
	// Create new block
	block := &Block{
		ParentID:       parentID,
		BlockHeight:    parent.Height() + 1,
		BlockTimestamp: now.Unix(),
		Epoch:          currentEpoch,
		EpochRoots:     epochRoots,
		vm:             vm,
	}
	
	block.ID_ = block.computeID()
	
	// Verify block size
	blockBytes := block.Bytes()
	if blockBytes == nil {
		return nil, errors.New("failed to marshal block")
	}
	
	if len(blockBytes) > maxBlockSize {
		return nil, fmt.Errorf("block size %d exceeds maximum %d", len(blockBytes), maxBlockSize)
	}
	
	// Store pending block
	vm.pendingBlocks[block.ID()] = block
	
	vm.log.Info("built Y-Chain epoch checkpoint block",
		zap.Stringer("blockID", block.ID()),
		zap.Uint64("epoch", currentEpoch),
		zap.Int("blockSize", len(blockBytes)),
	)
	
	return block, nil
}

// SubmitEpochRoot allows validators to submit epoch roots
func (vm *VM) SubmitEpochRoot(nodeID ids.NodeID, tx *EpochRootTx) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	
	// Verify epoch
	now := time.Now()
	currentEpoch := uint64(now.Sub(vm.epochStartTime) / vm.config.EpochDuration)
	
	if tx.Epoch != currentEpoch {
		return fmt.Errorf("invalid epoch: expected %d, got %d", currentEpoch, tx.Epoch)
	}
	
	// Verify chain roots
	if len(tx.ChainRoots) > maxChains {
		return fmt.Errorf("too many chain roots: %d > %d", len(tx.ChainRoots), maxChains)
	}
	
	for chainID, root := range tx.ChainRoots {
		if len(root) > maxRootSize {
			return fmt.Errorf("root for chain %s exceeds max size", chainID)
		}
	}
	
	// Compute epoch root hash
	epochRootHash := computeEpochRootHash(tx.Epoch, tx.ChainRoots)
	
	// Verify SPHINCS+ signature
	vm.sphincsAggregator.mu.RLock()
	pubKey, exists := vm.sphincsAggregator.publicKeys[nodeID]
	vm.sphincsAggregator.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("unknown node ID: %s", nodeID)
	}
	
	if !verifySPHINCS(pubKey, epochRootHash, tx.SPHINCSig) {
		return errors.New("invalid SPHINCS+ signature")
	}
	
	// Store signature
	vm.sphincsAggregator.mu.Lock()
	if vm.sphincsAggregator.pendingSigs[tx.Epoch] == nil {
		vm.sphincsAggregator.pendingSigs[tx.Epoch] = make(map[ids.NodeID][]byte)
	}
	vm.sphincsAggregator.pendingSigs[tx.Epoch][nodeID] = tx.SPHINCSig
	vm.sphincsAggregator.mu.Unlock()
	
	// Update chain roots
	vm.chainRoots = tx.ChainRoots
	
	return nil
}

// GetBlock implements the snowman.ChainVM interface
func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (snowman.Block, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	
	// Check pending blocks first
	if block, exists := vm.pendingBlocks[id]; exists {
		return block, nil
	}
	
	return vm.getBlock(id)
}

// ParseBlock implements the snowman.ChainVM interface
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	block := &Block{vm: vm}
	if _, err := Codec.Unmarshal(blockBytes, block); err != nil {
		return nil, err
	}
	
	block.ID_ = block.computeID()
	return block, nil
}

// SetPreference implements the snowman.ChainVM interface
func (vm *VM) SetPreference(ctx context.Context, id ids.ID) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	
	vm.preferred = id
	return nil
}

// LastAccepted implements the snowman.ChainVM interface
func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	
	return vm.lastAcceptedID, nil
}

// CreateHandlers implements the common.VM interface
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	handlers := map[string]http.Handler{
		"/epoch":      http.HandlerFunc(vm.handleEpochStatus),
		"/checkpoint": http.HandlerFunc(vm.handleCheckpointQuery),
		"/verify":     http.HandlerFunc(vm.handleVerifyRoot),
	}
	
	// Add fork management handlers if enabled
	if vm.config.EnableForkManagement {
		handlers["/versions"] = http.HandlerFunc(vm.handleVersions)
		handlers["/migrate"] = http.HandlerFunc(vm.handleMigration)
		handlers["/quantum"] = http.HandlerFunc(vm.handleQuantumState)
	}
	
	return handlers, nil
}

// Helper functions

func computeEpochRootHash(epoch uint64, chainRoots map[string][]byte) []byte {
	h := sha256.New()
	
	// Write epoch number
	epochBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(epochBytes, epoch)
	h.Write(epochBytes)
	
	// Sort chain IDs for deterministic ordering
	chainIDs := make([]string, 0, len(chainRoots))
	for chainID := range chainRoots {
		chainIDs = append(chainIDs, chainID)
	}
	// Use standard library sort for strings
	sort.Strings(chainIDs)
	
	// Write each chain root
	for _, chainID := range chainIDs {
		h.Write([]byte(chainID))
		h.Write(chainRoots[chainID])
	}
	
	return h.Sum(nil)
}

func verifySPHINCS(pubKey, message, signature []byte) bool {
	// TODO: Implement actual SPHINCS+ verification
	// This would use the NIST PQC reference implementation
	// For now, return true for testing
	return len(signature) > 0
}

func (vm *VM) putBlock(block *Block) error {
	blockBytes, err := Codec.Marshal(codecVersion, block)
	if err != nil {
		return err
	}
	id := block.ID()
	return vm.db.Put(id[:], blockBytes)
}

func (vm *VM) getBlock(id ids.ID) (*Block, error) {
	blockBytes, err := vm.db.Get(id[:])
	if err != nil {
		return nil, err
	}
	
	block := &Block{vm: vm}
	if _, err := Codec.Unmarshal(blockBytes, block); err != nil {
		return nil, err
	}
	
	block.ID_ = id
	return block, nil
}

// AppGossip implements the common.VM interface
func (vm *VM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	// Y-Chain doesn't use app gossip
	return nil
}

// AppRequest implements the common.VM interface
func (vm *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	// Y-Chain doesn't handle app requests
	return nil
}

// AppResponse implements the common.VM interface
func (vm *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	// Y-Chain doesn't handle app responses
	return nil
}

// AppRequestFailed implements the common.VM interface
func (vm *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	// Y-Chain doesn't handle app request failures
	return nil
}

// HealthCheck implements the common.VM interface
func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	
	health := map[string]interface{}{
		"status": "healthy",
		"currentEpoch": vm.currentEpoch,
		"lastAcceptedID": vm.lastAcceptedID.String(),
		"trackedChains": len(vm.config.TrackedChains),
	}
	
	return health, nil
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
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	// Track connected validators for SPHINCS+ signatures
	return nil
}

// Disconnected implements the common.VM interface
func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	// Remove disconnected validators
	return nil
}

// Version implements the common.VM interface
func (vm *VM) Version(ctx context.Context) (string, error) {
	return Version.String(), nil
}

// CrossChainAppRequest implements the common.VM interface
func (vm *VM) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
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

// GetBlockIDAtHeight implements the snowman.HeightIndexedChainVM interface
func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	// For now, return not implemented
	// In production, maintain a height index
	return ids.Empty, errors.New("height index not implemented")
}

// SetState implements the common.VM interface
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	// For now, no-op
	// In production, handle state transitions
	return nil
}

// HTTP handler implementations

func (vm *VM) handleEpochStatus(w http.ResponseWriter, r *http.Request) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	
	status := map[string]interface{}{
		"currentEpoch": vm.currentEpoch,
		"epochStartTime": vm.epochStartTime.Unix(),
		"chainRoots": vm.chainRoots,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (vm *VM) handleCheckpointQuery(w http.ResponseWriter, r *http.Request) {
	// Handle checkpoint queries
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "checkpoint handler"}`))
}

func (vm *VM) handleVerifyRoot(w http.ResponseWriter, r *http.Request) {
	// Handle root verification
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "verify handler"}`))
}

func (vm *VM) handleVersions(w http.ResponseWriter, r *http.Request) {
	// Handle version queries
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"versions": []}`))
}

func (vm *VM) handleMigration(w http.ResponseWriter, r *http.Request) {
	// Handle migration requests
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "migration handler"}`))
}

func (vm *VM) handleQuantumState(w http.ResponseWriter, r *http.Request) {
	// Handle quantum state queries
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "quantum state handler"}`))
}

// Additional handler implementations...

// Genesis represents the genesis state
type Genesis struct {
	Timestamp int64 `json:"timestamp"`
}