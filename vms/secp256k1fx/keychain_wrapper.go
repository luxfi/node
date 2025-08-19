// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/ledger-lux-go/keychain"
)

// Ensure Keychain implements keychain.Keychain interface
var _ keychain.Keychain = (*keychainWrapper)(nil)

// keychainWrapper wraps a Keychain to implement the ledger keychain interface
type keychainWrapper struct {
	*Keychain
}

// Get implements keychain.Keychain
func (kw *keychainWrapper) Get(addr ids.ShortID) (keychain.Signer, bool) {
	return kw.Keychain.Get(addr)
}

// Addresses implements keychain.Keychain
func (kw *keychainWrapper) Addresses() keychain.Set[ids.ShortID] {
	addrs := make(keychain.Set[ids.ShortID], kw.Keychain.Addrs.Len())
	for addr := range kw.Keychain.Addrs {
		addrs[addr] = struct{}{}
	}
	return addrs
}

// WrapKeychain wraps a Keychain to implement the ledger keychain interface
func WrapKeychain(kc *Keychain) keychain.Keychain {
	return &keychainWrapper{Keychain: kc}
}