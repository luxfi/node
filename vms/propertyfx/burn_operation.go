// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type BurnOperation struct {
	secp256k1fx.Input `serialize:"true"`
}

<<<<<<< HEAD
<<<<<<< HEAD
func (*BurnOperation) InitCtx(*snow.Context) {}

func (*BurnOperation) Outs() []verify.State {
	return nil
}
=======
func (*BurnOperation) InitCtx(ctx *snow.Context) {}

func (*BurnOperation) Outs() []verify.State { return nil }
>>>>>>> 707ffe48f (Add UnusedReceiver linter (#2224))
=======
func (*BurnOperation) InitCtx(*snow.Context) {}
<<<<<<< HEAD
func (*BurnOperation) Outs() []verify.State  { return nil }
>>>>>>> 3a7ebb1da (Add UnusedParameter linter (#2226))
=======

func (*BurnOperation) Outs() []verify.State {
	return nil
}
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
