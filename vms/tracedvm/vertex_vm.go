// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/luxfi/consensus/engine/dag"
	"github.com/luxfi/consensus/engine/vertex"
	"github.com/luxfi/ids"
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

// Initialize is not overridden - use the embedded implementation directly

func (vm *vertexVM) ParseTx(ctx context.Context, txBytes []byte) (dag.Transaction, error) {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.ParseTx", oteltrace.WithAttributes(
		attribute.Int("txLen", len(txBytes)),
	))
	defer span.End()

	tx, err := vm.LinearizableVMWithEngine.ParseTx(ctx, txBytes)
	if err != nil {
		return nil, err
	}

	// Wrap it with tracedTransaction
	return &tracedTransaction{
		Transaction: tx,
		tracer:      vm.tracer,
	}, nil
}

func (vm *vertexVM) GetTx(ctx context.Context, txID ids.ID) (dag.Transaction, error) {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.GetTx", oteltrace.WithAttributes(
		attribute.Stringer("txID", txID),
	))
	defer span.End()

	tx, err := vm.LinearizableVMWithEngine.GetTx(ctx, txID)
	if err != nil {
		return nil, err
	}

	// Wrap it with tracedTransaction
	return &tracedTransaction{
		Transaction: tx,
		tracer:      vm.tracer,
	}, nil
}

func (vm *vertexVM) PendingTxs(ctx context.Context) []dag.Transaction {
	ctx, span := vm.tracer.Start(ctx, "vertexVM.PendingTxs")
	defer span.End()

	txs := vm.LinearizableVMWithEngine.PendingTxs(ctx)
	
	// Wrap all transactions
	wrappedTxs := make([]dag.Transaction, len(txs))
	for i, tx := range txs {
		wrappedTxs[i] = &tracedTransaction{
			Transaction: tx,
			tracer:      vm.tracer,
		}
	}
	
	return wrappedTxs
}
