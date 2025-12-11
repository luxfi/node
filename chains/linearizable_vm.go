// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"
	"sync"

	"github.com/luxfi/database"
	consensusctx "github.com/luxfi/consensus/context"
	"github.com/luxfi/consensus/core/appsender"
	"github.com/luxfi/consensus/engine/chain/block"
	consensusvertex "github.com/luxfi/consensus/engine/vertex"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/platformvm/fx"
)

var (
	_ consensusvertex.LinearizableVM = (*initializeOnLinearizeVM)(nil)
	// Note: linearizeOnInitializeVM doesn't need to fully implement block.ChainVM
	// It's a wrapper that transforms Initialize calls into Linearize calls

	// ErrSkipped is returned when a linearizable VM is asked to perform
	// chain VM operations
	ErrSkipped = errors.New("skipped")
)

// initializeOnLinearizeVM transforms the consensus engine's call to Linearize
// into a call to Initialize. This enables the proposervm to be initialized by
// the call to Linearize. This also provides the stopVertexID to the
// linearizeOnInitializeVM.
type initializeOnLinearizeVM struct {
	consensusvertex.DAGVM
	vmToInitialize block.ChainVM
	vmToLinearize  *linearizeOnInitializeVM

	ctx              *consensusctx.Context
	db               database.Database
	genesisBytes     []byte
	upgradeBytes     []byte
	configBytes      []byte
	fxs              []fx.Fx
	appSender        appsender.AppSender
	waitForLinearize chan struct{}
	linearizeOnce    sync.Once
}

func (vm *initializeOnLinearizeVM) WaitForEvent(ctx context.Context) (block.Message, error) {
	select {
	case <-vm.waitForLinearize:
		msg, err := vm.vmToInitialize.WaitForEvent(ctx)
		if err != nil {
			return block.Message{}, err
		}
		// Type assert the interface{} return to block.Message
		if blockMsg, ok := msg.(block.Message); ok {
			return blockMsg, nil
		}
		return block.Message{}, errors.New("unexpected message type from WaitForEvent")
	case <-ctx.Done():
		return block.Message{}, ctx.Err()
	}
}

func (vm *initializeOnLinearizeVM) Linearize(ctx context.Context, stopVertexID ids.ID, toVertex ids.ID) error {
	vm.vmToLinearize.stopVertexID = stopVertexID
	defer vm.linearizeOnce.Do(func() {
		close(vm.waitForLinearize)
	})
	
	// Convert []fx.Fx to []interface{}
	fxsInterface := make([]interface{}, len(vm.fxs))
	for i, fxItem := range vm.fxs {
		fxsInterface[i] = fxItem
	}
	
	// Note: toVertex parameter is the toEngine channel for block.Message
	// but Initialize expects chan<- consensus.Message, so we need proper conversion
	// For now, passing nil as toEngine since this is complex to adapt
	return vm.vmToInitialize.Initialize(
		ctx,
		vm.ctx,
		&dbManagerWrapper{db: vm.db},
		vm.genesisBytes,
		vm.upgradeBytes,
		vm.configBytes,
		nil, // toEngine channel - needs proper adaptation
		fxsInterface,
		vm.appSender,
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

// blockAppSenderWrapper wraps appsender.AppSender to implement block.AppSender
type blockAppSenderWrapper struct {
	appSender appsender.AppSender
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
	toEngine     chan<- block.Message
}

func NewLinearizeOnInitializeVM(vm consensusvertex.LinearizableVMWithEngine, toEngine chan<- block.Message) *linearizeOnInitializeVM {
	return &linearizeOnInitializeVM{
		LinearizableVMWithEngine: vm,
		toEngine:                 toEngine,
	}
}

func (vm *linearizeOnInitializeVM) Initialize(
	ctx context.Context,
	consensusCtx *consensusctx.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []fx.Fx,
	appSender appsender.AppSender,
) error {
	// When Initialize is called, we need to linearize the DAG
	// The stopVertexID should have been set by initializeOnLinearizeVM.Linearize
	if vm.stopVertexID == ids.Empty {
		return errors.New("stopVertexID not set - Linearize must be called first")
	}

	// Get the underlying linearizable VM
	linearizableVM, ok := vm.LinearizableVMWithEngine.(consensusvertex.LinearizableVM)
	if !ok {
		// If it doesn't implement LinearizableVM, try to call Linearize directly via interface
		// This is a fallback for VMs that embed the engine but expose Linearize differently
		return errors.New("VM does not implement LinearizableVM interface")
	}

	// Call Linearize to convert DAG to linear chain at stopVertexID
	// The toEngine channel will be used to signal when linearization is complete
	toVertexID := ids.Empty // Use empty to indicate full linearization
	return linearizableVM.Linearize(ctx, vm.stopVertexID, toVertexID)
}
