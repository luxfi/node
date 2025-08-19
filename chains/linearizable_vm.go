// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/core"
	// "github.com/luxfi/consensus/engine/chain" // currently unused
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/engine/dag/vertex"
	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
)

var (
	_ vertex.LinearizableVM = (*initializeOnLinearizeVM)(nil)
	_ block.ChainVM         = (*linearizeOnInitializeVM)(nil)

	// ErrSkipped is returned when a linearizable VM is asked to perform
	// chain VM operations
	ErrSkipped = errors.New("skipped")
)

// initializeOnLinearizeVM transforms the consensus engine's call to Linearize
// into a call to Initialize. This enables the proposervm to be initialized by
// the call to Linearize. This also provides the stopVertexID to the
// linearizeOnInitializeVM.
type initializeOnLinearizeVM struct {
	vertex.LinearizableVMWithEngine
	vmToInitialize block.ChainVM // Changed from core.VM to block.ChainVM
	vmToLinearize  *linearizeOnInitializeVM

	ctx          context.Context
	db           database.Database
	genesisBytes []byte
	upgradeBytes []byte
	configBytes  []byte
	fxs          []*core.Fx
	appSender    core.AppSender
}

func (vm *initializeOnLinearizeVM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	vm.vmToLinearize.stopVertexID = stopVertexID

	// Initialize the ChainVM
	// Convert consensus types to block types
	chainCtx := &block.ChainContext{
		NetworkID:    consensus.GetNetworkID(vm.ctx),
		SubnetID:     consensus.GetSubnetID(vm.ctx),
		ChainID:      consensus.GetChainID(vm.ctx),
		NodeID:       consensus.GetNodeID(vm.ctx),
		PublicKey:    consensus.PK(vm.ctx),
		LUXAssetID:   consensus.LuxAssetID(vm.ctx),
		CChainID:     ids.Empty, // Implementation note
		ChainDataDir: "",        // Implementation note
	}

	// Create DBManager wrapper
	dbManager := &dbManagerWrapper{db: vm.db}

	// Convert fxs - since core.Fx has no ID, we need to handle this differently
	var blockFxs []*block.Fx
	// For now, just create empty Fx entries
	for range vm.fxs {
		blockFxs = append(blockFxs, &block.Fx{})
	}

	// Create block AppSender wrapper
	blockAppSender := &blockAppSenderWrapper{appSender: vm.appSender}

	// Create message channel
	toEngine := make(chan block.Message, 1)

	return vm.vmToInitialize.Initialize(
		ctx,
		chainCtx,
		dbManager,
		vm.genesisBytes,
		vm.upgradeBytes,
		vm.configBytes,
		toEngine,
		blockFxs,
		blockAppSender,
	)
}

// dbManagerWrapper wraps a database.Database to implement block.DBManager
type dbManagerWrapper struct {
	db database.Database
}

func (d *dbManagerWrapper) Current() database.Database {
	return d.db
}

func (d *dbManagerWrapper) Get(version uint64) (database.Database, error) {
	// For now, just return the current database
	return d.db, nil
}

func (d *dbManagerWrapper) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// blockAppSenderWrapper wraps core.AppSender to implement block.AppSender
type blockAppSenderWrapper struct {
	appSender core.AppSender
}

func (b *blockAppSenderWrapper) SendAppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, appRequestBytes []byte) error {
	if b.appSender == nil {
		return errors.New("app sender is nil")
	}
	// Convert single nodeID to a set
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	return b.appSender.SendAppRequest(ctx, nodeIDs, requestID, appRequestBytes)
}

func (b *blockAppSenderWrapper) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if b.appSender == nil {
		return errors.New("app sender is nil")
	}
	return b.appSender.SendAppResponse(ctx, nodeID, requestID, appResponseBytes)
}

func (b *blockAppSenderWrapper) SendAppGossip(ctx context.Context, appGossipBytes []byte) error {
	if b.appSender == nil {
		return errors.New("app sender is nil")
	}
	// SendAppGossip now requires a set of node IDs, use empty set for broadcast
	nodeIDs := set.NewSet[ids.NodeID](0)
	return b.appSender.SendAppGossip(ctx, nodeIDs, appGossipBytes)
}

