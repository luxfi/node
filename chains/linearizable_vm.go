// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"
	"sync"

	db "github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/quasar/engine/chain/block"
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/quasar/engine/dag/vertex"
	"github.com/luxfi/node/v2/version"
)

var (
	_ vertex.LinearizableVM = (*initializeOnLinearizeVM)(nil)
	_ block.ChainVM         = (*linearizeOnInitializeVM)(nil)
	
	errNotImplemented = errors.New("not implemented")
)

// initializeOnLinearizeVM transforms the consensus engine's call to Linearize
// into a call to Initialize. This enables the proposervm to be initialized by
// the call to Linearize. This also provides the stopVertexID to the
// linearizeOnInitializeVM.
type initializeOnLinearizeVM struct {
	vertex.DAGVM
	vmToInitialize   block.ChainVM
	vmToLinearize    *linearizeOnInitializeVM

	ctx              *quasar.Context
	db               db.Database
	genesisBytes     []byte
	upgradeBytes     []byte
	configBytes      []byte
	fxs              []*core.Fx
	appSender        core.AppSender
	waitForLinearize chan struct{}
	linearizeOnce    sync.Once
}

func (vm *initializeOnLinearizeVM) WaitForEvent(ctx context.Context) (core.Message, error) {
	select {
	case <-vm.waitForLinearize:
		return vm.vmToInitialize.WaitForEvent(ctx)
	case <-ctx.Done():
		return core.Message{}, ctx.Err()
	}
}

func (vm *initializeOnLinearizeVM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	vm.vmToLinearize.stopVertexID = stopVertexID
	defer vm.linearizeOnce.Do(func() {
		close(vm.waitForLinearize)
	})
	
	return vm.vmToInitialize.Initialize(
		ctx,
		vm.ctx,
		vm.db,
		vm.genesisBytes,
		vm.upgradeBytes,
		vm.configBytes,
		vm.fxs,
		vm.appSender,
	)
}

// linearizeOnInitializeVM transforms the proposervm's call to Initialize into a
// call to Linearize. This enables the proposervm to provide its toEngine
// channel to the VM that is being linearized.
type linearizeOnInitializeVM struct {
	vertex.LinearizableVMWithEngine
	vertex.LinearizableVMWithLinearize
	stopVertexID ids.ID
}

func NewLinearizeOnInitializeVM(vm interface{}) *linearizeOnInitializeVM {
	result := &linearizeOnInitializeVM{}
	
	// Set the appropriate interfaces if vm implements them
	if engineVM, ok := vm.(vertex.LinearizableVMWithEngine); ok {
		result.LinearizableVMWithEngine = engineVM
	}
	if linearizeVM, ok := vm.(vertex.LinearizableVMWithLinearize); ok {
		result.LinearizableVMWithLinearize = linearizeVM
	}
	
	return result
}

func (vm *linearizeOnInitializeVM) Initialize(
	ctx context.Context,
	_ *quasar.Context,
	_ db.Database,
	_ []byte,
	_ []byte,
	_ []byte,
	_ []*core.Fx,
	_ core.AppSender,
) error {
	if vm.LinearizableVMWithLinearize != nil {
		return vm.Linearize(ctx, vm.stopVertexID)
	}
	return errNotImplemented
}

// Implement missing block.ChainVM methods
func (vm *linearizeOnInitializeVM) BuildBlock(ctx context.Context) (block.Block, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	// For now, return an error indicating this method should not be called
	return nil, errNotImplemented
}

func (vm *linearizeOnInitializeVM) ParseBlock(ctx context.Context, blockBytes []byte) (block.Block, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil, errNotImplemented
}

func (vm *linearizeOnInitializeVM) GetBlock(ctx context.Context, blockID ids.ID) (block.Block, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil, errNotImplemented
}

func (vm *linearizeOnInitializeVM) SetState(ctx context.Context, state quasar.State) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) Shutdown(ctx context.Context) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) Version(ctx context.Context) (string, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return "", nil
}

func (vm *linearizeOnInitializeVM) CreateHandlers(ctx context.Context) (map[string]interface{}, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil, nil
}

func (vm *linearizeOnInitializeVM) CreateStaticHandlers(ctx context.Context) (map[string]interface{}, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil, nil
}

func (vm *linearizeOnInitializeVM) HealthCheck(ctx context.Context) (interface{}, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil, nil
}

func (vm *linearizeOnInitializeVM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request []byte) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, request []byte) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return ids.Empty, nil
}

func (vm *linearizeOnInitializeVM) VerifyHeightIndex(ctx context.Context) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}

func (vm *linearizeOnInitializeVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return ids.Empty, nil
}

func (vm *linearizeOnInitializeVM) WaitForEvent(ctx context.Context) (core.Message, error) {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return core.Message{}, nil
}

func (vm *linearizeOnInitializeVM) SetPreference(ctx context.Context, blockID ids.ID) error {
	// This should be implemented by the underlying LinearizableVMWithEngine
	return nil
}