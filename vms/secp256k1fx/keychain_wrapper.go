// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"github.com/luxfi/ids"
	ledgerkeychain "github.com/luxfi/ledger-lux-go/keychain"
	nodekeychain "github.com/luxfi/node/utils/crypto/keychain"
	"github.com/luxfi/node/utils/set"
)

// Ensure Keychain implements ledgerkeychain.Keychain interface
var _ ledgerkeychain.Keychain = (*keychainWrapper)(nil)

// keychainWrapper wraps a Keychain to implement the ledger keychain interface
type keychainWrapper struct {
	*Keychain
}

// Get implements ledgerkeychain.Keychain
func (kw *keychainWrapper) Get(addr ids.ShortID) (ledgerkeychain.Signer, bool) {
	return kw.Keychain.Get(addr)
}

// Addresses implements ledgerkeychain.Keychain
func (kw *keychainWrapper) Addresses() []ids.ShortID {
	addrs := make([]ids.ShortID, 0, kw.Keychain.Addrs.Len())
	for addr := range kw.Keychain.Addrs {
		addrs = append(addrs, addr)
	}
	return addrs
}

// WrapKeychain wraps a Keychain to implement the ledger keychain interface
func WrapKeychain(kc *Keychain) ledgerkeychain.Keychain {
	return &keychainWrapper{Keychain: kc}
}

// nodeKeychainWrapper wraps a Keychain to implement the node's keychain interface
type nodeKeychainWrapper struct {
	*Keychain
}

// Get implements node's keychain.Keychain
func (nkw *nodeKeychainWrapper) Get(addr ids.ShortID) (nodekeychain.Signer, bool) {
	return nkw.Keychain.Get(addr)
}

// Addresses implements node's keychain.Keychain, returning a set
func (nkw *nodeKeychainWrapper) Addresses() set.Set[ids.ShortID] {
	return nkw.Keychain.Addrs
}

// WrapKeychainForNode wraps a Keychain to implement the node's keychain interface
func WrapKeychainForNode(kc *Keychain) nodekeychain.Keychain {
	return &nodeKeychainWrapper{Keychain: kc}
}