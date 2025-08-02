// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/crypto/bls"
	"github.com/luxfi/node/quasar/crypto/ringtail"
	"github.com/luxfi/node/utils/hashing"
)

// QuantumFinalityEngine coordinates dual-finality between P-Chain and Q-Chain
type QuantumFinalityEngine struct {
	// Chain references
	pChainID ids.ID
	qChainID ids.ID
	
	// Wrapped chains
	wrappedChains map[ids.ID]*ChainWrapper // A,B,C,M,X,Z chains
	
	// Consensus components
	blsVerifier     *bls.Verifier
	ringtailVerifier *ringtail.Verifier
	
	// Validator chain manager
	validatorManager ValidatorChainManager
	
	// Finality tracking
	pendingBlocks   map[ids.ID]*PendingBlock
	finalizedBlocks map[ids.ID]*FinalizedBlock
	
	// Synchronization
	mu              sync.RWMutex
	finalitySignals map[ids.ID]chan struct{}
}

// ValidatorChainManager interface for checking validator permissions
type ValidatorChainManager interface {
	CanValidateChain(validatorID ids.NodeID, chainID ids.ID) bool
	GetValidatorsForChain(chainID ids.ID) []ids.NodeID
	GetChainValidatorSet(chainID ids.ID) (*ChainValidatorSet, error)
}

// ChainValidatorSet represents validators for a specific chain
type ChainValidatorSet struct {
	ChainID        ids.ID
	Validators     []ids.NodeID
	IsGenesisGated bool
	MinValidators  int
}

// PendingBlock represents a block awaiting dual finality
type PendingBlock struct {
	BlockID        ids.ID
	ChainID        ids.ID
	Height         uint64
	Timestamp      time.Time
	
	// Operations from each chain
	Operations     map[ids.ID][]Operation // Chain -> Operations
	
	// Finality signatures
	PChainBLS      *bls.Signature
	QChainRingtail *ringtail.Signature
	
	// Status
	PChainFinalized bool
	QChainFinalized bool
}

// FinalizedBlock represents a block with quantum finality
type FinalizedBlock struct {
	PendingBlock
	
	// Dual finality proof
	FinalityProof *DualFinalityProof
	FinalizedAt   time.Time
}

// DualFinalityProof proves both P-Chain and Q-Chain consensus
type DualFinalityProof struct {
	BlockID         ids.ID
	Height          uint64
	
	// P-Chain BLS aggregate signature
	PChainSignature *bls.AggregateSignature
	PChainSigners   []ids.NodeID
	
	// Q-Chain Ringtail signature
	QChainSignature *ringtail.Signature
	QChainWitness   *ringtail.Witness
	
	// Combined proof
	ProofHash       ids.ID
	ProofTimestamp  time.Time
}

// Operation represents an operation from any chain (A/B/C/M/X/Z)
type Operation struct {
	ChainID       ids.ID
	OperationType string
	Payload       []byte
	Signature     []byte
	Timestamp     time.Time
}

// ChainWrapper wraps a chain for quantum finality
type ChainWrapper struct {
	ChainID       ids.ID
	ChainName     string
	
	// Operation collection
	operationPool *OperationPool
	
	// Finality callback
	onFinality    func(block *FinalizedBlock) error
}

// NewQuantumFinalityEngine creates a new quantum finality engine
func NewQuantumFinalityEngine(pChainID, qChainID ids.ID, validatorManager ValidatorChainManager) *QuantumFinalityEngine {
	return &QuantumFinalityEngine{
		pChainID:         pChainID,
		qChainID:         qChainID,
		wrappedChains:    make(map[ids.ID]*ChainWrapper),
		pendingBlocks:    make(map[ids.ID]*PendingBlock),
		finalizedBlocks:  make(map[ids.ID]*FinalizedBlock),
		finalitySignals:  make(map[ids.ID]chan struct{}),
		blsVerifier:      bls.NewVerifier(),
		ringtailVerifier: ringtail.NewVerifier(),
		validatorManager: validatorManager,
	}
}

