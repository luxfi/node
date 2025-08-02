// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	db "github.com/luxfi/database"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/quasar/engine/dag/vertex"
	"github.com/luxfi/node/v2/quasar/graph"
	"github.com/luxfi/node/v2/utils/timer/mockable"
)

var (
	_ vertex.LinearizableVMWithEngine = (*vertexVM)(nil)
	_ graph.Tx                        = (*meterTx)(nil)
)

func NewVertexVM(
	vm vertex.LinearizableVMWithEngine,
	reg prometheus.Registerer,
) vertex.LinearizableVMWithEngine {
	return &vertexVM{
		LinearizableVMWithEngine: vm,
		registry:                 reg,
	}
}

type vertexVM struct {
	vertex.LinearizableVMWithEngine
	vertexMetrics
	registry prometheus.Registerer
	clock    mockable.Clock
}

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
	if err := vm.vertexMetrics.Initialize(vm.registry); err != nil {
		return err
	}

	// TODO: LinearizableVMWithEngine doesn't have Initialize method
	// return vm.LinearizableVMWithEngine.Initialize(
	// 	ctx,
	// 	chainCtx,
	// 	db,
	// 	genesisBytes,
	// 	upgradeBytes,
	// 	configBytes,
	// 	fxs,
	// 	appSender,
	// )
	return nil
}

func (vm *vertexVM) ParseTx(ctx context.Context, b []byte) (graph.Tx, error) {
	// TODO: LinearizableVMWithEngine doesn't have ParseTx method
	return nil, errors.New("ParseTx not implemented")
}

type meterTx struct {
	graph.Tx

	vm *vertexVM
}

func (mtx *meterTx) Verify(ctx context.Context) error {
	// TODO: graph.Tx doesn't have Verify method
	return nil
}

func (mtx *meterTx) Accept(ctx context.Context) error {
	// TODO: graph.Tx doesn't have Accept method
	return nil
}

func (mtx *meterTx) Reject(ctx context.Context) error {
	// TODO: graph.Tx doesn't have Reject method
	return nil
}
