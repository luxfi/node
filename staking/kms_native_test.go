// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/keys"
)

// testMnemonic is the canonical BIP-39 all-zero-entropy English test vector.
const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

// DERIVE mode via the MNEMONIC env short-circuit (no live KMS): the resolved
// StakingIdentity must derive the SAME NodeIDs — classical AND strict-PQ — that
// the standalone keys derivations produce. This exercises the whole composition
// (mnemonic → derive → bundle) end-to-end minus the KMS wire.
func TestResolveStakingIdentity_DeriveEnvRoundTrip(t *testing.T) {
	t.Setenv("MNEMONIC", testMnemonic)

	id, err := ResolveStakingIdentity(context.Background(), NativeKMSConfig{
		Endpoint:         "kms:9999", // never dialed — env seam explicitly allowed
		Env:              "test",
		MnemonicPath:     "providers/lux/deploy-mnemonic",
		ValidatorIndex:   2,
		StrictPQ:         true,
		AllowEnvMnemonic: true, // dev seam: honor MNEMONIC env (H1: prod would force KMS)
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer id.Wipe()

	if !id.HasClassical() || !id.HasStrictPQ() {
		t.Fatal("expected both classical and strict-PQ materials")
	}

	// Classical NodeID: the resolved TLS cert must parse and yield a valid,
	// self-consistent NodeID. NOTE: the classical NodeID is NOT asserted equal
	// to a separate DeriveValidatorFromMnemonic call — ECDSA staking-cert
	// signing is non-deterministic under Go's hedged signer, so each derivation
	// mints a different cert (hence a different classical NodeID). Classical
	// NodeID STABILITY across boots is provided by CUSTODY (save/reload the
	// exact cert), proven in TestStakingIdentity_CustodyNodeIDStable.
	cert, err := LoadTLSCertFromBytes(id.TLSKeyPEM, id.TLSCertPEM)
	if err != nil {
		t.Fatalf("parse resolved TLS cert: %v", err)
	}
	gotClassicalNodeID := ids.NodeIDFromCert(&ids.Certificate{
		Raw:       cert.Leaf.Raw,
		PublicKey: cert.Leaf.PublicKey,
	})
	if gotClassicalNodeID == ids.EmptyNodeID {
		t.Fatal("resolved classical NodeID is empty")
	}

	// Strict-PQ NodeID: from ML-DSA-65 pub, must equal DeriveValidatorPQ's
	// (deterministic — this is the custody-free restart-stability guarantee).
	wantPQ, err := keys.DeriveValidatorPQ(testMnemonic, 2)
	if err != nil {
		t.Fatalf("reference pq derive: %v", err)
	}
	wantPQNodeID, err := wantPQ.StrictPQNodeID(ids.Empty)
	if err != nil {
		t.Fatalf("reference pq NodeID: %v", err)
	}
	gotPQNodeID, _, err := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, id.MLDSAPub)
	if err != nil {
		t.Fatalf("resolved pq NodeID: %v", err)
	}
	if gotPQNodeID != wantPQNodeID {
		t.Fatalf("strict-PQ NodeID mismatch: got %s want %s", gotPQNodeID, wantPQNodeID)
	}
}

// CUSTODY stability: an identity built from ONE derivation, marshaled and
// unmarshaled (the KMS save/reload round-trip), preserves BOTH NodeIDs exactly —
// the classical one (from the cert bytes) AND the strict-PQ one. This is how a
// classical validator gets a stable NodeID despite ECDSA-cert non-determinism.
func TestStakingIdentity_CustodyNodeIDStable(t *testing.T) {
	vk, err := keys.DeriveValidatorFromMnemonic(testMnemonic, 4)
	if err != nil {
		t.Fatalf("derive classical: %v", err)
	}
	pq, err := keys.DeriveValidatorPQ(testMnemonic, 4)
	if err != nil {
		t.Fatalf("derive pq: %v", err)
	}
	before := keys.StakingIdentityFromValidatorKey(vk, pq)

	blob, err := before.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	after, err := keys.UnmarshalStakingIdentity(blob)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Classical NodeID stable across the save/reload.
	certBefore, _ := LoadTLSCertFromBytes(before.TLSKeyPEM, before.TLSCertPEM)
	certAfter, _ := LoadTLSCertFromBytes(after.TLSKeyPEM, after.TLSCertPEM)
	nBefore := ids.NodeIDFromCert(&ids.Certificate{Raw: certBefore.Leaf.Raw, PublicKey: certBefore.Leaf.PublicKey})
	nAfter := ids.NodeIDFromCert(&ids.Certificate{Raw: certAfter.Leaf.Raw, PublicKey: certAfter.Leaf.PublicKey})
	if nBefore != nAfter {
		t.Fatalf("custody changed classical NodeID: %s != %s", nBefore, nAfter)
	}
	// Strict-PQ NodeID stable too.
	pqBefore, _, _ := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, before.MLDSAPub)
	pqAfter, _, _ := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, after.MLDSAPub)
	if pqBefore != pqAfter {
		t.Fatalf("custody changed strict-PQ NodeID: %s != %s", pqBefore, pqAfter)
	}
}

