// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"encoding/binary"

	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/runtime"
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

func (*TestState) InitRuntime(*runtime.Runtime) {}

func (v *TestState) Verify() error {
	return v.Err
}

type TestTransferable struct {
	TestState

	Val uint64 `serialize:"true"`
}

func (*TestTransferable) InitRuntime(*runtime.Runtime) {}

func (t *TestTransferable) Amount() uint64 {
	return t.Val
}

func (*TestTransferable) Cost() (uint64, error) {
	return 0, nil
}

// Bytes implements the utxo wire-serializable interface (utxo v0.3.7+)
// so the test primitive can be stored as a UTXO.Out. Test-only: returns
// a deterministic encoding of the amount, not a production wire format.
func (t *TestTransferable) Bytes() []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, t.Val)
	return b
}

type TestAddressable struct {
	TestTransferable `serialize:"true"`

	Addrs [][]byte `serialize:"true"`
}

func (a *TestAddressable) Addresses() [][]byte {
	return a.Addrs
}
