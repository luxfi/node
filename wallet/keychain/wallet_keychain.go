// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"github.com/luxfi/geth/common"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/secp256k1fx"
)

// WalletKeychain provides both LUX and Eth keychain functionality for wallets
type WalletKeychain struct {
	kc *secp256k1fx.Keychain
}

// NewWalletKeychain creates a new wallet keychain from secp256k1fx.Keychain
func NewWalletKeychain(kc *secp256k1fx.Keychain) *WalletKeychain {
	return &WalletKeychain{kc: kc}
}

// Addresses implements Keychain
func (w *WalletKeychain) Addresses() []ids.ShortID {
	return w.kc.Addresses()
}

// Get implements Keychain
func (w *WalletKeychain) Get(addr ids.ShortID) (Signer, bool) {
	signer, found := w.kc.Get(addr)
	if !found {
		return nil, false
	}
	return &signerAdapter{signer: signer}, true
}

// GetEth returns a signer for an Ethereum address
func (w *WalletKeychain) GetEth(addr common.Address) (Signer, bool) {
	signer, found := w.kc.GetEth(addr)
	if !found {
		return nil, false
	}
	return &signerAdapter{signer: signer}, true
}

// EthAddresses returns Ethereum addresses
func (w *WalletKeychain) EthAddresses() set.Set[common.Address] {
	return w.kc.EthAddresses()
}