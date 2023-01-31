<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
=======
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
>>>>>>> 34554f662 (Update LICENSE)
>>>>>>> c5eafdb72 (Update LICENSE)
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 8fb2bec88 (Must keep bloodline pure)
// See the file LICENSE for licensing terms.

package vertex

import (
<<<<<<< HEAD
	"errors"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/consensus/snowstorm"
	"github.com/luxdefi/node/snow/engine/common"
)

var (
	errPending = errors.New("unexpectedly called Pending")
=======
	"context"
	"errors"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/consensus/snowstorm"
	"github.com/luxdefi/node/snow/engine/snowman/block"
)

var (
	errPending   = errors.New("unexpectedly called Pending")
	errLinearize = errors.New("unexpectedly called Linearize")
>>>>>>> 53a8245a8 (Update consensus)

	_ DAGVM = (*TestVM)(nil)
)

type TestVM struct {
<<<<<<< HEAD
	common.TestVM

	CantPendingTxs, CantParse, CantGet bool

	PendingTxsF func() []snowstorm.Tx
	ParseTxF    func([]byte) (snowstorm.Tx, error)
	GetTxF      func(ids.ID) (snowstorm.Tx, error)
=======
	block.TestVM

	CantLinearize, CantPendingTxs, CantParse, CantGet bool

<<<<<<< HEAD
<<<<<<< HEAD
	LinearizeF  func(context.Context, ids.ID) error
=======
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
	LinearizeF  func(context.Context, ids.ID) error
>>>>>>> db5704fcd (Update DAGVM interface to support linearization (#2442))
	PendingTxsF func(context.Context) []snowstorm.Tx
	ParseTxF    func(context.Context, []byte) (snowstorm.Tx, error)
	GetTxF      func(context.Context, ids.ID) (snowstorm.Tx, error)
>>>>>>> 53a8245a8 (Update consensus)
}

func (vm *TestVM) Default(cant bool) {
	vm.TestVM.Default(cant)

	vm.CantPendingTxs = cant
	vm.CantParse = cant
	vm.CantGet = cant
}

<<<<<<< HEAD
func (vm *TestVM) PendingTxs() []snowstorm.Tx {
	if vm.PendingTxsF != nil {
		return vm.PendingTxsF()
=======
<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> db5704fcd (Update DAGVM interface to support linearization (#2442))
func (vm *TestVM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	if vm.LinearizeF != nil {
		return vm.LinearizeF(ctx, stopVertexID)
	}
	if vm.CantLinearize && vm.T != nil {
		vm.T.Fatal(errLinearize)
	}
	return errLinearize
}

<<<<<<< HEAD
=======
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
>>>>>>> db5704fcd (Update DAGVM interface to support linearization (#2442))
func (vm *TestVM) PendingTxs(ctx context.Context) []snowstorm.Tx {
	if vm.PendingTxsF != nil {
		return vm.PendingTxsF(ctx)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if vm.CantPendingTxs && vm.T != nil {
		vm.T.Fatal(errPending)
	}
	return nil
}

<<<<<<< HEAD
func (vm *TestVM) ParseTx(b []byte) (snowstorm.Tx, error) {
	if vm.ParseTxF != nil {
		return vm.ParseTxF(b)
=======
func (vm *TestVM) ParseTx(ctx context.Context, b []byte) (snowstorm.Tx, error) {
	if vm.ParseTxF != nil {
		return vm.ParseTxF(ctx, b)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if vm.CantParse && vm.T != nil {
		vm.T.Fatal(errParse)
	}
	return nil, errParse
}

<<<<<<< HEAD
func (vm *TestVM) GetTx(txID ids.ID) (snowstorm.Tx, error) {
	if vm.GetTxF != nil {
		return vm.GetTxF(txID)
=======
func (vm *TestVM) GetTx(ctx context.Context, txID ids.ID) (snowstorm.Tx, error) {
	if vm.GetTxF != nil {
		return vm.GetTxF(ctx, txID)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if vm.CantGet && vm.T != nil {
		vm.T.Fatal(errGet)
	}
	return nil, errGet
}
