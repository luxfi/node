// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package localsigner

import (
	"github.com/luxfi/node/utils/crypto/bls"
)

// New creates a new local BLS signer
func New() (*bls.SecretKey, error) {
	return bls.NewSecretKey()
}