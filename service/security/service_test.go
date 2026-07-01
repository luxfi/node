// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package security

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-json-experiment/json"
	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/log"
)

// TestRPC_securityProfile_StrictPQ proves the securityProfile
// RPC returns the canonical strict-PQ shape: every Forbid bit true,
// every E2E axis post-quantum, hash suite SHA3_NIST, contract auth
// names the ML_DSA_65|ZCHAIN_AUTH_PROOF union, and the post-quantum
// + NIST + Lux-canonical booleans are all true.
//
// Closes the spec contract for the public RPC surface that auditors
// and dApps consult.
func TestRPC_securityProfile_StrictPQ(t *testing.T) {
	profile := consensusconfig.StrictPQ()
	svc := &Service{log: log.Noop(), profile: profile}

	var reply ProfileReply
	if err := svc.SecurityProfile(nil, nil, &reply); err != nil {
		t.Fatalf("SecurityProfile: %v", err)
	}

	if reply.ProfileID != uint32(consensusconfig.ProfileStrictPQ) {
		t.Errorf("ProfileID = %d; want %d",
			reply.ProfileID, consensusconfig.ProfileStrictPQ)
	}
	if reply.ProfileName != "STRICT" {
		t.Errorf("ProfileName = %q; want STRICT", reply.ProfileName)
	}
	wantHash := "0x" + hex.EncodeToString(profile.ProfileHash[:])
	if reply.ProfileHash != wantHash {
		t.Errorf("ProfileHash = %q; want %q", reply.ProfileHash, wantHash)
	}
	if !reply.PostQuantumEndToEnd {
		t.Error("PostQuantumEndToEnd = false on StrictPQ; want true")
	}
	if !reply.NISTFriendly {
		t.Error("NISTFriendly = false on StrictPQ; want true")
	}
	if !reply.LuxCanonical {
		t.Error("LuxCanonical = false on StrictPQ; want true")
	}
	if reply.HashSuite != "SHA3_NIST" {
		t.Errorf("HashSuite = %q; want SHA3_NIST", reply.HashSuite)
	}
	if reply.WalletScheme != "ML_DSA_65" {
		t.Errorf("WalletScheme = %q; want ML_DSA_65", reply.WalletScheme)
	}
	if reply.TxScheme != "ML_DSA_65" {
		t.Errorf("TxScheme = %q; want ML_DSA_65", reply.TxScheme)
	}
	if reply.ContractAuth != "ML_DSA_65|ZCHAIN_AUTH_PROOF" {
		t.Errorf("ContractAuth = %q; want ML_DSA_65|ZCHAIN_AUTH_PROOF",
			reply.ContractAuth)
	}
	if reply.KeyExchange != "ML_KEM_768" {
		t.Errorf("KeyExchange = %q; want ML_KEM_768", reply.KeyExchange)
	}
	if reply.HighValueKEM != "ML_KEM_1024" {
		t.Errorf("HighValueKEM = %q; want ML_KEM_1024", reply.HighValueKEM)
	}
	if reply.RecoveryScheme != "SLH_DSA_192" {
		t.Errorf("RecoveryScheme = %q; want SLH_DSA_192", reply.RecoveryScheme)
	}
	if reply.ProofPolicy != "STARK_FRI_SHA3_PQ" {
		t.Errorf("ProofPolicy = %q; want STARK_FRI_SHA3_PQ", reply.ProofPolicy)
	}

	// Every strict-PQ Forbid bit MUST be true on the wire.
	allTrue := map[string]bool{
		"ForbidECDSAWallets":      reply.ForbidECDSAWallets,
		"ForbidECDSAContractAuth": reply.ForbidECDSAContractAuth,
		"ForbidBLSContractAuth":   reply.ForbidBLSContractAuth,
		"ForbidClassicalKEM":      reply.ForbidClassicalKEM,
		"RequireTypedTxAuth":      reply.RequireTypedTxAuth,
		"ForbidPairings":          reply.ForbidPairings,
		"ForbidKZG":               reply.ForbidKZG,
		"ForbidTrustedSetup":      reply.ForbidTrustedSetup,
		"ForbidClassicalSNARKs":   reply.ForbidClassicalSNARKs,
		"ForbidDevProofs":         reply.ForbidDevProofs,
		"ForbidFallbacks":         reply.ForbidFallbacks,
	}
	for name, v := range allTrue {
		if !v {
			t.Errorf("%s = false on StrictPQ; want true", name)
		}
	}
}

