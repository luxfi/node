// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import "github.com/luxfi/node/v2/vms/components/verify"

var _ verify.State = (*MintOutput)(nil)

type MintOutput struct {
	verify.IsState `json:"-"`

	OutputOwners `serialize:"true"`
}

func (out *MintOutput) InitCtx(ctx interface{}) {
	// No initialization needed
}

func (out *MintOutput) Initialize(ctx interface{}) error {
	// No initialization needed
	return nil
}

func (out *MintOutput) Verify() error {
	if out == nil {
		return ErrNilOutput
	}

	return out.OutputOwners.Verify()
}
