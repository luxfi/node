// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"context"
	"github.com/luxfi/consensus"
	"github.com/luxfi/node/vms/components/verify"
)

var _ verify.State = (*MintOutput)(nil)

type MintOutput struct {
	verify.IsState `json:"-"`

	OutputOwners `serialize:"true"`
}

// InitializeWithContext implements context.ContextInitializable
func (out *MintOutput) InitializeWithContext(ctx context.Context, chainCtx context.Context) error {
	return nil
}

func (out *MintOutput) Verify() error {
	switch {
	case out == nil:
		return ErrNilOutput
	default:
		return out.OutputOwners.Verify()
	}
}