// TestRPC_securityProfile_Unsafe proves the unsafe-fork profile
// surfaces every false flag the spec demands: post_quantum_end_to_end,
// nist_friendly stays true (the hash suite is still SHA3-NIST), but
// lux_canonical is false and every forbid_ecdsa* / forbid_bls* /
// forbid_classical_kem / require_typed_tx_auth is false.
//
// This is the load-bearing audit gate: a wallet that pins
// post_quantum_end_to_end=true MUST refuse to sign against a chain
// whose RPC reports this shape.
func TestRPC_securityProfile_Unsafe(t *testing.T) {
	profile := &consensusconfig.ForkClassicalCompatUnsafeProfile
	// ComputeHash to mirror what initSecurityProfile does at boot.
	hash, err := profile.ComputeHash()
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	pCopy := *profile
	pCopy.ProfileHash = hash
	svc := &Service{log: log.Noop(), profile: &pCopy}

	var reply ProfileReply
	if err := svc.SecurityProfile(nil, nil, &reply); err != nil {
		t.Fatalf("SecurityProfile: %v", err)
	}

	if reply.ProfileName != "FORK_CLASSICAL_COMPAT_UNSAFE" {
		t.Errorf("ProfileName = %q; want FORK_CLASSICAL_COMPAT_UNSAFE",
			reply.ProfileName)
	}
	if reply.ProfileID != uint32(consensusconfig.ForkClassicalCompatUnsafeProfileID) {
		t.Errorf("ProfileID = %#x; want %#x",
			reply.ProfileID, consensusconfig.ForkClassicalCompatUnsafeProfileID)
	}
	if reply.PostQuantumEndToEnd {
		t.Error("PostQuantumEndToEnd = true on unsafe fork; want false")
	}
	if reply.LuxCanonical {
		t.Error("LuxCanonical = true on unsafe fork; want false")
	}

	// Every E2E forbid bit MUST be false on the wire.
	allFalse := map[string]bool{
		"ForbidECDSAWallets":      reply.ForbidECDSAWallets,
		"ForbidECDSAContractAuth": reply.ForbidECDSAContractAuth,
		"ForbidBLSContractAuth":   reply.ForbidBLSContractAuth,
		"ForbidClassicalKEM":      reply.ForbidClassicalKEM,
		"RequireTypedTxAuth":      reply.RequireTypedTxAuth,
	}
	for name, v := range allFalse {
		if v {
			t.Errorf("%s = true on unsafe fork; want false", name)
		}
	}
	// Scheme axes name the classical primitives on every E2E layer.
	if !strings.HasPrefix(reply.WalletScheme, "ECDSA_UNSAFE") {
		t.Errorf("WalletScheme = %q; want ECDSA_UNSAFE_*", reply.WalletScheme)
	}
	if !strings.HasPrefix(reply.KeyExchange, "X25519_UNSAFE") {
		t.Errorf("KeyExchange = %q; want X25519_UNSAFE_*", reply.KeyExchange)
	}
}

// TestRPC_securityProfile_NoProfile_Refused proves the RPC
// refuses to answer when the node booted without a profile pin. A
// caller MUST see ErrNoProfile, not a half-populated reply.
func TestRPC_securityProfile_NoProfile_Refused(t *testing.T) {
	svc := &Service{log: log.Noop(), profile: nil}
	var reply ProfileReply
	err := svc.SecurityProfile(nil, nil, &reply)
	if !errors.Is(err, ErrNoProfile) {
		t.Errorf("SecurityProfile() = %v; want ErrNoProfile", err)
	}
}

