// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"errors"

	"github.com/luxfi/consensus"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var errNilMintOperation = errors.New("nil mint operation")

type MintOperation struct {
	MintInput   secp256k1fx.Input `serialize:"true" json:"mintInput"`
	MintOutput  MintOutput        `serialize:"true" json:"mintOutput"`
	OwnedOutput OwnedOutput       `serialize:"true" json:"ownedOutput"`
}

func (op *MintOperation) InitCtx(ctx context.Context) {
	op.MintOutput.OutputOwners.InitCtx(ctx)
	op.OwnedOutput.OutputOwners.InitCtx(ctx)
}

func (op *MintOperation) Cost() (uint64, error) {
	return op.MintInput.Cost()
}

func (op *MintOperation) Outs() []verify.State {
	return []verify.State{
		&op.MintOutput,
		&op.OwnedOutput,
	}
}

func (op *MintOperation) Verify() error {
	switch {
	case op == nil:
		return errNilMintOperation
	default:
		return verify.All(&op.MintInput, &op.MintOutput, &op.OwnedOutput)
	}
}
