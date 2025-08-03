// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"github.com/luxfi/crypto"
)

// RecoverCache provides a cache for public key recovery operations
type RecoverCache struct {
	// In the future, this could include an LRU cache
	// For now, it's a simple pass-through
}

// NewRecoverCache creates a new recovery cache
func NewRecoverCache(size int) *RecoverCache {
	return &RecoverCache{}
}

// RecoverPublicKeyFromHash recovers the public key from a hash and signature
func (rc *RecoverCache) RecoverPublicKeyFromHash(hash, sig []byte) (*PublicKey, error) {
	// Use the crypto package's Ecrecover function
	pubkeyBytes, err := crypto.Ecrecover(hash, sig)
	if err != nil {
		return nil, err
	}
	
	// Return a wrapper that provides the Bytes() method
	return &PublicKey{bytes: pubkeyBytes}, nil
}

// PublicKey wraps the recovered public key bytes
type PublicKey struct {
	bytes []byte
}

// Bytes returns the public key bytes
func (pk *PublicKey) Bytes() []byte {
	return pk.bytes
}

// RecoverCacheType is an alias for compatibility
type RecoverCacheType = RecoverCache