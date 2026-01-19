// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package servicenodevm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/rpc/v2"
	grjson "github.com/gorilla/rpc/v2/json"

	"github.com/luxfi/vm/chain"
	vmcore "github.com/luxfi/vm"
	"github.com/luxfi/runtime"
	"github.com/luxfi/consensus/core/choices"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

const (
	Name = "servicenodevm"
)

var (
	_ chain.ChainVM = (*VM)(nil)

	lastAcceptedKey = []byte("lastAccepted")
)

// VM implements the ServiceNodeVM for private messaging infrastructure
type VM struct {
	rt     *runtime.Runtime
	config *Config
	log    log.Logger
	db     database.Database

	// Components
	registry *Registry

	// Block state
	pendingBlocks  map[ids.ID]*Block
	lastAccepted   *Block
	lastAcceptedID ids.ID

	// Pending transactions
	pendingRegs     []*RegistrationTx
	pendingExits    []*ExitTx
	pendingSlashes  []*SlashTx
	pendingProofs   []*UptimeProof
	pendingCommits  []*StorageCommitment

	mu sync.RWMutex

	// RPC
	rpcServer *rpc.Server
}

// Initialize implements chain.ChainVM
func (vm *VM) Initialize(
	ctx context.Context,
	vmInit vmcore.Init,
) error {
	vm.rt = vmInit.Runtime
	vm.db = vmInit.DB

	if logger, ok := vm.rt.Log.(log.Logger); ok {
		vm.log = logger
	} else {
		return errors.New("invalid logger type")
	}

	// Initialize config with defaults
	vm.config = DefaultConfig()

	// Parse genesis
	genesis, err := ParseGenesis(vmInit.Genesis)
	if err != nil {
		return fmt.Errorf("failed to parse genesis: %w", err)
	}

	// Merge genesis config
	if genesis.Config != nil {
		vm.config.Merge(genesis.Config)
	}

	// Validate config
	if err := vm.config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Initialize registry
	vm.registry = NewRegistry(vm.db, vm.config)
	if err := vm.registry.Load(ctx); err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}

	// Initialize pending state
	vm.pendingBlocks = make(map[ids.ID]*Block)
	vm.pendingRegs = make([]*RegistrationTx, 0)
	vm.pendingExits = make([]*ExitTx, 0)
	vm.pendingSlashes = make([]*SlashTx, 0)
	vm.pendingProofs = make([]*UptimeProof, 0)
	vm.pendingCommits = make([]*StorageCommitment, 0)

	// Initialize RPC server
	vm.rpcServer = rpc.NewServer()
	vm.rpcServer.RegisterCodec(grjson.NewCodec(), "application/json")
	vm.rpcServer.RegisterCodec(grjson.NewCodec(), "application/json;charset=UTF-8")
	vm.rpcServer.RegisterService(&Service{vm: vm}, "servicenode")

	// Load last accepted block
	if err := vm.loadLastAccepted(); err != nil {
		return err
	}

	// Register genesis nodes
	for _, node := range genesis.ServiceNodes {
		regTx := &RegistrationTx{
			NodeID:       node.NodeID,
			PublicKey:    node.PublicKey,
			EndpointHash: node.EndpointHash,
			StakeAmount:  node.StakeAmount,
			StakeLockEnd: node.StakeLockEnd,
		}

		if _, err := vm.registry.Register(ctx, regTx); err != nil {
			vm.log.Warn("Failed to register genesis node",
				log.String("nodeID", node.NodeID.String()),
				log.String("error", err.Error()),
			)
			continue
		}

		// Activate immediately
		if err := vm.registry.Activate(ctx, node.NodeID); err != nil {
			vm.log.Warn("Failed to activate genesis node",
				log.String("nodeID", node.NodeID.String()),
				log.String("error", err.Error()),
			)
		}
	}

	vm.log.Info("ServiceNodeVM initialized",
		log.Uint64("minStake", vm.config.MinStake),
		log.Uint32("nodesPerSwarm", vm.config.NodesPerSwarm),
		log.Int("genesisNodes", len(genesis.ServiceNodes)),
	)

	return nil
}

