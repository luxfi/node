// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/secp256k1fx"
)

type BurnOperation struct {
	secp256k1fx.Input `serialize:"true"`
}

func (*BurnOperation) InitCtx(*quasar.Context) {}

// Initialize implements quasar.ContextInitializable
func (op *BurnOperation) Initialize(ctx *quasar.Context) error {
	op.InitCtx(ctx)
	return nil
}

func (*BurnOperation) Outs() []verify.State {
	return nil
}
