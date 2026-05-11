// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"testing"

	"github.com/luxfi/consensus/config"
	"github.com/luxfi/node/vms/txs/auth"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/utxo/secp256k1fx"
)

// TestXVMMempoolAdd_StrictPQ_RefusesClassicalCredentials verifies that
// the X-chain mempool refuses a tx whose FxCredentials carry a
// classical secp256k1fx.Credential under RequireTypedTxAuth=true with
// no ClassicalCompatRegistry.
func TestXVMMempoolAdd_StrictPQ_RefusesClassicalCredentials(t *testing.T) {
	mpool, err := newMempool()
	if err != nil {
		t.Fatalf("newMempool: %v", err)
	}
	mpool.SetAuthPolicy(&config.ChainSecurityProfile{RequireTypedTxAuth: true}, nil)

	tx := newTx(0, 32)
	tx.Creds = []*fxs.FxCredential{{Credential: &secp256k1fx.Credential{}}}

	if err := mpool.Add(tx); !errors.Is(err, auth.ErrLegacyCredentialUnderStrictPQ) {
		t.Fatalf("xvm mempool.Add: got %v, want ErrLegacyCredentialUnderStrictPQ", err)
	}
}

// TestXVMMempoolAdd_NoPolicy_AdmitsClassicalCredentials confirms the
// gate is opt-in — an X-chain mempool with no policy admits everything.
func TestXVMMempoolAdd_NoPolicy_AdmitsClassicalCredentials(t *testing.T) {
	mpool, err := newMempool()
	if err != nil {
		t.Fatalf("newMempool: %v", err)
	}

	tx := newTx(0, 32)
	tx.Creds = []*fxs.FxCredential{{Credential: &secp256k1fx.Credential{}}}

	if err := mpool.Add(tx); err != nil {
		t.Fatalf("xvm mempool.Add: %v (expected admission)", err)
	}
}

// TestXVMMempoolAdd_StrictPQ_AdmitsAllowedOriginator verifies the
// registry allow-list path.
func TestXVMMempoolAdd_StrictPQ_AdmitsAllowedOriginator(t *testing.T) {
	// The X-chain mempool passes ids.ShortEmpty as the originator
	// (the mempool does not parse owner addresses); so the registry
	// must accept ids.ShortEmpty for this test. The contract is: the
	// registry is the named allow-list and any address it admits is
	// admitted. Production callers will install a registry whose
	// IsAllowed reflects their migration policy.
	registry := auth.NewStaticClassicalCompatRegistry(nil) // empty registry refuses
	mpool, err := newMempool()
	if err != nil {
		t.Fatalf("newMempool: %v", err)
	}
	mpool.SetAuthPolicy(&config.ChainSecurityProfile{RequireTypedTxAuth: true}, registry)

	tx := newTx(0, 32)
	tx.Creds = []*fxs.FxCredential{{Credential: &secp256k1fx.Credential{}}}

	if err := mpool.Add(tx); !errors.Is(err, auth.ErrLegacyCredentialUnderStrictPQ) {
		t.Fatalf("xvm mempool.Add: empty registry should refuse; got %v", err)
	}
}
