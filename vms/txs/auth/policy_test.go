// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package auth

import (
	"errors"
	"testing"

	"github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/utxo/secp256k1fx"
)

// fakePQCred satisfies verify.Verifiable but is NOT a
// *secp256k1fx.Credential. EnforceCredentialPolicy treats every non-
// classical-credential type as PQ-compatible.
type fakePQCred struct{}

func (fakePQCred) Verify() error { return nil }

// pqProfile returns a minimal ChainSecurityProfile with
// RequireTypedTxAuth=true. The other fields are left zero; the gate only
// reads RequireTypedTxAuth.
func pqProfile() *config.ChainSecurityProfile {
	return &config.ChainSecurityProfile{RequireTypedTxAuth: true}
}

// classicalProfile returns a profile with RequireTypedTxAuth=false, i.e.
// the gate should admit anything.
func classicalProfile() *config.ChainSecurityProfile {
	return &config.ChainSecurityProfile{RequireTypedTxAuth: false}
}

func TestEnforceCredentialPolicy_NilProfile_IsProgrammerError(t *testing.T) {
	err := EnforceCredentialPolicy(nil, nil, nil, ids.ShortEmpty)
	if !errors.Is(err, ErrNilProfile) {
		t.Fatalf("expected ErrNilProfile, got %v", err)
	}
}

func TestEnforceCredentialPolicy_ClassicalProfile_AdmitsClassicalCredential(t *testing.T) {
	creds := []verify.Verifiable{&secp256k1fx.Credential{}}
	if err := EnforceCredentialPolicy(creds, classicalProfile(), nil, ids.ShortEmpty); err != nil {
		t.Fatalf("classical profile should admit classical credential, got %v", err)
	}
}

func TestEnforceCredentialPolicy_StrictPQ_AdmitsPQCredentials(t *testing.T) {
	creds := []verify.Verifiable{fakePQCred{}, fakePQCred{}}
	if err := EnforceCredentialPolicy(creds, pqProfile(), nil, ids.ShortEmpty); err != nil {
		t.Fatalf("strict-PQ profile should admit PQ credentials, got %v", err)
	}
}

func TestEnforceCredentialPolicy_StrictPQ_RefusesClassicalWithoutRegistry(t *testing.T) {
	creds := []verify.Verifiable{&secp256k1fx.Credential{}}
	err := EnforceCredentialPolicy(creds, pqProfile(), nil, ids.ShortEmpty)
	if !errors.Is(err, ErrLegacyCredentialUnderStrictPQ) {
		t.Fatalf("strict-PQ with no registry should refuse classical credential, got %v", err)
	}
}

func TestEnforceCredentialPolicy_StrictPQ_RefusesClassicalNotInRegistry(t *testing.T) {
	allowed := ids.ShortID{0x01}
	other := ids.ShortID{0x02}
	registry := NewStaticClassicalCompatRegistry([]ids.ShortID{allowed})

	creds := []verify.Verifiable{&secp256k1fx.Credential{}}
	err := EnforceCredentialPolicy(creds, pqProfile(), registry, other)
	if !errors.Is(err, ErrLegacyCredentialUnderStrictPQ) {
		t.Fatalf("strict-PQ with originator not in registry should refuse, got %v", err)
	}
}

func TestEnforceCredentialPolicy_StrictPQ_AdmitsClassicalInRegistry(t *testing.T) {
	allowed := ids.ShortID{0x01}
	registry := NewStaticClassicalCompatRegistry([]ids.ShortID{allowed})

	creds := []verify.Verifiable{&secp256k1fx.Credential{}}
	if err := EnforceCredentialPolicy(creds, pqProfile(), registry, allowed); err != nil {
		t.Fatalf("strict-PQ with allow-listed originator should admit, got %v", err)
	}
}

func TestEnforceCredentialPolicy_StrictPQ_MixedCreds_OneClassicalRefuses(t *testing.T) {
	creds := []verify.Verifiable{
		fakePQCred{},
		&secp256k1fx.Credential{},
		fakePQCred{},
	}
	err := EnforceCredentialPolicy(creds, pqProfile(), nil, ids.ShortEmpty)
	if !errors.Is(err, ErrLegacyCredentialUnderStrictPQ) {
		t.Fatalf("mixed creds with one classical should refuse, got %v", err)
	}
}

func TestEnforceCredentialPolicy_EmptyCreds_AlwaysAdmits(t *testing.T) {
	// No credentials means nothing to refuse — the gate is policy on
	// credentials present, not a requirement that any be present. The
	// chain's syntactic verifier enforces "at least one credential".
	if err := EnforceCredentialPolicy(nil, pqProfile(), nil, ids.ShortEmpty); err != nil {
		t.Fatalf("empty creds should admit under strict-PQ, got %v", err)
	}
}

func TestStaticClassicalCompatRegistry_NilSafe(t *testing.T) {
	var r *staticRegistry
	if r.IsAllowed(ids.ShortID{0x01}) {
		t.Fatalf("nil receiver must report no allow-list membership")
	}
}

func TestStaticClassicalCompatRegistry_DistinguishesAddresses(t *testing.T) {
	a := ids.ShortID{0x01}
	b := ids.ShortID{0x02}
	r := NewStaticClassicalCompatRegistry([]ids.ShortID{a})

	if !r.IsAllowed(a) {
		t.Fatalf("registry should report %x allowed", a[:])
	}
	if r.IsAllowed(b) {
		t.Fatalf("registry should NOT report %x allowed", b[:])
	}
}
