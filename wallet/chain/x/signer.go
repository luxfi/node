// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	stdcontext "context"

	"github.com/luxfi/ids"
	ledgerkeychain "github.com/luxfi/ledger-lux-go/keychain"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/crypto/keychain"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/exchangevm/txs"
)

var _ Signer = (*txSigner)(nil)

type Signer interface {
	SignUnsigned(ctx stdcontext.Context, tx txs.UnsignedTx) (*txs.Tx, error)
	Sign(ctx stdcontext.Context, tx *txs.Tx) error
}

type SignerBackend interface {
	GetUTXO(ctx stdcontext.Context, chainID, utxoID ids.ID) (*lux.UTXO, error)
}

type txSigner struct {
	kc      ledgerkeychain.Keychain
	backend SignerBackend
}

func NewSigner(kc ledgerkeychain.Keychain, backend SignerBackend) Signer {
	return &txSigner{
		kc:      kc,
		backend: backend,
	}
}

// keychainAdapter adapts ledger keychain to utils keychain interface
type keychainAdapter struct {
	*ledgerkeychain.Keychain
}

func (k *keychainAdapter) Addresses() set.Set[ids.ShortID] {
	addrs := k.Keychain.Addresses()
	result := set.NewSet[ids.ShortID](len(addrs))
	for _, addr := range addrs {
		result.Add(addr)
	}
	return result
}

func (k *keychainAdapter) Get(addr ids.ShortID) (keychain.Signer, bool) {
	ledgerSigner, ok := k.Keychain.Get(addr)
	if !ok {
		return nil, false
	}
	return ledgerSigner, true
}

func (s *txSigner) SignUnsigned(ctx stdcontext.Context, utx txs.UnsignedTx) (*txs.Tx, error) {
	tx := &txs.Tx{Unsigned: utx}
	return tx, s.Sign(ctx, tx)
}

func (s *txSigner) Sign(ctx stdcontext.Context, tx *txs.Tx) error {
	return tx.Unsigned.Visit(&signerVisitor{
		kc:      &keychainAdapter{Keychain: &s.kc},
		backend: s.backend,
		ctx:     ctx,
		tx:      tx,
	})
}
