// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/luxdefi/node/database/manager"
	"github.com/luxdefi/node/snow"
	"github.com/luxdefi/node/snow/consensus/snowstorm"
	"github.com/luxdefi/node/snow/engine/avalanche/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/trace"
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

func (vm *vertexVM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db manager.Manager,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
	appSender common.AppSender,
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
		toEngine,
		fxs,
		appSender,
	)
}

func (vm *vertexVM) ParseTx(ctx context.Context, txBytes []byte) (snowstorm.Tx, error) {
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
