// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"context"
	"github.com/luxfi/consensus"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var _ verify.State = (*MintOutput)(nil)

type MintOutput struct {
	verify.IsState `json:"-"`

	secp256k1fx.OutputOwners `serialize:"true"`
}

// InitializeWithContext implements context.ContextInitializable
func (out *MintOutput) InitializeWithContext(ctx context.Context, chainCtx context.Context) error {
	return out.OutputOwners.InitializeWithContext(ctx, chainCtx)
}
