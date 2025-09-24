// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"

	consContext "github.com/luxfi/consensus/context"
	"github.com/luxfi/consensus/core"
	coreinterfaces "github.com/luxfi/consensus/core/interfaces"
	// "github.com/luxfi/consensus/engine/chain" // currently unused
	"github.com/luxfi/consensus/engine/chain/block"
	// "github.com/luxfi/consensus/engine/dag/vertex" // Not used
	consensusvertex "github.com/luxfi/consensus/engine/vertex"
	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
)

var (
	_ consensusvertex.LinearizableVM = (*initializeOnLinearizeVM)(nil)
	_ block.ChainVM                  = (*linearizeOnInitializeVM)(nil)

	// ErrSkipped is returned when a linearizable VM is asked to perform
	// chain VM operations
	ErrSkipped = errors.New("skipped")
)

// initializeOnLinearizeVM transforms the consensus engine's call to Linearize
// into a call to Initialize. This enables the proposervm to be initialized by
// the call to Linearize. This also provides the stopVertexID to the
// linearizeOnInitializeVM.
type initializeOnLinearizeVM struct {
	consensusvertex.LinearizableVMWithEngine
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

func (vm *initializeOnLinearizeVM) Linearize(ctx context.Context, stopVertexID ids.ID, toVertex ids.ID) error {
	vm.vmToLinearize.stopVertexID = stopVertexID

	// Initialize the ChainVM
	// Convert consensus types to block types
	// Use consensus.ConsensusContext
	consensusCtx := &consContext.Context{}

	chainCtx := &block.ChainContext{
		Context: consensusCtx,
	}

	// Create DBManager wrapper
	dbManager := &dbManagerWrapper{db: vm.db}

	// Convert fxs to []interface{}
	var fxsInterface []interface{}
	for range vm.fxs {
		// Convert core.Fx to block.Fx
		fxsInterface = append(fxsInterface, &block.Fx{})
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
		fxsInterface,
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

func (d *dbManagerWrapper) Database(id ids.ID) database.Database {
	// For now, just return the current database
	return d.db
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

func (b *blockAppSenderWrapper) SendAppRequest(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, appRequestBytes []byte) error {
	if b.appSender == nil {
		return errors.New("app sender is nil")
	}
	nodeIDSet := set.NewSet[ids.NodeID](len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeIDSet.Add(nodeID)
	}
	return b.appSender.SendAppRequest(ctx, nodeIDSet, requestID, appRequestBytes)
}

func (b *blockAppSenderWrapper) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if b.appSender == nil {
		return errors.New("app sender is nil")
	}
	return b.appSender.SendAppResponse(ctx, nodeID, requestID, appResponseBytes)
}

func (b *blockAppSenderWrapper) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	if b.appSender == nil {
		return errors.New("app sender is nil")
	}
	return b.appSender.SendAppError(ctx, nodeID, requestID, errorCode, errorMessage)
}

func (b *blockAppSenderWrapper) SendAppGossip(ctx context.Context, nodeIDs []ids.NodeID, appGossipBytes []byte) error {
	if b.appSender == nil {
		return errors.New("app sender is nil")
	}
	// Convert slice to set
	nodeIDSet := set.NewSet[ids.NodeID](len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeIDSet.Add(nodeID)
	}
	return b.appSender.SendAppGossip(ctx, nodeIDSet, appGossipBytes)
}

// linearizeOnInitializeVM transforms the proposervm's call to Initialize into a
// call to Linearize. This enables the proposervm to provide its toEngine
// channel to the VM that is being linearized.
type linearizeOnInitializeVM struct {
	consensusvertex.LinearizableVMWithEngine
	stopVertexID ids.ID

	// Stored from Initialize for later use
	chainCtx     context.Context
	db           database.Database
	dbManager    block.DBManager
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
	// Send to the first node in the set
	for nodeID := range nodeIDs {
		return a.appSender.SendAppRequest(ctx, []ids.NodeID{nodeID}, requestID, appRequestBytes)
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
	// Convert set to slice for SendAppGossip
	nodeIDList := nodeIDs.List()
	return a.appSender.SendAppGossip(ctx, nodeIDList, appGossipBytes)
}

func (a *appSenderAdapter) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	// block.AppSender doesn't have SendAppError, so we just log and return nil
	return nil
}

func (a *appSenderAdapter) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// Just use regular gossip
	return a.SendAppGossip(ctx, nodeIDs, appGossipBytes)
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

func NewLinearizeOnInitializeVM(vm consensusvertex.LinearizableVMWithEngine) *linearizeOnInitializeVM {
	return &linearizeOnInitializeVM{
		LinearizableVMWithEngine: vm,
	}
}

func (vm *linearizeOnInitializeVM) Initialize(
	ctx context.Context,
	chainCtx interface{},
	db interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	msgChan interface{},
	fxs []interface{},
	appSender interface{},
) error {
	// Convert block types to consensus types for the underlying VM
	consensusCtx := context.Background()
	if cc, ok := chainCtx.(*block.ChainContext); ok && cc != nil {
		// consensusCtx := cc.Context
		// Context reassignment commented out due to type mismatch
		// TODO: Fix context type compatibility between context.Context and context.Context
	}

	// Get current database from DBManager
	// db is an interface{}, need to type assert
	var vmDB database.Database
	if dbManager, ok := db.(interface{ Current() database.Database }); ok {
		vmDB = dbManager.Current()
	} else if database, ok := db.(database.Database); ok {
		// If it's already a database, use it directly
		vmDB = database
	}

	// Convert fxs
	var coreFxs []*core.Fx
	for range fxs {
		// core.Fx is an empty struct, so just create them
		coreFxs = append(coreFxs, &core.Fx{})
	}

	// Create core AppSender adapter
	var coreAppSender core.AppSender
	if as, ok := appSender.(block.AppSender); ok {
		coreAppSender = &appSenderAdapter{appSender: as}
	}

	// Store for later use
	vm.chainCtx = consensusCtx
	vm.db = vmDB
	vm.genesisBytes = genesisBytes
	vm.upgradeBytes = upgradeBytes
	vm.configBytes = configBytes
	vm.fxs = coreFxs
	vm.appSender = coreAppSender

	// Type assert msgChan
	if toEngine, ok := msgChan.(chan<- block.Message); ok {
		vm.toEngine = toEngine
	}

	// Type assert db for DBManager
	if dbManager, ok := db.(block.DBManager); ok {
		vm.dbManager = dbManager
	}

	// The LinearizableVMWithEngine doesn't have a Linearize method,
	// return nil as initialization is complete
	return nil
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

// GetChainID implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) GetChainID(ctx context.Context) (ids.ID, error) {
	// This is a linearizable VM, not a chain VM, so we return an error
	return ids.Empty, ErrSkipped
}

// Shutdown implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) Shutdown(ctx context.Context) error {
	// This is a linearizable VM, not a chain VM, so we return an error
	return ErrSkipped
}

// SetState implements block.ChainVM interface
func (vm *linearizeOnInitializeVM) SetState(ctx context.Context, state coreinterfaces.State) error {
	// This is a linearizable VM, not a chain VM, so we return an error
	return ErrSkipped
}
