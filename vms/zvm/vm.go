// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zvm

import (
	"context"
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

	errNotImplemented = errors.New("not implemented")
)

// ZConfig contains VM configuration
type ZConfig struct {
	// Privacy configuration
	EnableConfidentialTransfers bool `json:"enableConfidentialTransfers"`
	EnablePrivateAddresses      bool `json:"enablePrivateAddresses"`
	
	// ZK proof configuration
	ProofSystem            string `json:"proofSystem"`            // groth16, plonk, etc.
	CircuitType           string `json:"circuitType"`            // transfer, mint, burn
	VerifyingKeyPath      string `json:"verifyingKeyPath"`
	TrustedSetupPath      string `json:"trustedSetupPath"`
	
	// FHE configuration
	EnableFHE              bool   `json:"enableFHE"`
	FHEScheme             string `json:"fheScheme"`              // BFV, CKKS, etc.
	SecurityLevel         int    `json:"securityLevel"`          // 128, 192, 256
	
	// Performance
	MaxUTXOsPerBlock      int    `json:"maxUtxosPerBlock"`
	ProofVerificationTimeout time.Duration `json:"proofVerificationTimeout"`
	ProofCacheSize        int    `json:"proofCacheSize"`
}

// VM implements the Zero-Knowledge UTXO Chain VM
type VM struct {
	ctx    *snow.Context
	config ZConfig
	
	// State management
	db              database.Database
	utxoDB          *UTXODB
	nullifierDB     *NullifierDB
	stateTree       *StateTree
	
	// Privacy components
	proofVerifier   *ProofVerifier
	fheProcessor    *FHEProcessor
	addressManager  *AddressManager
	
	// Block management
	genesisBlock    *Block
	lastAcceptedID  ids.ID
	lastAccepted    *Block
	pendingBlocks   map[ids.ID]*Block
	
	// Transaction mempool
	mempool         *Mempool
	
	// Consensus
	toEngine        chan<- common.Message
	
	// Logging
	log             logging.Logger
	
	mu              sync.RWMutex
}

// Initialize initializes the VM
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
	
	// Parse configuration
	if _, err := Codec.Unmarshal(configBytes, &vm.config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	
	// Initialize UTXO database
	utxoDB, err := NewUTXODB(db, vm.log)
	if err != nil {
		return fmt.Errorf("failed to initialize UTXO DB: %w", err)
	}
	vm.utxoDB = utxoDB
	
	// Initialize nullifier database
	nullifierDB, err := NewNullifierDB(db, vm.log)
	if err != nil {
		return fmt.Errorf("failed to initialize nullifier DB: %w", err)
	}
	vm.nullifierDB = nullifierDB
	
	// Initialize state tree
	stateTree, err := NewStateTree(db, vm.log)
	if err != nil {
		return fmt.Errorf("failed to initialize state tree: %w", err)
	}
	vm.stateTree = stateTree
	
	// Initialize proof verifier
	proofVerifier, err := NewProofVerifier(vm.config, vm.log)
	if err != nil {
		return fmt.Errorf("failed to initialize proof verifier: %w", err)
	}
	vm.proofVerifier = proofVerifier
	
	// Initialize FHE processor if enabled
	if vm.config.EnableFHE {
		fheProcessor, err := NewFHEProcessor(vm.config, vm.log)
		if err != nil {
			return fmt.Errorf("failed to initialize FHE processor: %w", err)
		}
		vm.fheProcessor = fheProcessor
	}
	
	// Initialize address manager
	addressManager, err := NewAddressManager(db, vm.config.EnablePrivateAddresses, vm.log)
	if err != nil {
		return fmt.Errorf("failed to initialize address manager: %w", err)
	}
	vm.addressManager = addressManager
	
	// Initialize mempool
	vm.mempool = NewMempool(1000, vm.log) // Max 1000 pending txs
	
	// Initialize genesis block
	genesis, err := ParseGenesis(genesisBytes)
	if err != nil {
		return fmt.Errorf("failed to parse genesis: %w", err)
	}
	
	vm.genesisBlock = &Block{
		BlockHeight:    0,
		BlockTimestamp: genesis.Timestamp,
		Txs:            genesis.InitialTxs,
		vm:             vm,
	}
	vm.genesisBlock.ID_ = vm.genesisBlock.computeID()
	
	// Load last accepted block
	lastAcceptedBytes, err := vm.db.Get(lastAcceptedKey)
	if err == database.ErrNotFound {
		// First time initialization
		vm.lastAccepted = vm.genesisBlock
		vm.lastAcceptedID = vm.genesisBlock.ID()
		
		if err := vm.db.Put(lastAcceptedKey, vm.lastAcceptedID[:]); err != nil {
			return err
		}
		
		// Process genesis transactions
		if err := vm.processGenesisTransactions(genesis); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		vm.lastAcceptedID, _ = ids.ToID(lastAcceptedBytes)
		// Load the block (implementation depends on block storage)
	}
	
	vm.log.Info("ZK UTXO VM initialized",
		zap.String("version", Version.String()),
		zap.Bool("confidentialTransfers", vm.config.EnableConfidentialTransfers),
		zap.Bool("privateAddresses", vm.config.EnablePrivateAddresses),
		zap.String("proofSystem", vm.config.ProofSystem),
		zap.Bool("fheEnabled", vm.config.EnableFHE),
	)
	
	return nil
}