// linearizeOnInitializeVM transforms the proposervm's call to Initialize into a
// call to Linearize. This enables the proposervm to provide its toEngine
// channel to the VM that is being linearized.
type linearizeOnInitializeVM struct {
	vertex.LinearizableVMWithEngine
	stopVertexID ids.ID

	// Stored from Initialize for later use
	chainCtx     context.Context
	db           database.Database
	genesisBytes []byte
	upgradeBytes []byte
	configBytes  []byte
	fxs          []*core.Fx
	appSender    core.AppSender
	toEngine     chan<- block.Message
}

// appSenderAdapter adapts block.AppSender to core.AppSender
type appSenderAdapter struct {
	appSender block.AppSender
}

func (a *appSenderAdapter) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	if a.appSender == nil {
		return errors.New("app sender is nil")
	}
	// block.AppSender expects a single nodeID, so we take the first one
	// This is appropriate for single-node subnets
	for nodeID := range nodeIDs {
		return a.appSender.SendAppRequest(ctx, nodeID, requestID, appRequestBytes)
	}
	return nil
}

func (a *appSenderAdapter) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if a.appSender == nil {
		return errors.New("app sender is nil")
	}
	return a.appSender.SendAppResponse(ctx, nodeID, requestID, appResponseBytes)
}

func (a *appSenderAdapter) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	if a.appSender == nil {
		return errors.New("app sender is nil")
	}
	// block.AppSender doesn't use nodeIDs for gossip
	return a.appSender.SendAppGossip(ctx, appGossipBytes)
}

func (a *appSenderAdapter) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	// block.AppSender doesn't have SendAppError, so we just log and return nil
	return nil
}

func (a *appSenderAdapter) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	// Not implemented for now - cross chain requests not supported
	return nil
}

func (a *appSenderAdapter) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	// Not implemented for now - cross chain responses not supported
	return nil
}

func (a *appSenderAdapter) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// SendAppGossipSpecific is the same as SendAppGossip for this implementation
	return a.SendAppGossip(ctx, nodeIDs, appGossipBytes)
}

func (a *appSenderAdapter) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	// Not implemented for now - cross chain errors not supported
	return nil
}

func NewLinearizeOnInitializeVM(vm vertex.LinearizableVMWithEngine) *linearizeOnInitializeVM {
	return &linearizeOnInitializeVM{
		LinearizableVMWithEngine: vm,
	}
}

func (vm *linearizeOnInitializeVM) Initialize(
	ctx context.Context,
	chainCtx *block.ChainContext,
	dbManager block.DBManager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- block.Message,
	fxs []*block.Fx,
	appSender block.AppSender,
) error {
	// Convert block types to consensus types for the underlying VM
	consensusCtx := context.Background()
	consensusCtx = consensus.WithIDs(consensusCtx, consensus.IDs{
		NetworkID: chainCtx.NetworkID,
		SubnetID:  chainCtx.SubnetID,
		ChainID:   chainCtx.ChainID,
		NodeID:    chainCtx.NodeID,
		PublicKey: chainCtx.PublicKey,
	})

	// Get current database from DBManager
	var db database.Database
	if dbManager != nil {
		db = dbManager.Current()
	}

	// Convert fxs
	var coreFxs []*core.Fx
	for range fxs {
		// core.Fx is an empty struct, so just create them
		coreFxs = append(coreFxs, &core.Fx{})
	}

	// Create core AppSender adapter
	coreAppSender := &appSenderAdapter{appSender: appSender}

	// Store for later use
	vm.chainCtx = consensusCtx
	vm.db = db
	vm.genesisBytes = genesisBytes
	vm.upgradeBytes = upgradeBytes
	vm.configBytes = configBytes
	vm.fxs = coreFxs
	vm.appSender = coreAppSender
	vm.toEngine = toEngine

	// Now linearize
	return vm.Linearize(ctx, vm.stopVertexID)
}

// BuildBlock implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) BuildBlock(ctx context.Context) (block.Block, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return nil, ErrSkipped
}

// ParseBlock implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) ParseBlock(ctx context.Context, b []byte) (block.Block, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return nil, ErrSkipped
}

// GetBlock implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) GetBlock(ctx context.Context, blkID ids.ID) (block.Block, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return nil, ErrSkipped
}

// SetPreference implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) SetPreference(ctx context.Context, blkID ids.ID) error {
	// This is a linearizable VM, not a chain VM, so we return an error
	return ErrSkipped
}

// LastAccepted implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return ids.Empty, ErrSkipped
}

// GetBlockIDAtHeight implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return ids.Empty, ErrSkipped
}
