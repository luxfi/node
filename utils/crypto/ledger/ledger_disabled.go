// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !ledger
// +build !ledger

package ledger

import (
	"errors"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/crypto/keychain"
)

var ErrLedgerDisabled = errors.New("ledger support is disabled")

type Ledger struct{}

type Keychain struct{}

func NewKeychain() (*Keychain, error) {
	return nil, ErrLedgerDisabled
}

func (l *Keychain) Close() error {
	return ErrLedgerDisabled
}

func (l *Keychain) Addresses() []ids.ShortID {
	return nil
}

func (l *Keychain) Ledger() *Ledger {
	return nil
}

func (l *Keychain) Get(address ids.ShortID) (keychain.Signer, bool) {
	return nil, false
}

func (l *Keychain) Match(owners interface{}, minSigs uint32) ([]ids.ShortID, []uint32, bool) {
	return nil, nil, false
}

func (l *Keychain) Spend(owners interface{}, minSigs uint32) ([]ids.ShortID, []keychain.Signer, bool) {
	return nil, nil, false
}