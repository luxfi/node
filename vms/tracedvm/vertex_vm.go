// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/luxfi/consensus/engine/graph/vertex"
	"github.com/luxfi/consensus/graph"
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
	db interface{},
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	toEngine chan<- interface{},
	fxs []interface{},
	appSender interface{},
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

func (vm *vertexVM) ParseTx(ctx context.Context, txBytes []byte) (interface{}, error) {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.ParseTx", oteltrace.WithAttributes(
		attribute.Int("txLen", len(txBytes)),
	))
	defer span.End()

	tx, err := vm.LinearizableVMWithEngine.ParseTx(ctx, txBytes)
	if err != nil {
		return nil, err
	}
	
	// If the tx implements graph.Tx, wrap it with tracedTx
	if graphTx, ok := tx.(graph.Tx); ok {
		return &tracedTx{
			Tx:     graphTx,
			tracer: vm.tracer,
		}, nil
	}
	
	// Otherwise return as-is
	return tx, nil
}
