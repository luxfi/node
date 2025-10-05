// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package qvm

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/luxfi/consensus"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"

	"github.com/luxfi/database"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/pubsub"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/index"
	"github.com/luxfi/node/vms/qvm/config"
	"github.com/luxfi/node/vms/qvm/quantum"
)

const (
	// Version of the QVM
	Version = "1.0.0"

	// MaxParallelVerifications is the maximum number of parallel verifications
	MaxParallelVerifications = 100

	// DefaultBatchSize is the default batch size for parallel processing
	DefaultBatchSize = 10
)

var (
	errNotImplemented      = errors.New("not implemented")
	errInvalidQuantumStamp = errors.New("invalid quantum stamp")
	errParallelProcessingFailed = errors.New("parallel transaction processing failed")
	errVMShutdown         = errors.New("vm is shutting down")
)

// BCLookup provides blockchain alias lookup
type BCLookup interface {
	Lookup(string) (ids.ID, error)
	PrimaryAlias(ids.ID) (string, error)
}

// SharedMemory provides cross-chain shared memory
type SharedMemory interface {
	Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error)
	Apply(map[ids.ID]interface{}, ...interface{}) error
}

// VM implements the Q-chain Virtual Machine with quantum features
type VM struct {
	config.Config

	// Core components
	ctx             context.Context
	// consensusCtx    *consensusctx.Context
	log             log.Logger
	db              database.Database
	versiondb       *versiondb.Database
	blockchainID    ids.ID
	ChainAlias      string
	NetworkID       uint32

	// Quantum components
	quantumSigner   *quantum.QuantumSigner
	quantumCache    *cache.LRU[ids.ID, *quantum.QuantumSignature]

	// Consensus and validation
	// validators      validators.Manager
	// versionManager  consensusversion.Manager
	// consensusEngine consensus.Consensus

	// Metrics and monitoring
	metrics         metric.Registry

	// State management
	state           database.Database
	shuttingDown    bool
	shuttingDownMu  sync.RWMutex

	// Transaction processing
	txPool          *TransactionPool
	parallelWorkers int
	workerPool      *sync.Pool

	// Clock and timing
	clock           mockable.Clock

	// Network communication
	bcLookup        BCLookup
	sharedMemory    SharedMemory

	// HTTP service
	httpServer      *http.Server
	rpcServer       *rpc.Server

	// Synchronization
	lock            sync.RWMutex
}

// Initialize initializes the VM with the given context
func (vm *VM) Initialize(
	ctx context.Context,
	// chainCtx *consensusctx.Context,
	chainCtx interface{},
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- consensus.Message,
	fxs []*consensus.Fx,
	appSender consensus.AppSender,
) error {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	// Set up basic context
	vm.ctx = ctx
	// vm.consensusCtx = chainCtx
	vm.db = db
	// vm.blockchainID = chainCtx.ChainID
	// vm.NetworkID = chainCtx.NetworkID

	// Initialize logger
	// if vm.log == nil {
	//	vm.log = chainCtx.Log
	// }
	// vm.log.Info("initializing QVM",
	//	"version", Version,
	//	"chainID", vm.blockchainID,
	//	"networkID", vm.NetworkID,
	// )

	// Initialize quantum components
	vm.quantumSigner = quantum.NewQuantumSigner(
		vm.log,
		vm.Config.QuantumAlgorithmVersion,
		vm.Config.RingtailKeySize,
		vm.Config.QuantumStampWindow,
		vm.Config.QuantumSigCacheSize,
	)

	// Initialize transaction pool
	vm.txPool = NewTransactionPool(
		vm.Config.MaxParallelTxs,
		vm.Config.ParallelBatchSize,
		vm.log,
	)

	// Set up worker pool for parallel processing
	vm.parallelWorkers = vm.Config.MaxParallelTxs
	if vm.parallelWorkers <= 0 {
		vm.parallelWorkers = MaxParallelVerifications
	}
	vm.workerPool = &sync.Pool{
		New: func() interface{} {
			return &TransactionWorker{
				vm:            vm,
				quantumSigner: vm.quantumSigner,
			}
		},
	}

	// Initialize version database
	vm.versiondb = versiondb.New(vm.db)

	// Initialize metrics
	vm.metrics = chainCtx.Metrics

	// Parse genesis if provided
	if len(genesisBytes) > 0 {
		if err := vm.parseGenesis(genesisBytes); err != nil {
			return fmt.Errorf("failed to parse genesis: %w", err)
		}
	}

	// Initialize state
	vm.state = vm.versiondb

	// Set up HTTP handlers
	if err := vm.initializeHTTPHandlers(); err != nil {
		return fmt.Errorf("failed to initialize HTTP handlers: %w", err)
	}

	vm.log.Info("QVM initialized successfully",
		"quantumEnabled", vm.Config.QuantumStampEnabled,
		"ringtailEnabled", vm.Config.RingtailEnabled,
		"parallelTxs", vm.Config.MaxParallelTxs,
	)

	return nil
}