// WrapChain adds a chain to be wrapped by quantum finality
func (qfe *QuantumFinalityEngine) WrapChain(chainID ids.ID, chainName string) error {
	qfe.mu.Lock()
	defer qfe.mu.Unlock()
	
	if _, exists := qfe.wrappedChains[chainID]; exists {
		return errors.New("chain already wrapped")
	}
	
	// Check if there are enough validators for this chain
	if qfe.validatorManager != nil {
		validatorSet, err := qfe.validatorManager.GetChainValidatorSet(chainID)
		if err != nil {
			return err
		}
		
		if len(validatorSet.Validators) < validatorSet.MinValidators {
			return errors.New("insufficient validators for chain " + chainName)
		}
	}
	
	wrapper := &ChainWrapper{
		ChainID:       chainID,
		ChainName:     chainName,
		operationPool: NewOperationPool(1000), // 1000 operations per chain
	}
	
	qfe.wrappedChains[chainID] = wrapper
	qfe.finalitySignals[chainID] = make(chan struct{}, 1)
	
	return nil
}

// SubmitOperation submits an operation from a wrapped chain
func (qfe *QuantumFinalityEngine) SubmitOperation(chainID ids.ID, op Operation) error {
	qfe.mu.RLock()
	wrapper, exists := qfe.wrappedChains[chainID]
	qfe.mu.RUnlock()
	
	if !exists {
		return errors.New("chain not wrapped")
	}
	
	return wrapper.operationPool.Add(op)
}

// CreateConsensusBlock creates a new consensus block with operations from all chains
func (qfe *QuantumFinalityEngine) CreateConsensusBlock(height uint64) (*PendingBlock, error) {
	qfe.mu.Lock()
	defer qfe.mu.Unlock()
	
	heightBytes := []byte{byte(height >> 56), byte(height >> 48), byte(height >> 40), byte(height >> 32),
		byte(height >> 24), byte(height >> 16), byte(height >> 8), byte(height)}
	blockID := hashing.ComputeHash256Array(heightBytes)
	
	block := &PendingBlock{
		BlockID:    blockID,
		Height:     height,
		Timestamp:  time.Now(),
		Operations: make(map[ids.ID][]Operation),
	}
	
	// Collect operations from all wrapped chains
	for chainID, wrapper := range qfe.wrappedChains {
		ops := wrapper.operationPool.GetBatch(100) // Max 100 ops per chain per block
		if len(ops) > 0 {
			block.Operations[chainID] = ops
		}
	}
	
	qfe.pendingBlocks[blockID] = block
	return block, nil
}

// SubmitPChainBLS submits P-Chain BLS signature for a block
func (qfe *QuantumFinalityEngine) SubmitPChainBLS(blockID ids.ID, sig *bls.Signature) error {
	qfe.mu.Lock()
	defer qfe.mu.Unlock()
	
	block, exists := qfe.pendingBlocks[blockID]
	if !exists {
		return errors.New("block not found")
	}
	
	// Verify BLS signature
	if err := qfe.blsVerifier.Verify(blockID[:], sig); err != nil {
		return err
	}
	
	block.PChainBLS = sig
	block.PChainFinalized = true
	
	// Check if we can finalize
	return qfe.checkFinality(block)
}

// SubmitQChainRingtail submits Q-Chain Ringtail signature for a block
func (qfe *QuantumFinalityEngine) SubmitQChainRingtail(blockID ids.ID, sig *ringtail.Signature) error {
	qfe.mu.Lock()
	defer qfe.mu.Unlock()
	
	block, exists := qfe.pendingBlocks[blockID]
	if !exists {
		return errors.New("block not found")
	}
	
	// Verify Ringtail signature
	if err := qfe.ringtailVerifier.Verify(blockID[:], sig); err != nil {
		return err
	}
	
	block.QChainRingtail = sig
	block.QChainFinalized = true
	
	// Check if we can finalize
	return qfe.checkFinality(block)
}

// checkFinality checks if a block has achieved dual finality
func (qfe *QuantumFinalityEngine) checkFinality(block *PendingBlock) error {
	if !block.PChainFinalized || !block.QChainFinalized {
		return nil // Not ready for finality
	}
	
	// Create dual finality proof
	proof := &DualFinalityProof{
		BlockID:         block.BlockID,
		Height:          block.Height,
		PChainSignature: &bls.AggregateSignature{Signature: block.PChainBLS},
		QChainSignature: block.QChainRingtail,
		ProofTimestamp:  time.Now(),
	}
	
	// Calculate proof hash
	proofData := append(block.BlockID[:], proof.PChainSignature.Bytes()...)
	proofData = append(proofData, proof.QChainSignature.Bytes()...)
	proof.ProofHash = hashing.ComputeHash256Array(proofData)
	
	// Create finalized block
	finalizedBlock := &FinalizedBlock{
		PendingBlock:  *block,
		FinalityProof: proof,
		FinalizedAt:   time.Now(),
	}
	
	// Move to finalized
	delete(qfe.pendingBlocks, block.BlockID)
	qfe.finalizedBlocks[block.BlockID] = finalizedBlock
	
	// Notify all chains
	for chainID, wrapper := range qfe.wrappedChains {
		if wrapper.onFinality != nil {
			go wrapper.onFinality(finalizedBlock)
		}
		
		// Signal finality
		select {
		case qfe.finalitySignals[chainID] <- struct{}{}:
		default:
		}
	}
	
	return nil
}

