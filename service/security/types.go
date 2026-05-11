// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package security exposes the chain-wide ChainSecurityProfile to operators,
// dApps, and auditors through the security JSON-RPC namespace at
// /ext/security and two REST sidecars at /ext/security/profile and
// /ext/security/block/{n}.
//
// The handlers in this package are read-only: every shape is derived from
// the immutable *consensusconfig.ChainSecurityProfile resolved at node
// bootstrap (Node.initSecurityProfile, F102). There is no per-request
// re-resolution and no profile mutation surface.
//
// One service, one namespace ("security"), one fixed JSON shape. The
// wire vocabulary uses SCREAMING_SNAKE_CASE canonical names
// (renderName) so audit pipelines can grep on stable identifiers; the
// underlying scheme IDs come from consensus/config.
package security

import (
	consensusconfig "github.com/luxfi/consensus/config"
)

// ProfileReply is the JSON body returned by the securityProfile RPC
// (POST /ext/security) and the REST sidecar (GET /ext/security/profile).
//
// Stable shape: every field has a fixed JSON tag, no embedded structs.
// Adding a new field requires bumping the major version of the security
// namespace; renaming an existing field is forbidden.
//
// The field order in the struct literal MUST match the field order in
// the locked profile (consensus/config.profiles.go) so a developer
// reviewing a diff against the canonical profile sees the same shape.
type ProfileReply struct {
	// ProfileID is the wire byte of the active profile (see
	// consensus/config.ProfileID). Stable across upgrades.
	ProfileID uint32 `json:"profile_id"`

	// ProfileName is the canonical SCREAMING_SNAKE_CASE name (e.g.
	// "LUX_STRICT_PQ"). Audit tooling matches on this name.
	ProfileName string `json:"profile_name"`

	// ProfileHash is the 48-byte SHA3-384 commitment to the profile
	// encoded as a 0x-prefixed lowercase hex string (96 hex chars).
	// Distinct profiles produce distinct hashes; identical profiles
	// produce identical hashes regardless of allow-list ordering.
	ProfileHash string `json:"profile_hash"`

	// PostQuantumEndToEnd reports whether every E2E axis (wallet, tx,
	// contract-auth, KEM, recovery) is satisfied by a NIST-PQ
	// primitive AND every classical primitive is explicitly
	// forbidden. Single boolean for dashboard wiring.
	PostQuantumEndToEnd bool `json:"post_quantum_end_to_end"`

	// NISTFriendly reports whether the chain pins a NIST-aligned
	// canonical Lux profile (StrictPQ or FIPS). A profile whose
	// HashSuiteID is SHA3_NIST but whose Forbid* bits are disabled
	// (the classical-compat fork case) is NOT NIST-friendly: NIST
	// alignment is a full-posture predicate, not a hash-suite axis.
	NISTFriendly bool `json:"nist_friendly"`

	// LuxCanonical reports whether the profile is one of the three
	// audit-signed-off-on Lux canonical profiles (StrictPQ, FIPS,
	// Permissive). Downstream forks that pin a different ProfileID
	// MUST receive false here so audit attestations refuse them.
	LuxCanonical bool `json:"lux_canonical"`

	// Scheme axes — each rendered as the SCREAMING_SNAKE canonical
	// name of the scheme byte. None / Invalid / unknown bytes are
	// rendered as the generic "UNKNOWN_0x<hex>" form so a forked
	// binary that pins an unknown byte cannot silently masquerade as
	// a canonical scheme.
	HashSuite        string `json:"hash_suite"`
	WalletScheme     string `json:"wallet_scheme"`
	TxScheme         string `json:"tx_scheme"`
	ContractAuth     string `json:"contract_auth"`
	ValidatorScheme  string `json:"validator_scheme"`
	FinalityScheme   string `json:"finality_scheme"`
	HighValueScheme  string `json:"high_value_scheme"`
	ProofPolicy      string `json:"proof_policy"`
	KeyExchange      string `json:"key_exchange"`
	HighValueKEM     string `json:"high_value_kem"`
	RecoveryScheme   string `json:"recovery_scheme"`

	// Forbid* axes — one boolean per refused-primitive class. Audit
	// tooling reads these to verify a deployment refuses what it
	// claims to refuse. Strict-PQ profiles set every entry true.
	ForbidECDSAWallets      bool `json:"forbid_ecdsa_wallets"`
	ForbidECDSAContractAuth bool `json:"forbid_ecdsa_contract_auth"`
	ForbidBLSContractAuth   bool `json:"forbid_bls_contract_auth"`
	ForbidClassicalKEM      bool `json:"forbid_classical_kem"`
	RequireTypedTxAuth      bool `json:"require_typed_tx_auth"`
	ForbidPairings          bool `json:"forbid_pairings"`
	ForbidKZG               bool `json:"forbid_kzg"`
	ForbidTrustedSetup      bool `json:"forbid_trusted_setup"`
	ForbidClassicalSNARKs   bool `json:"forbid_classical_snarks"`
	ForbidDevProofs         bool `json:"forbid_dev_proofs"`
	ForbidFallbacks         bool `json:"forbid_fallbacks"`
}

