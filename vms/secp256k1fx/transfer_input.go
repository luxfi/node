// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/luxfi/node/chain"
)

var ErrNoValueInput = errors.New("input has no value")

type TransferInput struct {
	Amt   uint64 `serialize:"true" json:"amount"`
	Input `serialize:"true"`
}

func (*TransferInput) InitCtx(*consensus.Context) {}

// Amount returns the quantity of the asset this input produces
func (in *TransferInput) Amount() uint64 {
	return in.Amt
}

// Verify this input is syntactically valid
func (in *TransferInput) Verify() error {
	switch {
	case in == nil:
		return ErrNilInput
	case in.Amt == 0:
		return ErrNoValueInput
	default:
		return in.Input.Verify()
	}
}
