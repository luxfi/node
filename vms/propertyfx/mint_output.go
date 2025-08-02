// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"github.com/luxfi/node/v2/vms/components/verify"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
)

var _ verify.State = (*MintOutput)(nil)

type MintOutput struct {
	verify.IsState `json:"-"`

	secp256k1fx.OutputOwners `serialize:"true"`
}

func (out *MintOutput) InitCtx(ctx interface{}) {
	// No initialization needed
}

func (out *MintOutput) Initialize(ctx interface{}) error {
	return nil
}