// BlockSecurityReply is the JSON body returned by the blockSecurity
// RPC (POST /ext/security) and the REST endpoint
// /ext/security/block/{n}. It enriches a block lookup with the
// chain-wide security envelope so explorers can show "this block was
// finalised under profile X with backend Y" without reimplementing
// profile lookup.
//
// pulsar_m_signature_valid is true iff the chain-wide FinalitySchemeID
// is a Pulsar-M scheme — the per-block signature is verified by the
// consensus layer before the block is accepted, so a block reachable
// via this API is already known-valid. Explorers that want to
// double-verify can re-fetch the cert envelope; this field is the
// summary boolean.
//
// proof_backend_id reports the chain-wide canonical proof backend
// (first entry of AllowedProofBackends). Per-block backend variance is
// not part of the chain's posture; if a chain accepts multiple
// backends, the explorer should fetch the cert envelope for the exact
// backend used.
type BlockSecurityReply struct {
	// SecurityProfileID is the wire byte of the chain's active
	// profile at the time the block was accepted. Constant for a
	// chain that has not undergone a profile migration.
	SecurityProfileID uint32 `json:"security_profile_id"`

	// SecurityProfileName mirrors ProfileReply.ProfileName.
	SecurityProfileName string `json:"security_profile_name"`

	// PulsarMSignatureValid is true iff the chain's finality scheme
	// is a Pulsar-M variant. Blocks accepted under this profile have
	// already passed the finality-signature gate at the consensus
	// layer; this field is the explorer-facing summary.
	PulsarMSignatureValid bool `json:"pulsar_m_signature_valid"`

	// ProofBackendID is the wire byte of the chain's canonical
	// proof backend (first AllowedProofBackends entry). Pre-rendered
	// here for explorer convenience.
	ProofBackendID uint32 `json:"proof_backend_id"`

	// ProofBackendName is the canonical SCREAMING_SNAKE_CASE name of
	// the chain's proof backend.
	ProofBackendName string `json:"proof_backend_name"`

	// PostQuantumEndToEnd mirrors ProfileReply.PostQuantumEndToEnd.
	// Duplicated onto the block-level reply so a single block
	// fetch carries the full E2E posture.
	PostQuantumEndToEnd bool `json:"post_quantum_end_to_end"`
}

// renderName converts an scheme String() (kebab-lowercase, e.g.
// "ml-dsa-65", "sha3-nist", "stark-fri-sha3-pq") to the
// SCREAMING_SNAKE_CASE canonical form ("ML_DSA_65", "SHA3_NIST",
// "STARK_FRI_SHA3_PQ") used on the wire.
//
// Single point of translation so the wire vocabulary stays consistent
// across every axis. Audit tooling matches on these names; renaming
// here breaks every downstream consumer, so do not.
func renderName(s string) string {
	if s == "" {
		return ""
	}
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '-':
			out[i] = '_'
		case c >= 'a' && c <= 'z':
			out[i] = c - ('a' - 'A')
		default:
			out[i] = c
		}
	}
	return string(out)
}

// hashSuiteName returns the canonical SCREAMING_SNAKE name for the
// HashSuiteID byte. Unknown bytes get the generic UNKNOWN_0x<hex> form
// so a forked profile can't silently masquerade.
func hashSuiteName(h consensusconfig.HashSuiteID) string {
	switch h {
	case consensusconfig.HashSuiteSHA3NIST:
		return "SHA3_NIST"
	case consensusconfig.HashSuiteBLAKE3Legacy:
		return "BLAKE3_LEGACY"
	default:
		return renderName(h.String())
	}
}

// proofBackendName returns the canonical SCREAMING_SNAKE name for the
// ProofBackendID byte. Mirrors hashSuiteName for the backend axis.
func proofBackendName(b consensusconfig.ProofBackendID) string {
	return renderName(b.String())
}

