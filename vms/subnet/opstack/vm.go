// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package opstack

import (
	"context"
	"errors"
	"math/big"
	"time"

		"github.com/ethereum/go-ethereum/common"
		"github.com/ethereum/go-ethereum/core/types"
		"github.com/ethereum/go-ethereum/crypto"
	
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/vms"
)

var (
	_ vms.VM = (*VM)(nil)

	errNotImplemented = errors.New("not implemented")
)

// OPStackConfig contains configuration for OP Stack based subnets
type OPStackConfig struct {
	// Sequencer configuration
	SequencerPrivateKey string
	SequencerAddress    common.Address
	
	// L1 configuration
	L1RPCEndpoint    string
	L1ChainID        uint64
	L1Confirmations  uint64
	
	// OP Stack parameters
	BlockTime        time.Duration
	MaxBlockGas      uint64
	DataAvailability string // "ethereum" or "luxda"
	
	// Rollup configuration
	ChallengeWindow     time.Duration
	FinalizationPeriod  time.Duration
	ProofSubmissionGas  uint64
	
	// Multi-consensus support
	ConsensusType string // "optimistic", "zk", "hybrid"
	ZKProverEndpoint string
}

// VM implements an OP Stack based subnet VM
type VM struct {
	ctx    *snow.Context
	config *OPStackConfig
	
	// OP Stack components
	sequencer    *Sequencer
	batcher      *Batcher
	proposer     *Proposer
	challenger   *Challenger
	
	// State management
	stateDB      StateDB
	pendingBlock *types.Block
	
	// Consensus
	consensusEngine ConsensusEngine
	
	log logging.Logger
}

// Sequencer handles transaction ordering and block production
type Sequencer struct {
	privateKey *crypto.PrivateKey
	address    common.Address
	mempool    *Mempool
	vm         *VM
}

// Batcher batches transactions for L1 submission
type Batcher struct {
	l1Client     L1Client
	batchSize    int
	batchTimeout time.Duration
	vm           *VM
}

// Proposer proposes state roots to L1
type Proposer struct {
	l1Client         L1Client
	proposalInterval time.Duration
	vm               *VM
}

// Challenger handles fraud proofs
type Challenger struct {
	l1Client        L1Client
	challengeWindow time.Duration
	vm              *VM
}

// ConsensusEngine defines the consensus mechanism
type ConsensusEngine interface {
	ValidateBlock(block *types.Block) error
	FinalizeBlock(block *types.Block) error
	GetFinalizationProof(blockHash common.Hash) ([]byte, error)
}

// Initialize initializes the VM
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db vms.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- vms.Message,
	fxs []*vms.Fx,
	appSender vms.AppSender,
) error {
	vm.ctx = chainCtx
	vm.log = chainCtx.Log
	
	// Parse configuration
	config, err := ParseConfig(configBytes)
	if err != nil {
		return err
	}
	vm.config = config
	
	// Initialize state database
	vm.stateDB = NewStateDB(db)
	
	// Initialize consensus engine based on type
	switch config.ConsensusType {
	case "optimistic":
		vm.consensusEngine = NewOptimisticConsensus(config)
	case "zk":
		vm.consensusEngine = NewZKConsensus(config)
	case "hybrid":
		vm.consensusEngine = NewHybridConsensus(config)
	default:
		return errors.New("unsupported consensus type")
	}
	
	// Initialize components
	vm.sequencer = NewSequencer(config, vm)
	vm.batcher = NewBatcher(config, vm)
	vm.proposer = NewProposer(config, vm)
	vm.challenger = NewChallenger(config, vm)
	
	// Start background services
	go vm.sequencer.Start()
	go vm.batcher.Start()
	go vm.proposer.Start()
	go vm.challenger.Start()
	
	vm.log.Info("OP Stack VM initialized",
		"consensusType", config.ConsensusType,
		"dataAvailability", config.DataAvailability,
	)
	
	return nil
}

// BuildBlock builds a new block
func (vm *VM) BuildBlock(ctx context.Context) (vms.Block, error) {
	// Get pending transactions from mempool
	txs := vm.sequencer.mempool.GetPendingTransactions(int(vm.config.MaxBlockGas))
	
	// Create new block
	block := &OPBlock{
		Height:       vm.stateDB.GetHeight() + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: txs,
		StateRoot:    common.Hash{}, // Will be computed
	}
	
	// Execute transactions and compute state root
	stateRoot, receipts, err := vm.executeTransactions(txs)
	if err != nil {
		return nil, err
	}
	block.StateRoot = stateRoot
	block.Receipts = receipts
	
	// Apply consensus rules
	if err := vm.consensusEngine.ValidateBlock(block.ToEthBlock()); err != nil {
		return nil, err
	}
	
	vm.pendingBlock = block.ToEthBlock()
	
	return block, nil
}

// ParseBlock parses a block from bytes
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (vms.Block, error) {
	block := &OPBlock{}
	if err := block.Unmarshal(blockBytes); err != nil {
		return nil, err
	}
	return block, nil
}

