// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"github.com/luxfi/geth/common"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/secp256k1fx"
)

// Secp256k1fxKeychain adapts a secp256k1fx.Keychain to implement the Keychain interface
type Secp256k1fxKeychain struct {
	kc *secp256k1fx.Keychain
}

// Ensure we implement the interface
var _ Keychain = (*Secp256k1fxKeychain)(nil)

// NewSecp256k1fxKeychain creates a new adapter from secp256k1fx.Keychain
func NewSecp256k1fxKeychain(kc *secp256k1fx.Keychain) *Secp256k1fxKeychain {
	return &Secp256k1fxKeychain{kc: kc}
}

func (a *Secp256k1fxKeychain) Addresses() []ids.ShortID {
	// Now that Addresses() returns []ids.ShortID directly, we can just return it
	return a.kc.Addresses()
}

func (a *Secp256k1fxKeychain) Get(addr ids.ShortID) (Signer, bool) {
	signer, found := a.kc.Get(addr)
	if !found {
		return nil, false
	}
	return &signerAdapter{signer: signer}, true
}

// signerAdapter adapts the secp256k1fx signer to our Signer interface
type signerAdapter struct {
	signer interface {
		SignHash([]byte) ([]byte, error)
		Sign([]byte) ([]byte, error)
		Address() ids.ShortID
	}
}

func (s *signerAdapter) SignHash(hash []byte) ([]byte, error) {
	return s.signer.SignHash(hash)
}

func (s *signerAdapter) Sign(msg []byte) ([]byte, error) {
	return s.signer.Sign(msg)
}

func (s *signerAdapter) Address() ids.ShortID {
	return s.signer.Address()
}

// GetEth implements c.EthKeychain
func (a *Secp256k1fxKeychain) GetEth(addr common.Address) (Signer, bool) {
	signer, found := a.kc.GetEth(addr)
	if !found {
		return nil, false
	}
	return &signerAdapter{signer: signer}, true
}

// EthAddresses implements c.EthKeychain
func (a *Secp256k1fxKeychain) EthAddresses() set.Set[common.Address] {
	return a.kc.EthAddresses()
}