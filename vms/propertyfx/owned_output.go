// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"github.com/luxdefi/luxd/vms/secp256k1fx"
)

type OwnedOutput struct {
	secp256k1fx.OutputOwners `serialize:"true"`
}
