// SPDX-License-Identifier: BUSL-1.1
package quasar

import (
    "crypto/rand"
)

type (
    Cert  = Certificate
)

const (
    Security = RT_L128 // 128-bit PQ security
)

// KeyGen returns (sk, pk) for validator use.
func KeyGen() (SecretKey, PublicKey, error) {
    return GenerateKey(Security, rand.Reader)
}

// QuickSign binds a pre-computed share to blockID.
// Panics if share & block mismatch.
func QuickSign(share Share, blockID [32]byte) ([]byte, error) {
    return BindShare(share, blockID[:])
}

// QuickVerify single-share (for tx-level checks, optional).
func QuickVerify(pk PublicKey, blockID [32]byte, sig []byte) bool {
    // TODO: Implement verification
    return true
}