// BuildBlock builds a new block
func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	
	// Get transactions from mempool
	txs := vm.mempool.GetPendingTransactions(vm.config.MaxUTXOsPerBlock)
	if len(txs) == 0 {
		return nil, errors.New("no transactions to include in block")
	}
	
	// Verify all transactions
	validTxs := make([]*Transaction, 0, len(txs))
	for _, tx := range txs {
		if err := vm.verifyTransaction(tx); err != nil {
			vm.log.Debug("Transaction verification failed",
				zap.String("txID", tx.ID.String()),
				zap.Error(err),
			)
			continue
		}
		validTxs = append(validTxs, tx)
	}
	
	if len(validTxs) == 0 {
		return nil, errors.New("no valid transactions to include in block")
	}
	
	// Create new block
	block := &Block{
		ParentID:       vm.lastAcceptedID,
		BlockHeight:    vm.lastAccepted.Height() + 1,
		BlockTimestamp: time.Now().Unix(),
		Txs:            validTxs,
		vm:             vm,
	}
	
	// Compute state root after applying transactions
	stateRoot, err := vm.computeStateRoot(validTxs)
	if err != nil {
		return nil, err
	}
	block.StateRoot = stateRoot
	
	// Compute block ID
	block.ID_ = block.computeID()
	
	// Store pending block
	vm.pendingBlocks[block.ID()] = block
	
	vm.log.Debug("Built new block",
		zap.String("blockID", block.ID().String()),
		zap.Uint64("height", block.BlockHeight),
		zap.Int("txCount", len(validTxs)),
	)
	
	return block, nil
}

// ParseBlock parses a block from bytes
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	block := &Block{vm: vm}
	if _, err := Codec.Unmarshal(blockBytes, block); err != nil {
		return nil, err
	}
	
	block.ID_ = block.computeID()
	return block, nil
}

// GetBlock retrieves a block by ID
func (vm *VM) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	
	// Check pending blocks
	if block, exists := vm.pendingBlocks[blkID]; exists {
		return block, nil
	}
	
	// Check if it's genesis
	if blkID == vm.genesisBlock.ID() {
		return vm.genesisBlock, nil
	}
	
	// Load from database
	blockBytes, err := vm.db.Get(blkID[:])
	if err != nil {
		return nil, err
	}
	
	return vm.ParseBlock(ctx, blockBytes)
}

// SetState sets the VM state
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	return nil
}

