// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/engine/core"
	"github.com/luxfi/consensus/engine/graph/vertex"
	"github.com/luxfi/consensus/engine/chain/block"
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
	vertex.GRAPHVM
	vmToInitialize core.VM
	vmToLinearize  *linearizeOnInitializeVM

	ctx          *consensus.Context
	db           database.Database
	genesisBytes []byte
	upgradeBytes []byte
	configBytes  []byte
	fxs          []*core.Fx
	appSender    core.AppSender
}

func (vm *initializeOnLinearizeVM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	vm.vmToLinearize.stopVertexID = stopVertexID
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
	stopVertexID ids.ID
}

func NewLinearizeOnInitializeVM(vm vertex.LinearizableVMWithEngine) *linearizeOnInitializeVM {
	return &linearizeOnInitializeVM{
		LinearizableVMWithEngine: vm,
	}
}

func (vm *linearizeOnInitializeVM) Initialize(
	ctx context.Context,
	_ *consensus.Context,
	_ database.Database,
	_ []byte,
	_ []byte,
	_ []byte,
	_ []*core.Fx,
	_ core.AppSender,
) error {
	return vm.Linearize(ctx, vm.stopVertexID)
}
