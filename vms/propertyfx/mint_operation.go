// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"errors"

	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/vms/components/verify"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
)

var errNilMintOperation = errors.New("nil mint operation")

type MintOperation struct {
	MintInput   secp256k1fx.Input `serialize:"true" json:"mintInput"`
	MintOutput  MintOutput        `serialize:"true" json:"mintOutput"`
	OwnedOutput OwnedOutput       `serialize:"true" json:"ownedOutput"`
}

func (op *MintOperation) InitCtx(ctx *quasar.Context) {
	op.MintOutput.OutputOwners.InitCtx(ctx)
	op.OwnedOutput.OutputOwners.InitCtx(ctx)
}

// Initialize implements quasar.ContextInitializable
func (op *MintOperation) Initialize(ctx *quasar.Context) error {
	op.InitCtx(ctx)
	return nil
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
	if op == nil {
		return errNilMintOperation
	}

	return verify.All(&op.MintInput, &op.MintOutput, &op.OwnedOutput)
}