// BuildBlock builds a new block with pending transactions
func (vm *VM) BuildBlock(ctx context.Context) (consensus.Block, error) {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	// Check if VM is shutting down
	if vm.isShuttingDown() {
		return nil, errVMShutdown
	}

	// Get pending transactions from pool
	pendingTxs := vm.txPool.GetPendingTransactions(vm.Config.ParallelBatchSize)
	if len(pendingTxs) == 0 {
		return nil, consensus.ErrNoPendingTxs
	}

	// Process transactions in parallel
	validTxs, err := vm.processTransactionsParallel(pendingTxs)
	if err != nil {
		return nil, fmt.Errorf("failed to process transactions: %w", err)
	}

	// Create new block with valid transactions
	// Generate block ID from block data
	blockData := make([]byte, 0, 100)
	blockData = append(blockData, vm.getLastAcceptedID()[:]...)
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, vm.getHeight()+1)
	blockData = append(blockData, heightBytes...)

	blockID, _ := ids.ToID(blockData)
	block := &Block{
		id:           blockID,
		timestamp:    vm.clock.Time(),
		height:       vm.getHeight() + 1,
		parentID:     vm.getLastAcceptedID(),
		transactions: validTxs,
		vm:           vm,
	}

	// Sign block with quantum signature if enabled
	if vm.Config.QuantumStampEnabled {
		if err := vm.signBlockWithQuantum(block); err != nil {
			return nil, fmt.Errorf("failed to sign block with quantum stamp: %w", err)
		}
	}

	vm.log.Debug("built block",
		"blockID", block.ID(),
		"height", block.Height(),
		"txCount", len(validTxs),
	)

	return block, nil
}

// ParseBlock parses a block from bytes
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (consensus.Block, error) {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	block, err := vm.parseBlock(blockBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse block: %w", err)
	}

	// Verify quantum signature if enabled
	if vm.Config.QuantumStampEnabled {
		if err := vm.verifyBlockQuantumSignature(block); err != nil {
			return nil, fmt.Errorf("quantum signature verification failed: %w", err)
		}
	}

	return block, nil
}

// GetBlock retrieves a block by its ID
func (vm *VM) GetBlock(ctx context.Context, blockID ids.ID) (consensus.Block, error) {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	blockBytes, err := vm.state.Get(blockID[:])
	if err != nil {
		return nil, fmt.Errorf("failed to get block %s: %w", blockID, err)
	}

	return vm.parseBlock(blockBytes)
}

// SetState sets the VM state
func (vm *VM) SetState(ctx context.Context, state consensus.State) error {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	switch state {
	case consensus.Bootstrapping:
		vm.log.Info("QVM entering bootstrapping state")
	case consensus.NormalOp:
		vm.log.Info("QVM entering normal operation state")
	case consensus.StateSyncing:
		vm.log.Info("QVM entering state syncing")
	default:
		return fmt.Errorf("unknown state: %v", state)
	}

	return nil
}

// Shutdown gracefully shuts down the VM
func (vm *VM) Shutdown(ctx context.Context) error {
	vm.shuttingDownMu.Lock()
	vm.shuttingDown = true
	vm.shuttingDownMu.Unlock()

	vm.log.Info("shutting down QVM")

	// Stop HTTP server
	if vm.httpServer != nil {
		if err := vm.httpServer.Shutdown(ctx); err != nil {
			vm.log.Error("failed to shutdown HTTP server", "error", err)
		}
	}

	// Close transaction pool
	if vm.txPool != nil {
		vm.txPool.Close()
	}

	// Close database
	if vm.versiondb != nil {
		if err := vm.versiondb.Close(); err != nil {
			vm.log.Error("failed to close versiondb", "error", err)
		}
	}

	vm.log.Info("QVM shutdown complete")
	return nil
}

