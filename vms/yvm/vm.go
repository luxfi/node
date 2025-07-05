// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package yvm implements the Y-Chain (Yield-Curve/Years-Proof) Quantum Checkpoint Ledger
// A minimal-footprint chain for long-term cryptographic safety using hash-based signatures
package yvm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/consensus/snowman"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/snow/engine/snowman/block"
	"github.com/luxfi/node/utils"
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
	
	// maxRootSize limits individual chain root size
	maxRootSize = 32 // SHA-256 hash
	
	// maxChains limits number of chains we checkpoint
	maxChains = 16
	
	// blockSize target is under 5KB
	maxBlockSize = 5 * 1024
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
	
	// Parse configuration
	if err := utils.Codec.Unmarshal(configBytes, &vm.config); err != nil {
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
	
	// Parse genesis
	genesis := &Genesis{}
	if err := utils.Codec.Unmarshal(genesisBytes, genesis); err != nil {
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
	blockBytes, err := block.Bytes()
	if err != nil {
		return nil, err
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
func (vm *VM) ParseBlock(ctx context.Context, bytes []byte) (snowman.Block, error) {
	block := &Block{vm: vm}
	if err := utils.Codec.Unmarshal(bytes, block); err != nil {
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
	utils.Sort(chainIDs)
	
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
	bytes, err := utils.Codec.Marshal(codecVersion, block)
	if err != nil {
		return err
	}
	return vm.db.Put(block.ID()[:], bytes)
}

func (vm *VM) getBlock(id ids.ID) (*Block, error) {
	bytes, err := vm.db.Get(id[:])
	if err != nil {
		return nil, err
	}
	
	block := &Block{vm: vm}
	if err := utils.Codec.Unmarshal(bytes, block); err != nil {
		return nil, err
	}
	
	block.ID_ = id
	return block, nil
}

// Additional handler implementations...

const codecVersion = 0

// Genesis represents the genesis state
type Genesis struct {
	Timestamp int64 `json:"timestamp"`
}