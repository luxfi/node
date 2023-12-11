// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"github.com/luxdefi/node/utils/crypto/bls"
	"github.com/luxdefi/node/vms/components/verify"
)

type Signer interface {
	verify.Verifiable

	// Key returns the public BLS key if it exists.
	// Note: [nil] will be returned if the key does not exist.
	// Invariant: Only called after [Verify] returns [nil].
	Key() *bls.PublicKey
}