// GetBlock retrieves a block by ID
func (vm *VM) GetBlock(ctx context.Context, blkID ids.ID) (vms.Block, error) {
	return vm.stateDB.GetBlock(blkID)
}

// LastAccepted returns the last accepted block ID
func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	return vm.stateDB.GetLastAccepted()
}

// executeTransactions executes a list of transactions
func (vm *VM) executeTransactions(txs []*types.Transaction) (common.Hash, []*types.Receipt, error) {
	// Create a new state transition
	stateDB := vm.stateDB.Copy()
	
	var receipts []*types.Receipt
	gasUsed := uint64(0)
	
	for i, tx := range txs {
		// Check gas limit
		if gasUsed+tx.Gas() > vm.config.MaxBlockGas {
			break
		}
		
		// Execute transaction
		receipt, err := vm.applyTransaction(stateDB, tx, uint64(i))
		if err != nil {
			vm.log.Warn("Failed to execute transaction",
				"tx", tx.Hash().Hex(),
				"error", err,
			)
			continue
		}
		
		receipts = append(receipts, receipt)
		gasUsed += receipt.GasUsed
	}
	
	// Compute new state root
	stateRoot := stateDB.IntermediateRoot(true)
	
	// Commit state changes
	if err := stateDB.Commit(true); err != nil {
		return common.Hash{}, nil, err
	}
	
	return stateRoot, receipts, nil
}

// applyTransaction applies a single transaction to the state
func (vm *VM) applyTransaction(stateDB StateDB, tx *types.Transaction, index uint64) (*types.Receipt, error) {
	// This is a simplified transaction execution
	// In production, use full EVM execution
	
	receipt := &types.Receipt{
		Type:              tx.Type(),
		PostState:         []byte{},
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: tx.Gas(),
		Logs:              []*types.Log{},
		TxHash:            tx.Hash(),
		GasUsed:           tx.Gas(),
		BlockNumber:       big.NewInt(int64(vm.stateDB.GetHeight() + 1)),
		TransactionIndex:  uint(index),
	}
	
	return receipt, nil
}

// Shutdown shuts down the VM
func (vm *VM) Shutdown(ctx context.Context) error {
	vm.log.Info("Shutting down OP Stack VM")
	
	// Stop all components
	vm.sequencer.Stop()
	vm.batcher.Stop()
	vm.proposer.Stop()
	vm.challenger.Stop()
	
	return nil
}

// HealthCheck performs a health check
func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	health := &Health{
		SequencerHealthy:  vm.sequencer.IsHealthy(),
		BatcherHealthy:    vm.batcher.IsHealthy(),
		ProposerHealthy:   vm.proposer.IsHealthy(),
		ChallengerHealthy: vm.challenger.IsHealthy(),
		BlockHeight:       vm.stateDB.GetHeight(),
		PendingTxs:        vm.sequencer.mempool.Size(),
	}
	return health, nil
}

// Health represents the health status of the VM
type Health struct {
	SequencerHealthy  bool   `json:"sequencerHealthy"`
	BatcherHealthy    bool   `json:"batcherHealthy"`
	ProposerHealthy   bool   `json:"proposerHealthy"`
	ChallengerHealthy bool   `json:"challengerHealthy"`
	BlockHeight       uint64 `json:"blockHeight"`
	PendingTxs        int    `json:"pendingTxs"`
}

// Multi-consensus support functions

// NewOptimisticConsensus creates an optimistic rollup consensus engine
func NewOptimisticConsensus(config *OPStackConfig) ConsensusEngine {
	return &OptimisticConsensus{
		challengeWindow:    config.ChallengeWindow,
		finalizationPeriod: config.FinalizationPeriod,
	}
}

// NewZKConsensus creates a ZK rollup consensus engine
func NewZKConsensus(config *OPStackConfig) ConsensusEngine {
	return &ZKConsensus{
		proverEndpoint: config.ZKProverEndpoint,
	}
}

// NewHybridConsensus creates a hybrid consensus engine
func NewHybridConsensus(config *OPStackConfig) ConsensusEngine {
	return &HybridConsensus{
		optimistic: NewOptimisticConsensus(config),
		zk:         NewZKConsensus(config),
	}
}

// Additional VM methods for compatibility
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	return nil
}

func (vm *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	return vm.stateDB.SetPreference(blkID)
}

func (vm *VM) Version(ctx context.Context) (string, error) {
	return "opstack-v1.0.0", nil
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	vm.log.Info("Node connected", "nodeID", nodeID)
	return nil
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	vm.log.Info("Node disconnected", "nodeID", nodeID)
	return nil
}

func (vm *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return errNotImplemented
}

func (vm *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return errNotImplemented
}

func (vm *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return errNotImplemented
}

func (vm *VM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return errNotImplemented
}

func (vm *VM) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	return errNotImplemented
}

func (vm *VM) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	return errNotImplemented
}

func (vm *VM) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	return errNotImplemented
}