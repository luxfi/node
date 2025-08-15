// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/luxfi/consensus/engine/dag"
	"github.com/luxfi/consensus/engine/dag/vertex"
	"github.com/luxfi/trace"

	oteltrace "go.opentelemetry.io/otel/trace"
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
	chainCtx interface{},
	dbManager interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	msgChan chan<- interface{},
	fxs []interface{},
	appSender interface{},
) error {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.Initialize")
	defer span.End()

	return vm.LinearizableVMWithEngine.Initialize(
		ctx,
		chainCtx,
		dbManager,
		genesisBytes,
		upgradeBytes,
		configBytes,
		msgChan,
		fxs,
		appSender,
	)
}

func (vm *vertexVM) ParseTx(ctx context.Context, txBytes []byte) (dag.Tx, error) {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.ParseTx", oteltrace.WithAttributes(
		attribute.Int("txLen", len(txBytes)),
	))
	defer span.End()

	tx, err := vm.LinearizableVMWithEngine.ParseTx(ctx, txBytes)
	if err != nil {
		return nil, err
	}
	
	// Wrap it with tracedTx
	return &tracedTx{
		Tx:     tx,
		tracer: vm.tracer,
	}, nil
}
