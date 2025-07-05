// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package vertex

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/consensus/snowstorm"
	"github.com/luxfi/node/snow/engine/snowman/block/blocktest"
)

var (
	errLinearize = errors.New("unexpectedly called Linearize")

	_ LinearizableVM = (*TestVM)(nil)
)

type TestVM struct {
	blocktest.VM

	CantLinearize, CantParse bool

	LinearizeF func(context.Context, ids.ID) error
	ParseTxF   func(context.Context, []byte) (snowstorm.Tx, error)
}

func (vm *TestVM) Default(cant bool) {
	vm.VM.Default(cant)

	vm.CantParse = cant
}

func (vm *TestVM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	if vm.LinearizeF != nil {
		return vm.LinearizeF(ctx, stopVertexID)
	}
	if vm.CantLinearize && vm.VM.T != nil {
		require.FailNow(vm.VM.T, errLinearize.Error())
	}
	return errLinearize
}

func (vm *TestVM) ParseTx(ctx context.Context, b []byte) (snowstorm.Tx, error) {
	if vm.ParseTxF != nil {
		return vm.ParseTxF(ctx, b)
	}
	if vm.CantParse && vm.VM.T != nil {
		require.FailNow(vm.VM.T, errParse.Error())
	}
	return nil, errParse
}
