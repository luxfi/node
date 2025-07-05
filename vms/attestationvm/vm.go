// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package attestationvm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
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
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/crypto/aggregated"
)

var (
	_ vms.VM = (*VM)(nil)

	Version = &version.Semantic{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	errNotImplemented = errors.New("not implemented")
)

// AttestationConfig contains VM configuration
type AttestationConfig struct {
	// Threshold signature configuration
	SignatureThreshold int `json:"signatureThreshold"`
	MaxSigners         int `json:"maxSigners"`
	
	// Oracle configuration
	OracleRegistryEnabled bool `json:"oracleRegistryEnabled"`
	
	// TEE configuration  
	TEEVerificationEnabled bool     `json:"teeVerificationEnabled"`
	TrustedEnclaveKeys     []string `json:"trustedEnclaveKeys"`
	
	// GPU proof configuration
	GPUProofVerificationEnabled bool `json:"gpuProofVerificationEnabled"`
	
	// Aggregated signature configuration
	AggregatedSignatureConfig *aggregated.SignatureConfig `json:"aggregatedSignatureConfig,omitempty"`
}

// VM implements the Attestation Chain VM
type VM struct {
	ctx    *snow.Context
	config AttestationConfig
	
	// State management
	db              database.Database
	attestationDB   *AttestationDB
	oracleRegistry  *OracleRegistry
	
	// Signature verification
	signatureVerifier *SignatureVerifier
	
	// Aggregated signature support
	signatureAggregator *aggregated.SignatureAggregator
	
	// Block management
	genesisBlock    *Block
	lastAcceptedID  ids.ID
	lastAccepted    *Block
	pendingBlocks   map[ids.ID]*Block
	
	// Consensus
	toEngine chan<- common.Message
	
	// Logging
	log logging.Logger
	
	mu sync.RWMutex
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
	fxs []*vms.Fx,
	appSender common.AppSender,
) error {
	vm.ctx = chainCtx
	vm.db = db
	vm.toEngine = toEngine
	vm.log = chainCtx.Log
	vm.pendingBlocks = make(map[ids.ID]*Block)
	
	// Parse configuration
	if err := utils.Codec.Unmarshal(configBytes, &vm.config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	
	// Initialize attestation database
	attestationDB, err := NewAttestationDB(db, vm.log)
	if err != nil {
		return fmt.Errorf("failed to initialize attestation DB: %w", err)
	}
	vm.attestationDB = attestationDB
	
	// Initialize oracle registry
	if vm.config.OracleRegistryEnabled {
		oracleRegistry, err := NewOracleRegistry(db, vm.log)
		if err != nil {
			return fmt.Errorf("failed to initialize oracle registry: %w", err)
		}
		vm.oracleRegistry = oracleRegistry
	}
	
	// Initialize signature verifier
	vm.signatureVerifier = NewSignatureVerifier(vm.config.SignatureThreshold, vm.log)
	
	// Initialize aggregated signature support if configured
	if vm.config.AggregatedSignatureConfig != nil {
		sigAggregator, err := aggregated.NewSignatureAggregator(*vm.config.AggregatedSignatureConfig, vm.log)
		if err != nil {
			return fmt.Errorf("failed to initialize signature aggregator: %w", err)
		}
		vm.signatureAggregator = sigAggregator
	}
	
	// Initialize genesis block
	genesis, err := ParseGenesis(genesisBytes)
	if err != nil {
		return fmt.Errorf("failed to parse genesis: %w", err)
	}
	
	vm.genesisBlock = &Block{
		Attestations: genesis.InitialAttestations,
		Height:       0,
		Timestamp:    genesis.Timestamp,
		vm:           vm,
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
	} else if err != nil {
		return err
	} else {
		vm.lastAcceptedID, _ = ids.ToID(lastAcceptedBytes)
		// Load the block (implementation depends on block storage)
	}
	
	vm.log.Info("Attestation VM initialized",
		zap.String("version", Version.String()),
		zap.Int("signatureThreshold", vm.config.SignatureThreshold),
		zap.Bool("oracleRegistry", vm.config.OracleRegistryEnabled),
		zap.Bool("teeVerification", vm.config.TEEVerificationEnabled),
		zap.Bool("gpuProofVerification", vm.config.GPUProofVerificationEnabled),
	)
	
	return nil
}

// BuildBlock builds a new block
func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	
	// Collect pending attestations from mempool
	attestations, err := vm.attestationDB.GetPendingAttestations(100) // Max 100 per block
	if err != nil {
		return nil, err
	}
	
	if len(attestations) == 0 {
		return nil, errors.New("no attestations to include in block")
	}
	
	// Create new block
	block := &Block{
		ParentID:     vm.lastAcceptedID,
		Height:       vm.lastAccepted.Height + 1,
		Timestamp:    time.Now().Unix(),
		Attestations: attestations,
		vm:           vm,
	}
	
	// Compute block ID
	block.ID_ = block.computeID()
	
	// Store pending block
	vm.pendingBlocks[block.ID()] = block
	
	vm.log.Debug("Built new block",
		zap.String("blockID", block.ID().String()),
		zap.Uint64("height", block.Height),
		zap.Int("attestations", len(attestations)),
	)
	
	return block, nil
}

// ParseBlock parses a block from bytes
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	block := &Block{vm: vm}
	if err := utils.Codec.Unmarshal(blockBytes, block); err != nil {
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
	vm.log.Info("Shutting down Attestation VM")
	
	if vm.attestationDB != nil {
		vm.attestationDB.Close()
	}
	
	if vm.oracleRegistry != nil {
		vm.oracleRegistry.Close()
	}
	
	return vm.db.Close()
}

// Version returns the VM version
func (vm *VM) Version(ctx context.Context) (string, error) {
	return Version.String(), nil
}

// HealthCheck performs a health check
func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	health := &Health{
		DatabaseHealthy:   true, // Simple check for now
		AttestationCount:  vm.attestationDB.GetAttestationCount(),
		LastBlockHeight:   vm.lastAccepted.Height,
		PendingBlockCount: len(vm.pendingBlocks),
	}
	
	if vm.oracleRegistry != nil {
		health.RegisteredOracles = vm.oracleRegistry.GetOracleCount()
	}
	
	return health, nil
}

