// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"sync"

	"github.com/luxfi/database"
	consensusctx "github.com/luxfi/consensus/context"
	"github.com/luxfi/consensus/core/appsender"
	"github.com/luxfi/consensus"
	"github.com/luxfi/ids"
	"github.com/luxfi/consensus/engine/dag/vertex"
	"github.com/luxfi/consensus/engine/core"
	"github.com/luxfi/consensus/engine/chain/block"
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
	vertex.DAGVM
	vmToInitialize core.VM
	vmToLinearize  *linearizeOnInitializeVM

	ctx              *consensusctx.Context
	db               database.Database
	genesisBytes     []byte
	upgradeBytes     []byte
	configBytes      []byte
	fxs              []*consensus.Fx
	appSender        appsender.AppSender
	waitForLinearize chan struct{}
	linearizeOnce    sync.Once
}

func (vm *initializeOnLinearizeVM) WaitForEvent(ctx context.Context) (block.Message, error) {
	select {
	case <-vm.waitForLinearize:
		return vm.vmToInitialize.WaitForEvent(ctx)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (vm *initializeOnLinearizeVM) Linearize(ctx context.Context, stopVertexID ids.ID, toVertex ids.ID) error {
	vm.vmToLinearize.stopVertexID = stopVertexID
	defer vm.linearizeOnce.Do(func() {
		close(vm.waitForLinearize)
	})
	return vm.vmToInitialize.Initialize(
		ctx,
		chainCtx,
		dbManager,
		vm.genesisBytes,
		vm.upgradeBytes,
		vm.configBytes,
		vm.fxs,
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
	toEngine     chan<- block.Message
}

func NewLinearizeOnInitializeVM(vm vertex.LinearizableVMWithEngine, toEngine chan<- block.Message) *linearizeOnInitializeVM {
	return &linearizeOnInitializeVM{
		LinearizableVMWithEngine: vm,
		toEngine:                 toEngine,
	}
}

func (vm *linearizeOnInitializeVM) Initialize(
	ctx context.Context,
	_ *consensusctx.Context,
	_ database.Database,
	_ []byte,
	_ []byte,
	_ []byte,
	_ []*consensus.Fx,
	_ appsender.AppSender,
) error {
	return vm.Linearize(ctx, vm.stopVertexID, vm.toEngine)
}
