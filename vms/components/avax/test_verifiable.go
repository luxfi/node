// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import "github.com/ava-labs/avalanchego/snow"

type TestVerifiable struct{ Err error }

<<<<<<< HEAD
<<<<<<< HEAD
func (*TestVerifiable) InitCtx(*snow.Context) {}

func (v *TestVerifiable) Verify() error {
	return v.Err
}

func (v *TestVerifiable) VerifyState() error {
	return v.Err
}
=======
func (*TestVerifiable) InitCtx(ctx *snow.Context) {}
func (v *TestVerifiable) Verify() error           { return v.Err }
func (v *TestVerifiable) VerifyState() error      { return v.Err }
>>>>>>> 707ffe48f (Add UnusedReceiver linter (#2224))
=======
func (*TestVerifiable) InitCtx(*snow.Context) {}
func (v *TestVerifiable) Verify() error       { return v.Err }
func (v *TestVerifiable) VerifyState() error  { return v.Err }
>>>>>>> 3a7ebb1da (Add UnusedParameter linter (#2226))

type TestTransferable struct {
	TestVerifiable

	Val uint64 `serialize:"true"`
}

func (*TestTransferable) InitCtx(*snow.Context) {}
<<<<<<< HEAD

func (t *TestTransferable) Amount() uint64 {
	return t.Val
}

func (*TestTransferable) Cost() (uint64, error) {
	return 0, nil
}
=======
func (t *TestTransferable) Amount() uint64      { return t.Val }
func (*TestTransferable) Cost() (uint64, error) { return 0, nil }
>>>>>>> 707ffe48f (Add UnusedReceiver linter (#2224))

type TestAddressable struct {
	TestTransferable `serialize:"true"`

	Addrs [][]byte `serialize:"true"`
}

func (a *TestAddressable) Addresses() [][]byte {
	return a.Addrs
}