// Health represents VM health status
type Health struct {
	DatabaseHealthy   bool   `json:"databaseHealthy"`
	AttestationCount  uint64 `json:"attestationCount"`
	LastBlockHeight   uint64 `json:"lastBlockHeight"`
	PendingBlockCount int    `json:"pendingBlockCount"`
	RegisteredOracles int    `json:"registeredOracles,omitempty"`
}

// CreateHandlers returns the VM handlers
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return map[string]http.Handler{
		"/attestation": NewAttestationHandler(vm),
		"/oracle":      NewOracleHandler(vm),
	}, nil
}

// SubmitAttestation submits a new attestation
func (vm *VM) SubmitAttestation(att *Attestation) error {
	// Verify attestation signature
	if err := vm.verifyAttestation(att); err != nil {
		return fmt.Errorf("attestation verification failed: %w", err)
	}
	
	// Add to pending pool
	return vm.attestationDB.AddPendingAttestation(att)
}

// verifyAttestation verifies an attestation
func (vm *VM) verifyAttestation(att *Attestation) error {
	switch att.Type {
	case AttestationTypeOracle:
		return vm.verifyOracleAttestation(att)
	case AttestationTypeTEE:
		return vm.verifyTEEAttestation(att)
	case AttestationTypeGPU:
		return vm.verifyGPUProof(att)
	default:
		return errors.New("unknown attestation type")
	}
}

// verifyOracleAttestation verifies oracle attestations
func (vm *VM) verifyOracleAttestation(att *Attestation) error {
	if !vm.config.OracleRegistryEnabled {
		return errors.New("oracle attestations not enabled")
	}
	
	// Check if attestation uses aggregated signatures
	if att.AggregatedSignature != nil && vm.signatureAggregator != nil {
		// Verify aggregated signature (BLS or Ringtail)
		return vm.signatureAggregator.VerifyAggregatedSignature(att.Data, att.AggregatedSignature)
	}
	
	// Otherwise use traditional threshold signatures
	return vm.signatureVerifier.VerifyThresholdSignature(
		att.Data,
		att.Signatures,
		att.SignerIDs,
	)
}

// verifyTEEAttestation verifies TEE attestations
func (vm *VM) verifyTEEAttestation(att *Attestation) error {
	if !vm.config.TEEVerificationEnabled {
		return errors.New("TEE attestations not enabled")
	}
	
	// Verify enclave signature
	// This would integrate with Intel SGX or similar
	return errors.New("TEE verification not yet implemented")
}

// verifyGPUProof verifies GPU computation proofs
func (vm *VM) verifyGPUProof(att *Attestation) error {
	if !vm.config.GPUProofVerificationEnabled {
		return errors.New("GPU proof verification not enabled")
	}
	
	// Verify ZK proof of GPU computation
	// This would integrate with proof verification libraries
	return errors.New("GPU proof verification not yet implemented")
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

func (vm *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return nil
}

func (vm *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return nil
}

func (vm *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return nil
}

func (vm *VM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return nil
}

func (vm *VM) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	return nil
}

func (vm *VM) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	return nil
}

func (vm *VM) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	return nil
}

var lastAcceptedKey = []byte("last_accepted")