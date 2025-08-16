// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"context"

	"github.com/luxfi/node/vms/components/verify"
)

var (
	_ verify.State    = (*TestState)(nil)
	_ TransferableOut = (*TestTransferable)(nil)
	_ Addressable     = (*TestAddressable)(nil)
)

type TestState struct {
	verify.IsState `json:"-"`

	Err error
}

func (*TestState) InitCtx(context.Context) {}

func (*TestState) InitializeWithContext(ctx context.Context) error {
	return nil
}

func (v *TestState) Verify() error {
	return v.Err
}

type TestTransferable struct {
	TestState

	Val uint64 `serialize:"true"`
}

func (*TestTransferable) InitCtx(context.Context) {}

func (*TestTransferable) InitializeWithContext(ctx context.Context) error {
	return nil
}

func (t *TestTransferable) Amount() uint64 {
	return t.Val
}

func (*TestTransferable) Cost() (uint64, error) {
	return 0, nil
}

type TestAddressable struct {
	TestTransferable `serialize:"true"`

	Addrs [][]byte `serialize:"true"`
}

func (a *TestAddressable) Addresses() [][]byte {
	return a.Addrs
}