// M1: classical DERIVE (StrictPQ=false) is REFUSED — it would mint a new NodeID
// every boot (non-deterministic ECDSA staking cert). The resolver fails closed
// and steers the operator to strict-PQ DERIVE or CUSTODY. This check fires
// before any KMS/env access, so it is fully hermetic.
func TestResolveStakingIdentity_ClassicalDeriveRefused(t *testing.T) {
	t.Setenv("MNEMONIC", testMnemonic)
	_, err := ResolveStakingIdentity(context.Background(), NativeKMSConfig{
		Endpoint:         "kms:9999",
		Env:              "test",
		MnemonicPath:     "providers/lux/deploy-mnemonic",
		StrictPQ:         false,
		AllowEnvMnemonic: true, // even with the env seam, classical DERIVE is refused
	})
	if err == nil {
		t.Fatal("expected classical DERIVE to be refused (churns NodeID every boot)")
	}
	if !strings.Contains(err.Error(), "classical DERIVE") {
		t.Fatalf("wrong error (want classical-DERIVE refusal): %v", err)
	}
}

// H1: with a real (non-loopback) endpoint configured and NO env seam allowed, a
// present MNEMONIC env must NOT silently derive the identity — the node forces
// the authenticated KMS read (which then fails to reach the unresolvable test
// endpoint). This proves the env cannot supersede a configured production KMS.
func TestResolveStakingIdentity_EnvIgnoredWhenEndpointConfigured(t *testing.T) {
	t.Setenv("MNEMONIC", testMnemonic)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := ResolveStakingIdentity(ctx, NativeKMSConfig{
		Endpoint:         "kms.invalid:9999", // non-loopback, NXDOMAIN → dial fails fast
		Env:              "test",
		MnemonicPath:     "providers/lux/deploy-mnemonic",
		ValidatorIndex:   2,
		StrictPQ:         true,
		AllowEnvMnemonic: false, // production: env must be ignored, KMS forced
	})
	if err == nil {
		t.Fatal("expected forced KMS read to fail (env must NOT supersede a configured endpoint)")
	}
	// The failure must come from the KMS load (forced read), not from deriving
	// off the env mnemonic — i.e. the env was ignored.
	if !strings.Contains(err.Error(), "load deploy mnemonic from KMS") {
		t.Fatalf("expected a KMS-load failure (env ignored), got: %v", err)
	}
}

// H1 (deterministic routing decision): the env seam is permitted ONLY for
// loopback endpoints or the explicit dev flag. Everything else is production.
func TestIsLoopbackEndpoint(t *testing.T) {
	loop := []string{"127.0.0.1:9999", "localhost:9999", "[::1]:9999", "127.0.0.1", "localhost"}
	prod := []string{"kms:9999", "kms.lux.network:9999", "10.0.0.5:9999", "kms.invalid:9999", ""}
	for _, e := range loop {
		if !isLoopbackEndpoint(e) {
			t.Errorf("isLoopbackEndpoint(%q) = false, want true", e)
		}
	}
	for _, e := range prod {
		if isLoopbackEndpoint(e) {
			t.Errorf("isLoopbackEndpoint(%q) = true, want false", e)
		}
	}
}