// contractAuthName renders the contract-auth axis. Under a strict-PQ
// profile the chain accepts ML-DSA-65 AND Z-Chain validity proofs;
// the profile pins one default ID but the second is implicitly
// admissible. Reflect that union on the wire so a dApp reading the
// RPC sees both gates it can route through.
//
// Forks that pin a different ContractAuthID get just the rendered
// name of that byte.
func contractAuthName(profile *consensusconfig.ChainSecurityProfile) string {
	if profile == nil {
		return ""
	}
	base := renderName(profile.ContractAuthID.String())
	if profile.ProfileID == uint32(consensusconfig.ProfileLuxStrictPQ) ||
		profile.ProfileID == uint32(consensusconfig.ProfileLuxFIPS) {
		// Strict-PQ + FIPS implicitly admit Z-Chain validity proofs
		// as a contract-auth primitive in addition to the raw
		// lattice scheme pinned on the profile.
		return base + "|ZCHAIN_AUTH_PROOF"
	}
	return base
}

// validatorSchemeName returns the canonical name of the identity
// scheme the chain accepts for validator registration. Identity
// scheme is the IdentitySchemeID (raw FIPS 204 ML-DSA on every
// locked profile).
func validatorSchemeName(profile *consensusconfig.ChainSecurityProfile) string {
	if profile == nil {
		return ""
	}
	return renderName(profile.IdentitySchemeID.String())
}

// isPostQuantumEndToEnd reports whether the profile satisfies the
// HIP-0078 §"E2E PQ surface" gate: every scheme axis is post-quantum
// AND every classical primitive is forbidden.
//
// This is the single boolean a dashboard / wallet / dApp reads to
// decide "am I behind a strict-PQ chain?". A profile that fails any
// axis returns false even if it satisfies the rest.
func isPostQuantumEndToEnd(profile *consensusconfig.ChainSecurityProfile) bool {
	if profile == nil {
		return false
	}
	return profile.WalletSchemeID.IsPostQuantum() &&
		profile.TxSchemeID.IsPostQuantum() &&
		profile.KeyExchangeID.IsPostQuantum() &&
		profile.HighValueKEM.IsPostQuantum() &&
		profile.RecoverySchemeID.IsPostQuantum() &&
		profile.ContractAuthID.IsPostQuantum() &&
		profile.ProofPolicyID.IsPostQuantum() &&
		profile.ForbidECDSAWallets &&
		profile.ForbidECDSAContractAuth &&
		profile.ForbidBLSContractAuth &&
		profile.ForbidClassicalKEM &&
		profile.ForbidPairings &&
		profile.ForbidKZG &&
		profile.ForbidTrustedSetup &&
		profile.ForbidClassicalSNARKs &&
		profile.RequireTypedTxAuth
}

// isLuxCanonical reports whether profile.ProfileID matches one of the
// three audit-signed-off-on Lux profiles (StrictPQ, Permissive, FIPS).
// Downstream forks that claim a different byte return false here.
func isLuxCanonical(profile *consensusconfig.ChainSecurityProfile) bool {
	if profile == nil {
		return false
	}
	switch profile.ProfileID {
	case uint32(consensusconfig.ProfileLuxStrictPQ),
		uint32(consensusconfig.ProfileLuxPermissive),
		uint32(consensusconfig.ProfileLuxFIPS):
		return true
	default:
		return false
	}
}

// isNISTFriendly reports whether the chain pins a NIST-aligned
// canonical Lux profile (StrictPQ or FIPS only — Permissive is
// canonical but allows dev backends so it does NOT carry the NIST
// posture). A profile whose HashSuiteID is SHA3_NIST but whose
// Forbid* bits are disabled (the classical-compat fork case) returns
// false: NIST alignment is a full-posture predicate, not a single-
// axis hash check.
//
// Single point of definition — service, metrics, and the banner all
// route through this predicate so the wire vocabulary stays consistent.
func isNISTFriendly(profile *consensusconfig.ChainSecurityProfile) bool {
	if profile == nil {
		return false
	}
	if profile.HashSuiteID != consensusconfig.HashSuiteSHA3NIST {
		return false
	}
	switch profile.ProfileID {
	case uint32(consensusconfig.ProfileLuxStrictPQ),
		uint32(consensusconfig.ProfileLuxFIPS):
		return true
	default:
		return false
	}
}