// processTransactionsParallel processes transactions in parallel batches
func (vm *VM) processTransactionsParallel(txs []Transaction) ([]Transaction, error) {
	if len(txs) == 0 {
		return nil, nil
	}

	// Determine batch size
	batchSize := vm.Config.ParallelBatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	var validTxs []Transaction
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process in batches
	for i := 0; i < len(txs); i += batchSize {
		end := i + batchSize
		if end > len(txs) {
			end = len(txs)
		}

		batch := txs[i:end]
		wg.Add(1)

		go func(batch []Transaction) {
			defer wg.Done()

			// Get worker from pool
			worker := vm.workerPool.Get().(*TransactionWorker)
			defer vm.workerPool.Put(worker)

			// Process batch
			validBatch, err := worker.ProcessBatch(batch)
			if err != nil {
				vm.log.Error("batch processing failed", "error", err)
				return
			}

			// Add valid transactions
			mu.Lock()
			validTxs = append(validTxs, validBatch...)
			mu.Unlock()
		}(batch)
	}

	wg.Wait()

	if len(validTxs) == 0 {
		return nil, errParallelProcessingFailed
	}

	return validTxs, nil
}

// signBlockWithQuantum signs a block with quantum signature
func (vm *VM) signBlockWithQuantum(block *Block) error {
	// Generate Ringtail key
	key, err := vm.quantumSigner.GenerateRingtailKey()
	if err != nil {
		return fmt.Errorf("failed to generate ringtail key: %w", err)
	}

	// Sign block data
	blockData := block.Bytes()
	sig, err := vm.quantumSigner.Sign(blockData, key)
	if err != nil {
		return fmt.Errorf("failed to sign block: %w", err)
	}

	block.quantumSignature = sig
	return nil
}

// verifyBlockQuantumSignature verifies a block's quantum signature
func (vm *VM) verifyBlockQuantumSignature(block *Block) error {
	if block.quantumSignature == nil {
		return errInvalidQuantumStamp
	}

	blockData := block.Bytes()
	return vm.quantumSigner.Verify(blockData, block.quantumSignature)
}

// parseGenesis parses genesis data
func (vm *VM) parseGenesis(genesisBytes []byte) error {
	// TODO: Implement genesis parsing
	vm.log.Info("parsing genesis", "size", len(genesisBytes))
	return nil
}

// parseBlock parses a block from bytes
func (vm *VM) parseBlock(blockBytes []byte) (*Block, error) {
	// TODO: Implement block parsing
	return &Block{
		vm: vm,
	}, nil
}

// initializeHTTPHandlers sets up HTTP handlers
func (vm *VM) initializeHTTPHandlers() error {
	vm.rpcServer = rpc.NewServer()

	// Register QVM service
	service := &Service{vm: vm}
	if err := vm.rpcServer.RegisterCodec(json.NewCodec(), "application/json"); err != nil {
		return err
	}
	if err := vm.rpcServer.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8"); err != nil {
		return err
	}
	return vm.rpcServer.RegisterService(service, "qvm")
}

// isShuttingDown returns true if VM is shutting down
func (vm *VM) isShuttingDown() bool {
	vm.shuttingDownMu.RLock()
	defer vm.shuttingDownMu.RUnlock()
	return vm.shuttingDown
}

// getHeight returns current blockchain height
func (vm *VM) getHeight() uint64 {
	// TODO: Implement actual height tracking
	return 0
}

// getLastAcceptedID returns the last accepted block ID
func (vm *VM) getLastAcceptedID() ids.ID {
	// TODO: Implement actual last accepted tracking
	return ids.Empty
}

// Version returns the version of the VM
func (vm *VM) Version(ctx context.Context) (string, error) {
	return Version, nil
}

// Connected notifies the VM that a validator has connected
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	vm.log.Debug("node connected", "nodeID", nodeID, "version", nodeVersion)
	return nil
}

// Disconnected notifies the VM that a validator has disconnected
func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	vm.log.Debug("node disconnected", "nodeID", nodeID)
	return nil
}

// HealthCheck returns the health status of the VM
func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	health := map[string]interface{}{
		"healthy": !vm.isShuttingDown(),
		"version": Version,
		"quantumEnabled": vm.Config.QuantumStampEnabled,
		"ringtailEnabled": vm.Config.RingtailEnabled,
		"pendingTxs": vm.txPool.PendingCount(),
	}

	return health, nil
}

// CreateHandlers returns HTTP handlers for the VM
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	handlers := map[string]http.Handler{
		"/rpc": vm.rpcServer,
	}
	return handlers, nil
}

// CreateStaticHandlers returns static HTTP handlers
func (vm *VM) CreateStaticHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return nil, nil
}