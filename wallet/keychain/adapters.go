// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"github.com/luxfi/geth/common"
	"github.com/luxfi/ids"
	ledgerkeychain "github.com/luxfi/ledger-lux-go/keychain"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/secp256k1fx"
)

// LedgerAdapter adapts a secp256k1fx.Keychain to wallet keychain interfaces
type LedgerAdapter struct {
	kc *secp256k1fx.Keychain
}

// NewLedgerAdapter creates a new adapter for secp256k1fx.Keychain
func NewLedgerAdapter(kc *secp256k1fx.Keychain) *LedgerAdapter {
	return &LedgerAdapter{kc: kc}
}

// Get returns the signer for the given address
func (a *LedgerAdapter) Get(addr ids.ShortID) (Signer, bool) {
	ledgerSigner, exists := a.kc.Get(addr)
	if !exists {
		return nil, false
	}
	return &ledgerSignerAdapter{signer: ledgerSigner}, true
}

// GetEth returns the signer for the given Ethereum address
func (a *LedgerAdapter) GetEth(addr common.Address) (Signer, bool) {
	ledgerSigner, exists := a.kc.GetEth(addr)
	if !exists {
		return nil, false
	}
	return &ledgerSignerAdapter{signer: ledgerSigner}, true
}

// Addresses returns all addresses in the keychain
func (a *LedgerAdapter) Addresses() []ids.ShortID {
	return a.kc.Addresses()
}

// EthAddresses returns all Ethereum addresses in the keychain
func (a *LedgerAdapter) EthAddresses() set.Set[common.Address] {
	return a.kc.EthAddresses()
}

// ledgerSignerAdapter adapts ledger-lux-go/keychain.Signer to wallet/keychain.Signer
type ledgerSignerAdapter struct {
	signer ledgerkeychain.Signer
}

// Sign signs a message
func (s *ledgerSignerAdapter) Sign(msg []byte) ([]byte, error) {
	return s.signer.Sign(msg)
}

// SignHash signs a hash
func (s *ledgerSignerAdapter) SignHash(hash []byte) ([]byte, error) {
	return s.signer.SignHash(hash)
}

// Address returns the address of the signer
func (s *ledgerSignerAdapter) Address() ids.ShortID {
	return s.signer.Address()
}