// loadLastAccepted loads the last accepted block from the database
func (vm *VM) loadLastAccepted() error {
	lastAcceptedBytes, err := vm.db.Get(lastAcceptedKey)
	if err == database.ErrNotFound {
		// No blocks yet, create genesis block
		vm.lastAccepted = &Block{
			BlockHeight:    0,
			BlockTimestamp: time.Now().Unix(),
			status:         choices.Accepted,
		}
		vm.lastAccepted.SetVM(vm)
		vm.lastAcceptedID = vm.lastAccepted.ID()
		return nil
	}
	if err != nil {
		return err
	}

	var blockID ids.ID
	copy(blockID[:], lastAcceptedBytes)

	blockBytes, err := vm.db.Get(blockID[:])
	if err != nil {
		return err
	}

	var block Block
	if err := json.Unmarshal(blockBytes, &block); err != nil {
		return err
	}

	block.SetVM(vm)
	block.SetStatus(choices.Accepted)
	vm.lastAccepted = &block
	vm.lastAcceptedID = blockID

	return nil
}

// SetState implements chain.ChainVM
func (vm *VM) SetState(ctx context.Context, state uint32) error {
	return nil
}

// NewHTTPHandler implements chain.ChainVM
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

// Shutdown implements chain.ChainVM
func (vm *VM) Shutdown(ctx context.Context) error {
	vm.log.Info("ServiceNodeVM shutting down")
	return nil
}

// CreateHandlers implements chain.ChainVM
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return map[string]http.Handler{
		"/rpc": vm.rpcServer,
	}, nil
}

// HealthCheck implements chain.ChainVM
func (vm *VM) HealthCheck(ctx context.Context) (*chain.HealthResult, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	activeCount := vm.registry.GetActiveNodeCount()

	return &chain.HealthResult{
		Healthy: activeCount >= vm.config.MinActiveNodes,
		Details: map[string]string{
			"activeNodes": fmt.Sprintf("%d", activeCount),
			"minRequired": fmt.Sprintf("%d", vm.config.MinActiveNodes),
		},
	}, nil
}

// Version implements chain.ChainVM
func (vm *VM) Version(ctx context.Context) (string, error) {
	return "1.0.0", nil
}

// Connected implements chain.ChainVM
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *chain.VersionInfo) error {
	vm.log.Debug("Node connected", log.String("nodeID", nodeID.String()))
	return nil
}

// Disconnected implements chain.ChainVM
func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	vm.log.Debug("Node disconnected", log.String("nodeID", nodeID.String()))
	return nil
}

// BuildBlock implements chain.ChainVM
func (vm *VM) BuildBlock(ctx context.Context) (chain.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Compute state roots
	registryRoot := vm.registry.ComputeRegistryRoot()

	block := &Block{
		ParentID_:       vm.lastAcceptedID,
		BlockHeight:     vm.lastAccepted.BlockHeight + 1,
		BlockTimestamp:  time.Now().Unix(),
		Registrations:   vm.pendingRegs,
		Exits:           vm.pendingExits,
		Slashes:         vm.pendingSlashes,
		UptimeProofs:    vm.pendingProofs,
		StorageCommits:  vm.pendingCommits,
		RegistryRoot:    registryRoot,
		status:          choices.Processing,
	}
	block.SetVM(vm)

	vm.pendingBlocks[block.ID()] = block

	return block, nil
}

// ParseBlock implements chain.ChainVM
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (chain.Block, error) {
	var block Block
	if err := json.Unmarshal(blockBytes, &block); err != nil {
		return nil, err
	}

	block.SetVM(vm)
	block.bytes = blockBytes

	return &block, nil
}

// GetBlock implements chain.ChainVM
func (vm *VM) GetBlock(ctx context.Context, blockID ids.ID) (chain.Block, error) {
	vm.mu.RLock()
	if vm.pendingBlocks != nil {
		if block, ok := vm.pendingBlocks[blockID]; ok {
			vm.mu.RUnlock()
			return block, nil
		}
	}
	vm.mu.RUnlock()

	blockBytes, err := vm.db.Get(blockID[:])
	if err != nil {
		return nil, err
	}

	var block Block
	if err := json.Unmarshal(blockBytes, &block); err != nil {
		return nil, err
	}

	block.SetVM(vm)
	block.bytes = blockBytes
	block.SetStatus(choices.Accepted)

	return &block, nil
}

