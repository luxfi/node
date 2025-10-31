// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	consensusctx "github.com/luxfi/consensus/context"

	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var _ verify.State = (*MintOutput)(nil)

type MintOutput struct {
	verify.IsState `json:"-"`

	secp256k1fx.OutputOwners `serialize:"true"`
}

// InitCtx implements consensus.ContextInitializable
func (out *MintOutput) InitCtx(ctx *consensusctx.Context) {
	out.OutputOwners.InitCtx(ctx)
}