// TestRPC_blockSecurity_StrictPQ proves the block-level reply
// carries the chain-wide profile envelope and the canonical proof
// backend name.
func TestRPC_blockSecurity_StrictPQ(t *testing.T) {
	svc := &Service{log: log.Noop(), profile: consensusconfig.StrictPQ()}
	var reply BlockSecurityReply
	if err := svc.BlockSecurity(nil, &BlockSecurityArgs{}, &reply); err != nil {
		t.Fatalf("BlockSecurity: %v", err)
	}
	if reply.SecurityProfileName != "STRICT" {
		t.Errorf("SecurityProfileName = %q; want STRICT",
			reply.SecurityProfileName)
	}
	if !reply.PulsarMSignatureValid {
		t.Error("PulsarMSignatureValid = false; want true on StrictPQ")
	}
	if !reply.PostQuantumEndToEnd {
		t.Error("PostQuantumEndToEnd = false; want true on StrictPQ")
	}
	if reply.ProofBackendID == 0 {
		t.Error("ProofBackendID = 0; want a populated backend byte")
	}
	if reply.ProofBackendName == "" {
		t.Error("ProofBackendName empty; want a populated name")
	}
}

// TestREST_securityProfile_GET proves the /profile sidecar (full path
// /v1/security/profile when mounted on APIServer) returns the same
// JSON shape as the JSON-RPC handler. One shape, two transports — no
// per-transport drift.
func TestREST_securityProfile_GET(t *testing.T) {
	profile := consensusconfig.StrictPQ()
	handler, err := NewHandler(log.Noop(), profile)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/profile")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q; want application/json prefix", got)
	}
	var body ProfileReply
	if err := json.UnmarshalRead(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ProfileName != "STRICT" {
		t.Errorf("REST ProfileName = %q; want STRICT", body.ProfileName)
	}
	if body.ContractAuth != "ML_DSA_65|ZCHAIN_AUTH_PROOF" {
		t.Errorf("REST ContractAuth = %q; want ML_DSA_65|ZCHAIN_AUTH_PROOF",
			body.ContractAuth)
	}
}

// TestREST_securityProfile_MethodNotAllowed proves the REST sidecar
// refuses non-GET methods (the endpoint is read-only).
func TestREST_securityProfile_MethodNotAllowed(t *testing.T) {
	profile := consensusconfig.StrictPQ()
	handler, err := NewHandler(log.Noop(), profile)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/profile",
		"application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d; want 405", resp.StatusCode)
	}
}

// TestREST_blockSecurity_GET proves the /block/{n} sidecar (full path
// /v1/security/block/{n} when mounted on APIServer) returns the same
// JSON shape as the JSON-RPC handler. One shape, two transports — no
// per-transport drift.
func TestREST_blockSecurity_GET(t *testing.T) {
	profile := consensusconfig.StrictPQ()
	handler, err := NewHandler(log.Noop(), profile)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/block/12345")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var body BlockSecurityReply
	if err := json.UnmarshalRead(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.SecurityProfileName != "STRICT" {
		t.Errorf("REST SecurityProfileName = %q; want STRICT",
			body.SecurityProfileName)
	}
	if !body.PulsarMSignatureValid {
		t.Error("PulsarMSignatureValid = false; want true on StrictPQ")
	}
	if !body.PostQuantumEndToEnd {
		t.Error("PostQuantumEndToEnd = false; want true on StrictPQ")
	}
}

// TestRenderName_StableMapping fences the name-translation behaviour
// so a stray refactor cannot silently change the wire vocabulary.
func TestRenderName_StableMapping(t *testing.T) {
	cases := map[string]string{
		"ml-dsa-65":              "ML_DSA_65",
		"ml-kem-768":             "ML_KEM_768",
		"sha3-nist":              "SHA3_NIST",
		"stark-fri-sha3-pq":      "STARK_FRI_SHA3_PQ",
		"pulsar-m-65":            "PULSAR_M_65",
		"slh-dsa-192":            "SLH_DSA_192",
		"":                       "",
		"ecdsa-unsafe-classical": "ECDSA_UNSAFE_CLASSICAL",
	}
	for in, want := range cases {
		if got := renderName(in); got != want {
			t.Errorf("renderName(%q) = %q; want %q", in, got, want)
		}
	}
}
