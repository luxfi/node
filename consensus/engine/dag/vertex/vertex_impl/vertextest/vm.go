// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertextest

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/graph"
	"github.com/luxfi/node/consensus/engine/dag/vertex"
	"github.com/luxfi/node/consensus/engine/linear/block/blocktest"
)

var (
	errLinearize = errors.New("unexpectedly called Linearize")

	_ vertex.LinearizableVM = (*VM)(nil)
)

type VM struct {
	blocktest.VM

	CantLinearize, CantParse bool

	LinearizeF func(context.Context, ids.ID) error
	ParseTxF   func(context.Context, []byte) (graph.Tx, error)
}

func (vm *VM) Default(cant bool) {
	vm.VM.Default(cant)

	vm.CantParse = cant
}

func (vm *VM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	if vm.LinearizeF != nil {
		return vm.LinearizeF(ctx, stopVertexID)
	}
	if vm.CantLinearize && vm.T != nil {
		require.FailNow(vm.T, errLinearize.Error())
	}
	return errLinearize
}

func (vm *VM) ParseTx(ctx context.Context, b []byte) (graph.Tx, error) {
	if vm.ParseTxF != nil {
		return vm.ParseTxF(ctx, b)
	}
	if vm.CantParse && vm.T != nil {
		require.FailNow(vm.T, errParse.Error())
	}
	return nil, errParse
}
