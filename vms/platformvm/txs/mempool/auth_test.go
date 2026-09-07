// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"testing"

	"github.com/luxfi/consensus/config"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/txs/auth"
	"github.com/luxfi/utxo/secp256k1fx"
)

// TestMempoolAdd_StrictPQ_RefusesClassicalCredentials verifies that the
// P-chain mempool refuses a tx whose credentials carry a classical
// secp256k1fx.Credential when the chain's profile pins
// RequireTypedTxAuth=true and no ClassicalCompatRegistry names the
// originator. This is the mempool-level enforcement of the F-class
// objection: a strict-PQ chain MUST NOT gossip a classical-credential
// tx.
func TestMempoolAdd_StrictPQ_RefusesClassicalCredentials(t *testing.T) {
	mpool, err := New("test", metric.NewRegistry())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Pin strict-PQ; supply no registry so every classical credential
	// is refused.
	mpool.SetAuthPolicy(&config.ChainSecurityProfile{RequireTypedTxAuth: true}, nil)

	decisions, err := createTestDecisionTxs(1)
	if err != nil {
		t.Fatalf("createTestDecisionTxs: %v", err)
	}
	tx := decisions[0]
	// createTestDecisionTxs leaves Creds empty; inject a single
	// classical credential to exercise the gate.
	tx.Creds = []verify.Verifiable{&secp256k1fx.Credential{}}

	if err := mpool.Add(tx); !errors.Is(err, auth.ErrLegacyCredentialUnderStrictPQ) {
		t.Fatalf("mempool.Add: got %v, want ErrLegacyCredentialUnderStrictPQ", err)
	}
}

// TestMempoolAdd_NoPolicy_AdmitsClassicalCredentials confirms backward
// compatibility — a mempool that has never called SetAuthPolicy admits
// classical credentials unchanged. This is the path every existing
// caller follows today; the new gate is opt-in.
func TestMempoolAdd_NoPolicy_AdmitsClassicalCredentials(t *testing.T) {
	mpool, err := New("test", metric.NewRegistry())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	decisions, err := createTestDecisionTxs(1)
	if err != nil {
		t.Fatalf("createTestDecisionTxs: %v", err)
	}
	tx := decisions[0]
	tx.Creds = []verify.Verifiable{&secp256k1fx.Credential{}}

	if err := mpool.Add(tx); err != nil {
		t.Fatalf("mempool.Add: %v (expected admission)", err)
	}
}

// TestMempoolAdd_ClassicalCompatProfile_AdmitsClassicalCredentials
// proves the gate is a no-op when the chain pins
// RequireTypedTxAuth=false (the classical-compat profile path).
func TestMempoolAdd_ClassicalCompatProfile_AdmitsClassicalCredentials(t *testing.T) {
	mpool, err := New("test", metric.NewRegistry())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mpool.SetAuthPolicy(&config.ChainSecurityProfile{RequireTypedTxAuth: false}, nil)

	decisions, err := createTestDecisionTxs(1)
	if err != nil {
		t.Fatalf("createTestDecisionTxs: %v", err)
	}
	tx := decisions[0]
	tx.Creds = []verify.Verifiable{&secp256k1fx.Credential{}}

	if err := mpool.Add(tx); err != nil {
		t.Fatalf("mempool.Add: %v (expected admission under classical-compat)", err)
	}
}
