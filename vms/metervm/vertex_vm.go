// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/db"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/graph"
	"github.com/luxfi/node/consensus/engine/dag/vertex"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/utils/timer/mockable"
)

var (
	_ vertex.LinearizableVMWithEngine = (*vertexVM)(nil)
	_ graph.Tx                    = (*meterTx)(nil)
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
	chainCtx *consensus.Context,
	db database.Database,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	fxs []*core.Fx,
	appSender core.AppSender,
) error {
	if err := vm.vertexMetrics.Initialize(vm.registry); err != nil {
		return err
	}

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

func (vm *vertexVM) ParseTx(ctx context.Context, b []byte) (graph.Tx, error) {
	start := vm.clock.Time()
	tx, err := vm.LinearizableVMWithEngine.ParseTx(ctx, b)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.vertexMetrics.parseErr.Observe(duration)
		return nil, err
	}
	vm.vertexMetrics.parse.Observe(duration)
	return &meterTx{
		Tx: tx,
		vm: vm,
	}, nil
}

type meterTx struct {
	graph.Tx

	vm *vertexVM
}

func (mtx *meterTx) Verify(ctx context.Context) error {
	start := mtx.vm.clock.Time()
	err := mtx.Tx.Verify(ctx)
	end := mtx.vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		mtx.vm.vertexMetrics.verifyErr.Observe(duration)
	} else {
		mtx.vm.vertexMetrics.verify.Observe(duration)
	}
	return err
}

func (mtx *meterTx) Accept(ctx context.Context) error {
	start := mtx.vm.clock.Time()
	err := mtx.Tx.Accept(ctx)
	end := mtx.vm.clock.Time()
	mtx.vm.vertexMetrics.accept.Observe(float64(end.Sub(start)))
	return err
}

func (mtx *meterTx) Reject(ctx context.Context) error {
	start := mtx.vm.clock.Time()
	err := mtx.Tx.Reject(ctx)
	end := mtx.vm.clock.Time()
	mtx.vm.vertexMetrics.reject.Observe(float64(end.Sub(start)))
	return err
}
