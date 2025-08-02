// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"github.com/luxfi/node/quasar/engine/dag/vertex"
	"github.com/luxfi/trace"
)

var _ vertex.LinearizableVMWithEngine = (*vertexVM)(nil)

type vertexVM struct {
	vertex.LinearizableVMWithEngine
	tracer trace.Tracer
}

func NewVertexVM(vm vertex.LinearizableVMWithEngine, tracer trace.Tracer) vertex.LinearizableVMWithEngine {
	return &vertexVM{
		LinearizableVMWithEngine: vm,
		tracer:                   tracer,
	}
}

// TODO: Fix interface mismatch - LinearizableVMWithEngine doesn't have Initialize method
/*
func (vm *vertexVM) Initialize(
	ctx context.Context,
	chainCtx *quasar.Context,
	db db.Database,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	fxs []*core.Fx,
	appSender core.AppSender,
) error {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.Initialize")
	defer span.End()

	return vm.LinearizableVMWithEngine.Initialize(
		ctx,
		chainCtx,
		db,
		genesisBytes,
		upgradeBytes,
		configBytes,
		fxs,
		appSender,
	)
}
*/

// TODO: Fix interface mismatch - LinearizableVMWithEngine doesn't have ParseTx method
/*
func (vm *vertexVM) ParseTx(ctx context.Context, txBytes []byte) (graph.Tx, error) {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.ParseTx", oteltrace.WithAttributes(
		attribute.Int("txLen", len(txBytes)),
	))
	defer span.End()

	tx, err := vm.LinearizableVMWithEngine.ParseTx(ctx, txBytes)
	return &tracedTx{
		Tx:     tx,
		tracer: vm.tracer,
	}, err
}
*/
