// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"github.com/luxfi/ids"
	ledgerkeychain "github.com/luxfi/ledger-lux-go/keychain"
)

// LedgerKeychain adapts a ledger-lux-go/keychain.Keychain to implement the Keychain interface
type LedgerKeychain struct {
	kc ledgerkeychain.Keychain
}

// Ensure we implement the interface
var _ Keychain = (*LedgerKeychain)(nil)

// NewLedgerKeychain creates a new adapter from ledger-lux-go/keychain.Keychain
func NewLedgerKeychain(kc ledgerkeychain.Keychain) Keychain {
	return &LedgerKeychain{kc: kc}
}

func (l *LedgerKeychain) Addresses() []ids.ShortID {
	// Convert the Set[ids.ShortID] to []ids.ShortID
	addrSet := l.kc.Addresses()
	addrs := make([]ids.ShortID, 0, len(addrSet))
	for addr := range addrSet {
		addrs = append(addrs, addr)
	}
	return addrs
}

func (l *LedgerKeychain) Get(addr ids.ShortID) (Signer, bool) {
	signer, found := l.kc.Get(addr)
	if !found {
		return nil, false
	}
	return &ledgerSignerAdapter{signer: signer}, true
}

// ledgerSignerAdapter adapts the ledger signer to our Signer interface
type ledgerSignerAdapter struct {
	signer ledgerkeychain.Signer
}

func (s *ledgerSignerAdapter) SignHash(hash []byte) ([]byte, error) {
	return s.signer.SignHash(hash)
}

func (s *ledgerSignerAdapter) Sign(msg []byte) ([]byte, error) {
	return s.signer.Sign(msg)
}

func (s *ledgerSignerAdapter) Address() ids.ShortID {
	return s.signer.Address()
}