// SetPreference implements chain.ChainVM
func (vm *VM) SetPreference(ctx context.Context, blockID ids.ID) error {
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

// WaitForEvent implements chain.ChainVM
func (vm *VM) WaitForEvent(ctx context.Context) (vmcore.Message, error) {
	return vmcore.Message{}, nil
}

// ========== Service Node Operations ==========

// RegisterServiceNode registers a new service node
func (vm *VM) RegisterServiceNode(ctx context.Context, tx *RegistrationTx) (*ServiceNode, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	node, err := vm.registry.Register(ctx, tx)
	if err != nil {
		return nil, err
	}

	// Add to pending transactions
	vm.pendingRegs = append(vm.pendingRegs, tx)

	return node, nil
}

// ActivateServiceNode activates a registered service node
func (vm *VM) ActivateServiceNode(ctx context.Context, nodeID ids.NodeID) error {
	return vm.registry.Activate(ctx, nodeID)
}

// DeactivateServiceNode starts the exit process for a service node
func (vm *VM) DeactivateServiceNode(ctx context.Context, nodeID ids.NodeID) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if err := vm.registry.Deactivate(ctx, nodeID); err != nil {
		return err
	}

	// Add to pending exits
	vm.pendingExits = append(vm.pendingExits, &ExitTx{
		NodeID:    nodeID,
		Timestamp: time.Now(),
	})

	return nil
}

// GetServiceNode retrieves a service node by ID
func (vm *VM) GetServiceNode(nodeID ids.NodeID) (*ServiceNode, error) {
	return vm.registry.Get(nodeID)
}

// GetActiveServiceNodes returns all active service nodes
func (vm *VM) GetActiveServiceNodes() []*ServiceNode {
	return vm.registry.GetActiveNodes()
}

// SubmitUptimeProof submits an uptime proof
func (vm *VM) SubmitUptimeProof(ctx context.Context, proof *UptimeProof) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Verify the node exists and is active
	node, err := vm.registry.Get(proof.NodeID)
	if err != nil {
		return err
	}

	if !node.IsActive() {
		return ErrNodeNotActive
	}

	vm.pendingProofs = append(vm.pendingProofs, proof)
	return nil
}

// SubmitStorageCommitment submits a storage commitment
func (vm *VM) SubmitStorageCommitment(ctx context.Context, commit *StorageCommitment) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Verify the node exists and is active
	node, err := vm.registry.Get(commit.NodeID)
	if err != nil {
		return err
	}

	if !node.IsActive() {
		return ErrNodeNotActive
	}

	vm.pendingCommits = append(vm.pendingCommits, commit)
	return nil
}

// GetRegistry returns the service node registry
func (vm *VM) GetRegistry() *Registry {
	return vm.registry
}

// GetConfig returns the VM configuration
func (vm *VM) GetConfig() *Config {
	return vm.config
}

// ========== Genesis ==========

// Genesis represents genesis data for ServiceNodeVM
type Genesis struct {
	Timestamp    int64          `json:"timestamp"`
	Config       *Config        `json:"config,omitempty"`
	ServiceNodes []*GenesisNode `json:"serviceNodes,omitempty"`
	Message      string         `json:"message,omitempty"`
}

// GenesisNode represents a genesis service node
type GenesisNode struct {
	NodeID       ids.NodeID `json:"nodeID"`
	PublicKey    []byte     `json:"publicKey"`
	EndpointHash [32]byte   `json:"endpointHash"`
	StakeAmount  uint64     `json:"stakeAmount"`
	StakeLockEnd uint64     `json:"stakeLockEnd"`
}

// ParseGenesis parses genesis bytes
func ParseGenesis(genesisBytes []byte) (*Genesis, error) {
	var genesis Genesis
	if len(genesisBytes) > 0 {
		if err := json.Unmarshal(genesisBytes, &genesis); err != nil {
			return nil, err
		}
	}

	if genesis.Timestamp == 0 {
		genesis.Timestamp = time.Now().Unix()
	}

	return &genesis, nil
}