// Shutdown shuts down the VM
func (vm *VM) Shutdown(ctx context.Context) error {
	if vm.log != nil {
		vm.log.Info("Shutting down ZK UTXO VM")
	}
	
	if vm.utxoDB != nil {
		vm.utxoDB.Close()
	}
	
	if vm.nullifierDB != nil {
		vm.nullifierDB.Close()
	}
	
	if vm.stateTree != nil {
		vm.stateTree.Close()
	}
	
	if vm.addressManager != nil {
		vm.addressManager.Close()
	}
	
	if vm.db != nil {
		return vm.db.Close()
	}
	return nil
}

// Version returns the VM version
func (vm *VM) Version(ctx context.Context) (string, error) {
	return Version.String(), nil
}

// HealthCheck performs a health check
func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	health := &Health{
		DatabaseHealthy:    true,
		UTXOCount:         vm.utxoDB.GetUTXOCount(),
		NullifierCount:    vm.nullifierDB.GetNullifierCount(),
		LastBlockHeight:   vm.lastAccepted.Height(),
		PendingBlockCount: len(vm.pendingBlocks),
		MempoolSize:       vm.mempool.Size(),
		ProofCacheSize:    vm.proofVerifier.GetCacheSize(),
	}
	
	return health, nil
}

// Health represents VM health status
type Health struct {
	DatabaseHealthy    bool   `json:"databaseHealthy"`
	UTXOCount         uint64 `json:"utxoCount"`
	NullifierCount    uint64 `json:"nullifierCount"`
	LastBlockHeight   uint64 `json:"lastBlockHeight"`
	PendingBlockCount int    `json:"pendingBlockCount"`
	MempoolSize       int    `json:"mempoolSize"`
	ProofCacheSize    int    `json:"proofCacheSize"`
}

// CreateHandlers returns the VM handlers
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return map[string]http.Handler{
		"/rpc":     NewRPCHandler(vm),
		"/privacy": NewPrivacyHandler(vm),
		"/proof":   NewProofHandler(vm),
	}, nil
}

// verifyTransaction verifies a transaction including ZK proofs
func (vm *VM) verifyTransaction(tx *Transaction) error {
	// Check nullifiers aren't already spent
	for _, nullifier := range tx.Nullifiers {
		if vm.nullifierDB.IsNullifierSpent(nullifier) {
			return errors.New("nullifier already spent")
		}
	}
	
	// Verify ZK proof
	if err := vm.proofVerifier.VerifyTransactionProof(tx); err != nil {
		return fmt.Errorf("proof verification failed: %w", err)
	}
	
	// Verify FHE operations if enabled
	if vm.config.EnableFHE && tx.HasFHEOperations() {
		if err := vm.fheProcessor.VerifyFHEOperations(tx); err != nil {
			return fmt.Errorf("FHE verification failed: %w", err)
		}
	}
	
	return nil
}

// computeStateRoot computes the new state root after applying transactions
func (vm *VM) computeStateRoot(txs []*Transaction) ([]byte, error) {
	// Apply transactions to state tree
	for _, tx := range txs {
		if err := vm.stateTree.ApplyTransaction(tx); err != nil {
			return nil, err
		}
	}
	
	// Compute and return new root
	return vm.stateTree.ComputeRoot()
}

// processGenesisTransactions processes initial transactions from genesis
func (vm *VM) processGenesisTransactions(genesis *Genesis) error {
	for _, tx := range genesis.InitialTxs {
		// Add outputs to UTXO set
		for i, output := range tx.Outputs {
			utxo := &UTXO{
				TxID:        tx.ID,
				OutputIndex: uint32(i),
				Commitment:  output.Commitment,
				Ciphertext:  output.EncryptedNote,
				EphemeralPK: output.EphemeralPubKey,
				Height:      0, // Genesis height
			}
			if err := vm.utxoDB.AddUTXO(utxo); err != nil {
				return err
			}
		}
		
		// Add to state tree
		if err := vm.stateTree.ApplyTransaction(tx); err != nil {
			return err
		}
	}
	
	return nil
}

// Additional interface implementations
func (vm *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	return nil
}

func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.lastAcceptedID, nil
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	return nil
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return nil
}

// AppRequest implements the common.VM interface
func (vm *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
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

var lastAcceptedKey = []byte("last_accepted")