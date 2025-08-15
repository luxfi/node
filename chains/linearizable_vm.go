// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/engine/core"
	"github.com/luxfi/consensus/engine/dag/vertex"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/chain"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
)

var (
	_ vertex.LinearizableVM = (*initializeOnLinearizeVM)(nil)
	_ block.ChainVM         = (*linearizeOnInitializeVM)(nil)
)

// initializeOnLinearizeVM transforms the consensus engine's call to Linearize
// into a call to Initialize. This enables the proposervm to be initialized by
// the call to Linearize. This also provides the stopVertexID to the
// linearizeOnInitializeVM.
type initializeOnLinearizeVM struct {
	vertex.LinearizableVMWithEngine
	vmToInitialize core.VM
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
	
	// Check if vmToInitialize is a ChainVM
	if chainVM, ok := vm.vmToInitialize.(block.ChainVM); ok {
		// Convert consensus types to block types
		chainCtx := &block.ChainContext{
			NetworkID:    vm.ctx.NetworkID,
			SubnetID:     vm.ctx.SubnetID,
			ChainID:      vm.ctx.ChainID,
			NodeID:       vm.ctx.NodeID,
			PublicKey:    vm.ctx.PublicKey,
			LUXAssetID:   vm.ctx.LUXAssetID,
			CChainID:     vm.ctx.CChainID,
			ChainDataDir: vm.ctx.ChainDataDir,
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
		
		return chainVM.Initialize(
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
	
	// Fallback to core.VM Initialize (if it exists)
	if coreVM, ok := vm.vmToInitialize.(core.VM); ok {
		// core.VM's Initialize takes no parameters, so we just call it
		return coreVM.Initialize()
	}
	
	return errors.New("vmToInitialize does not implement ChainVM or core.VM")
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
	return b.appSender.SendAppRequest(ctx, nodeID, requestID, appRequestBytes)
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
	return b.appSender.SendAppGossip(ctx, appGossipBytes)
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

func (a *appSenderAdapter) SendAppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, appRequestBytes []byte) error {
	if a.appSender == nil {
		return errors.New("app sender is nil")
	}
	return a.appSender.SendAppRequest(ctx, nodeID, requestID, appRequestBytes)
}

func (a *appSenderAdapter) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if a.appSender == nil {
		return errors.New("app sender is nil")
	}
	return a.appSender.SendAppResponse(ctx, nodeID, requestID, appResponseBytes)
}

func (a *appSenderAdapter) SendAppGossip(ctx context.Context, appGossipBytes []byte) error {
	if a.appSender == nil {
		return errors.New("app sender is nil")
	}
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
	consensusCtx := &context.Context{
		NetworkID:    chainCtx.NetworkID,
		SubnetID:     chainCtx.SubnetID,
		ChainID:      chainCtx.ChainID,
		NodeID:       chainCtx.NodeID,
		PublicKey:    chainCtx.PublicKey,
		LUXAssetID:   chainCtx.LUXAssetID,
		CChainID:     chainCtx.CChainID,
		ChainDataDir: chainCtx.ChainDataDir,
	}
	
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
	return nil, chain.ErrSkipped
}

// ParseBlock implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) ParseBlock(ctx context.Context, b []byte) (block.Block, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return nil, chain.ErrSkipped
}

// GetBlock implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) GetBlock(ctx context.Context, blkID ids.ID) (block.Block, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return nil, chain.ErrSkipped
}

// SetPreference implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) SetPreference(ctx context.Context, blkID ids.ID) error {
	// This is a linearizable VM, not a chain VM, so we return an error
	return chain.ErrSkipped
}

// LastAccepted implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return ids.Empty, chain.ErrSkipped
}

// GetBlockIDAtHeight implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return ids.Empty, chain.ErrSkipped
}
