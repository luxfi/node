// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !ledger
// +build !ledger

package ledger

import (
	"fmt"

	"github.com/luxfi/node/utils/crypto/keychain"
)

func New() (keychain.Ledger, error) {
	return nil, fmt.Errorf("ledger support is not available in this build")
}