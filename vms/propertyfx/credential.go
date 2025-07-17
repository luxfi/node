// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import "github.com/luxfi/node/vms/secp256k1fx"

type Credential struct {
	secp256k1fx.Credential `serialize:"true"`
}
