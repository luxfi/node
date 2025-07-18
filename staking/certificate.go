// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import "crypto"

type Certificate struct {
	Raw       []byte
	PublicKey crypto.PublicKey
}