// WaitForFinality waits for a block to achieve quantum finality
func (qfe *QuantumFinalityEngine) WaitForFinality(ctx context.Context, blockID ids.ID) (*FinalizedBlock, error) {
	// Check if already finalized
	qfe.mu.RLock()
	if finalizedBlock, exists := qfe.finalizedBlocks[blockID]; exists {
		qfe.mu.RUnlock()
		return finalizedBlock, nil
	}
	
	// Get the chain signal channel
	signal := make(chan struct{}, 1)
	qfe.mu.RUnlock()
	
	// Wait for finality
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-signal:
		qfe.mu.RLock()
		finalizedBlock, exists := qfe.finalizedBlocks[blockID]
		qfe.mu.RUnlock()
		
		if !exists {
			return nil, errors.New("block finalized but not found")
		}
		return finalizedBlock, nil
	}
}

// GetFinalityStatus returns the finality status of a block
func (qfe *QuantumFinalityEngine) GetFinalityStatus(blockID ids.ID) (pChain bool, qChain bool, finalized bool) {
	qfe.mu.RLock()
	defer qfe.mu.RUnlock()
	
	if _, exists := qfe.finalizedBlocks[blockID]; exists {
		return true, true, true
	}
	
	if pending, exists := qfe.pendingBlocks[blockID]; exists {
		return pending.PChainFinalized, pending.QChainFinalized, false
	}
	
	return false, false, false
}

// ConsensusFlow represents the consensus flow for quantum finality
type ConsensusFlow struct {
	engine *QuantumFinalityEngine
}

// ExecuteConsensusRound executes a full consensus round
func (cf *ConsensusFlow) ExecuteConsensusRound(height uint64) (*FinalizedBlock, error) {
	// Step 1: Create consensus block with operations from all chains
	block, err := cf.engine.CreateConsensusBlock(height)
	if err != nil {
		return nil, err
	}
	
	// Step 2: P-Chain validators sign with BLS
	// This would be done by actual P-Chain validators
	// For now, we simulate it
	pChainSig := &bls.Signature{} // Placeholder
	if err := cf.engine.SubmitPChainBLS(block.BlockID, pChainSig); err != nil {
		return nil, err
	}
	
	// Step 3: Q-Chain validators sign with Ringtail
	// This would be done by actual Q-Chain validators
	// For now, we simulate it
	qChainSig := &ringtail.Signature{} // Placeholder
	if err := cf.engine.SubmitQChainRingtail(block.BlockID, qChainSig); err != nil {
		return nil, err
	}
	
	// Step 4: Wait for dual finality
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	return cf.engine.WaitForFinality(ctx, block.BlockID)
}

// OperationPool manages operations from a chain
type OperationPool struct {
	operations []Operation
	maxSize    int
	mu         sync.Mutex
}

// NewOperationPool creates a new operation pool
func NewOperationPool(maxSize int) *OperationPool {
	return &OperationPool{
		operations: make([]Operation, 0, maxSize),
		maxSize:    maxSize,
	}
}

// Add adds an operation to the pool
func (op *OperationPool) Add(operation Operation) error {
	op.mu.Lock()
	defer op.mu.Unlock()
	
	if len(op.operations) >= op.maxSize {
		return errors.New("operation pool full")
	}
	
	op.operations = append(op.operations, operation)
	return nil
}

// GetBatch gets a batch of operations
func (op *OperationPool) GetBatch(maxBatch int) []Operation {
	op.mu.Lock()
	defer op.mu.Unlock()
	
	if len(op.operations) == 0 {
		return nil
	}
	
	batchSize := len(op.operations)
	if batchSize > maxBatch {
		batchSize = maxBatch
	}
	
	batch := make([]Operation, batchSize)
	copy(batch, op.operations[:batchSize])
	
	// Remove batched operations
	op.operations = op.operations[batchSize:]
	
	return batch
}