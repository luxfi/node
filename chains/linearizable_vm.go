// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"
	"sync"

	consensusvertex "github.com/luxfi/consensus/engine/vertex"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/runtime"
	vmcore "github.com/luxfi/vm"
	"github.com/luxfi/vm/chain"
	"github.com/luxfi/warp"
)

var (
	_ consensusvertex.LinearizableVM = (*initializeOnLinearizeVM)(nil)
	// Note: linearizeOnInitializeVM doesn't need to fully implement chain.ChainVM
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
	vmToInitialize chain.ChainVM
	vmToLinearize  *linearizeOnInitializeVM

	rt               *runtime.Runtime
	db               database.Database
	genesisBytes     []byte
	upgradeBytes     []byte
	configBytes      []byte
	fxs              []fx.Fx
	appSender        warp.Sender
	toEngine         chan<- vmcore.Message // Channel to notify consensus engine
	waitForLinearize chan struct{}
	linearizeOnce    sync.Once
}

func (vm *initializeOnLinearizeVM) WaitForEvent(ctx context.Context) (vmcore.Message, error) {
	select {
	case <-vm.waitForLinearize:
		return vm.vmToInitialize.WaitForEvent(ctx)
	case <-ctx.Done():
		return vmcore.Message{}, ctx.Err()
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

	// Pass the toEngine channel to the VM so it can notify consensus about pending transactions
	return vm.vmToInitialize.Initialize(
		ctx,
		vmcore.Init{
			Runtime:  vm.rt,
			DB:       vm.db,
			Genesis:  vm.genesisBytes,
			Upgrade:  vm.upgradeBytes,
			Config:   vm.configBytes,
			ToEngine: vm.toEngine,
			Fx:       fxsInterface,
			Sender:   vm.appSender,
		},
	)
}

// linearizeOnInitializeVM transforms the proposervm's call to Initialize into a
// call to Linearize. This enables the proposervm to provide its toEngine
// channel to the VM that is being linearized.
type linearizeOnInitializeVM struct {
	consensusvertex.LinearizableVMWithEngine
	stopVertexID ids.ID
	toEngine     chan<- vmcore.Message
}

func NewLinearizeOnInitializeVM(vm consensusvertex.LinearizableVMWithEngine, toEngine chan<- vmcore.Message) *linearizeOnInitializeVM {
	return &linearizeOnInitializeVM{
		LinearizableVMWithEngine: vm,
		toEngine:                 toEngine,
	}
}

func (vm *linearizeOnInitializeVM) Initialize(
	ctx context.Context,
	vmInit vmcore.Init,
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
