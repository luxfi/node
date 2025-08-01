// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"errors"

	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var errNilTransferOperation = errors.New("nil transfer operation")

type TransferOperation struct {
	Input  secp256k1fx.Input `serialize:"true" json:"input"`
	Output TransferOutput    `serialize:"true" json:"output"`
}

func (op *TransferOperation) InitCtx(ctx *quasar.Context) {
	op.Output.OutputOwners.InitCtx(ctx)
}

// Initialize implements quasar.ContextInitializable
func (op *TransferOperation) Initialize(ctx *quasar.Context) error {
	op.InitCtx(ctx)
	return nil
}

func (op *TransferOperation) Cost() (uint64, error) {
	return op.Input.Cost()
}

func (op *TransferOperation) Outs() []verify.State {
	return []verify.State{&op.Output}
}

func (op *TransferOperation) Verify() error {
	if op == nil {
		return errNilTransferOperation
	}

	return verify.All(&op.Input, &op.Output)
}
