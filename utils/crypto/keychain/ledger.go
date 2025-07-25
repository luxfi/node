// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/version"
)

// Ledger interface for the ledger wrapper
type Ledger interface {
	Version() (v *version.Semantic, err error)
	Address(displayHRP string, addressIndex uint32) (ids.ShortID, error)
	Addresses(addressIndices []uint32) ([]ids.ShortID, error)
	SignHash(hash []byte, addressIndices []uint32) ([][]byte, error)
	Sign(unsignedTxBytes []byte, addressIndices []uint32) ([][]byte, error)
	Disconnect() error
}
