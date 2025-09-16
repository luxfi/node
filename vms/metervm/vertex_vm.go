// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"net/http"

	"github.com/luxfi/metric"

	"github.com/luxfi/consensus/engine/dag"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/timer/mockable"
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
}

var (
	_ LinearizableVMWithEngine = (*vertexVM)(nil)
	_ dag.Tx                   = (*meterTx)(nil)
)

func NewVertexVM(
	vm LinearizableVMWithEngine,
	reg metric.Registerer,
) LinearizableVMWithEngine {
	return &vertexVM{
		LinearizableVMWithEngine: vm,
		registry:                 reg,
	}
}

type vertexVM struct {
	LinearizableVMWithEngine
	vertexMetrics
	registry metric.Registerer
	clock    mockable.Clock
}

func (vm *vertexVM) Initialize(
	ctx context.Context,
	chainCtx interface{},
	dbManager interface{},
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	msgChan chan<- interface{},
	fxs []interface{},
	appSender interface{},
) error {
	if err := vm.vertexMetrics.Initialize(vm.registry); err != nil {
		return err
	}

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

func (vm *vertexVM) ParseTx(ctx context.Context, b []byte) (dag.Tx, error) {
	start := vm.clock.Time()
	tx, err := vm.LinearizableVMWithEngine.ParseTx(ctx, b)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.vertexMetrics.parseErr.Observe(duration)
		return nil, err
	}
	vm.vertexMetrics.parse.Observe(duration)

	// Wrap it with meterTx
	return &meterTx{
		Tx: tx,
		vm: vm,
	}, nil
}

type meterTx struct {
	dag.Tx

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