// L3: verifyNodeLabel rejects a stored blob minted for a different node, accepts
// a matching label, and warns (not errors) when both are unset.
func TestVerifyNodeLabel(t *testing.T) {
	mk := func(label string) *keys.StakingIdentity {
		return &keys.StakingIdentity{NodeLabel: []byte(label)}
	}
	// Match → ok.
	if err := verifyNodeLabel(NativeKMSConfig{NodeLabel: "node-a", IdentityPath: "p"}, mk("node-a")); err != nil {
		t.Fatalf("matching label rejected: %v", err)
	}
	// Mismatch → refuse (this node must not adopt another node's identity).
	if err := verifyNodeLabel(NativeKMSConfig{NodeLabel: "node-b", IdentityPath: "p"}, mk("node-a")); err == nil {
		t.Fatal("label mismatch was not rejected")
	}
	// Configured label but stored blob unlabeled → refuse.
	if err := verifyNodeLabel(NativeKMSConfig{NodeLabel: "node-b", IdentityPath: "p"}, mk("")); err == nil {
		t.Fatal("labeled node adopting an unlabeled blob was not rejected")
	}
	// Unlabeled node, unlabeled blob → allowed (with a warn), no error.
	if err := verifyNodeLabel(NativeKMSConfig{NodeLabel: "", IdentityPath: "p"}, mk("")); err != nil {
		t.Fatalf("unlabeled/unlabeled must be allowed: %v", err)
	}
}

// Re-resolving from the same mnemonic yields the SAME strict-PQ NodeID — the
// custody-free restart-stability guarantee, verified through the resolver.
func TestResolveStakingIdentity_DeriveStable(t *testing.T) {
	t.Setenv("MNEMONIC", testMnemonic)
	cfg := NativeKMSConfig{Endpoint: "kms:9999", Env: "test", MnemonicPath: "p", ValidatorIndex: 5, StrictPQ: true, AllowEnvMnemonic: true}
	a, err := ResolveStakingIdentity(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resolve a: %v", err)
	}
	defer a.Wipe()
	b, err := ResolveStakingIdentity(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resolve b: %v", err)
	}
	defer b.Wipe()
	na, _, _ := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, a.MLDSAPub)
	nb, _, _ := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, b.MLDSAPub)
	if na != nb {
		t.Fatal("re-resolved identity produced a different NodeID (stability broken)")
	}
}

// Mode selection: neither or both of MnemonicPath/IdentityPath is a hard error
// (checked before any network I/O).
func TestResolveStakingIdentity_ModeAmbiguity(t *testing.T) {
	t.Setenv("MNEMONIC", testMnemonic)
	cases := []NativeKMSConfig{
		{Endpoint: "kms:9999", Env: "test"},                                       // neither
		{Endpoint: "kms:9999", Env: "test", MnemonicPath: "m", IdentityPath: "i"}, // both
	}
	for i, c := range cases {
		if _, err := ResolveStakingIdentity(context.Background(), c); err == nil {
			t.Fatalf("case %d: expected ErrKMSModeAmbiguous", i)
		}
	}
}

// Endpoint and env are required (fail closed before any dial).
func TestResolveStakingIdentity_RequiresEndpointEnv(t *testing.T) {
	t.Setenv("MNEMONIC", testMnemonic)
	if _, err := ResolveStakingIdentity(context.Background(), NativeKMSConfig{Env: "test", MnemonicPath: "m"}); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
	if _, err := ResolveStakingIdentity(context.Background(), NativeKMSConfig{Endpoint: "kms:9999", MnemonicPath: "m"}); err == nil {
		t.Fatal("expected error for empty env")
	}
}

// The bootstrap identity helper derives a stable ML-DSA-65 auth identity from a
// mnemonic and returns nil for an empty seed (anonymous dial).
func TestBootstrapIdentity(t *testing.T) {
	none, err := bootstrapIdentity("")
	if err != nil || none != nil {
		t.Fatalf("empty seed should yield (nil,nil), got (%v,%v)", none, err)
	}
	a, err := bootstrapIdentity(testMnemonic)
	if err != nil {
		t.Fatalf("bootstrap identity: %v", err)
	}
	defer a.Wipe()
	b, _ := bootstrapIdentity(testMnemonic)
	defer b.Wipe()
	if a.NodeID != b.NodeID {
		t.Fatal("bootstrap identity is not deterministic")
	}
	if _, err := bootstrapIdentity("not a valid bip39 phrase"); err == nil {
		t.Fatal("expected error for invalid bootstrap mnemonic")
	}
}
