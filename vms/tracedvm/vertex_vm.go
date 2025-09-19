// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/attribute"

	"github.com/luxfi/consensus/engine/dag"
	"github.com/luxfi/ids"
	"github.com/luxfi/trace"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// LinearizableVMWithEngine defines a VM that can be linearized with an engine
type LinearizableVMWithEngine interface {
	Initialize(
		ctx context.Context,
		chainCtx interface{},
		dbManager interface{},
		genesisBytes []byte,
		upgradeBytes []byte,
		configBytes []byte,
		msgChan chan<- interface{},
		fxs []interface{},
		appSender interface{},
	) error
	Shutdown() error
	CreateHandlers(ctx context.Context) (map[string]http.Handler, error)
	HealthCheck(ctx context.Context) (interface{}, error)
	ParseTx(ctx context.Context, txBytes []byte) (dag.Tx, error)
	GetTx(ctx context.Context, txID ids.ID) (dag.Tx, error)
	PendingTxs(ctx context.Context) []dag.Tx
}

var _ LinearizableVMWithEngine = (*vertexVM)(nil)

type vertexVM struct {
	LinearizableVMWithEngine
	tracer trace.Tracer
}

func NewVertexVM(vm LinearizableVMWithEngine, tracer trace.Tracer) LinearizableVMWithEngine {
	return &vertexVM{
		LinearizableVMWithEngine: vm,
		tracer:                   tracer,
	}
}

// Initialize is not overridden - use the embedded implementation directly

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

func (vm *vertexVM) GetTx(ctx context.Context, txID ids.ID) (dag.Tx, error) {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.GetTx", oteltrace.WithAttributes(
		attribute.Stringer("txID", txID),
	))
	defer span.End()

	tx, err := vm.LinearizableVMWithEngine.GetTx(ctx, txID)
	if err != nil {
		return nil, err
	}

	// Wrap it with tracedTx
	return &tracedTx{
		Tx:     tx,
		tracer: vm.tracer,
	}, nil
}

func (vm *vertexVM) PendingTxs(ctx context.Context) []dag.Tx {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.PendingTxs")
	defer span.End()

	txs := vm.LinearizableVMWithEngine.PendingTxs(ctx)

	// Wrap all transactions
	wrappedTxs := make([]dag.Tx, len(txs))
	for i, tx := range txs {
		wrappedTxs[i] = &tracedTx{
			Tx:     tx,
			tracer: vm.tracer,
		}
	}

	return wrappedTxs
}
