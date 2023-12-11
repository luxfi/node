// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"github.com/luxdefi/node/vms/components/verify"
	"github.com/luxdefi/node/vms/secp256k1fx"
)

var _ verify.State = (*OwnedOutput)(nil)

type OwnedOutput struct {
	verify.IsState `json:"-"`

	secp256k1fx.OutputOwners `serialize:"true"`
}
