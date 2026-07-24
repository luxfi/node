// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// kms_native.go — native-ZAP staking-identity custody against Lux KMS.
//
// This is the ONE way a luxd node loads (and optionally saves) its staking
// identity from KMS. It supersedes the legacy Infisical-style REST client: the
// current KMS (github.com/luxfi/kms v1.12.x) authenticates every secret opcode
// with an ML-DSA-65 signed envelope, so a bare HTTP GET no longer works.
//
// Composition (no logic braided — each layer owns one concern):
//
//	keys.NewServiceIdentity      → the ML-DSA-65 envelope-auth credential
//	kms/pkg/mnemonic.LoadFromKMS → fetch + BIP-39-validate the deploy mnemonic
//	keys.DeriveValidatorFromMnemonic + keys.DeriveValidatorPQ
//	                             → deterministic classical + strict-PQ keys
//	kms/pkg/zapclient            → GetAt/PutAt of the custody blob (envelope-auth)
//	keys.StakingIdentity codec   → the portable, integrity-framed blob
//
// Two modes, mutually exclusive, both fail CLOSED on any KMS/auth/read error —
// a node NEVER silently falls back to an insecure local key:
//
//	DERIVE  (MnemonicPath set): fetch the one deploy mnemonic, derive this
//	        node's identity at ValidatorIndex. REQUIRES strict-PQ — only the
//	        ML-DSA-65 NodeID is deterministic across boots, so a re-derived
//	        node recovers the SAME NodeID with nothing custodied (the canonical
//	        "one seed, N paths" Lux model). The classical ECDSA staking cert is
//	        signed non-deterministically (Go's hedged signer), so a cert-derived
//	        classical NodeID would churn every boot — classical DERIVE is
//	        therefore refused; use CUSTODY for a stable classical NodeID. Under
//	        strict-PQ DERIVE the transport-only TLS cert still churns, but the
//	        NodeID is anchored on the deterministic ML-DSA-65 key, so it is stable.
//	CUSTODY (IdentityPath set): GetAt this node's stored StakingIdentity blob.
//	        If absent AND Save is set (operator authority required — writes are
//	        operator-gated in KMS), mint a fresh identity and PutAt it. For
//	        nodes that hold a unique, non-derived key. The blob is bound to an
//	        operator-supplied node label so a second node pointed at the same
//	        path is refused (equivocation tripwire; server-side path authz is
//	        the primary control).
package staking

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/luxfi/keys"
	"github.com/luxfi/kms/pkg/envelope"
	kmsmnemonic "github.com/luxfi/kms/pkg/mnemonic"
	"github.com/luxfi/kms/pkg/zapclient"
	"github.com/luxfi/log"
)

// NativeKMSConfig parameterises a single staking-identity resolution.
type NativeKMSConfig struct {
	// Endpoint is the KMS ZAP peer (host:port). Required.
	Endpoint string
	// Env is the KMS environment field ("dev" | "test" | "main"). Required —
	// env is a field on the secret, never a hostname.
	Env string

	// MnemonicPath selects DERIVE mode: the KMS path of the deploy mnemonic
	// (e.g. "providers/lux/deploy-mnemonic"). Mutually exclusive with
	// IdentityPath.
	MnemonicPath string
	// ValidatorIndex is the BIP-44 index this node derives at (DERIVE mode).
	ValidatorIndex uint32

	// IdentityPath selects CUSTODY mode: the KMS path of THIS node's stored
	// StakingIdentity blob. Mutually exclusive with MnemonicPath.
	IdentityPath string
	// Save, in CUSTODY mode, mints + PutAt-saves a fresh identity when none is
	// stored. Requires operator authority on the KMS principal.
	Save bool

	// StrictPQ additionally derives/mints the FIPS 204 ML-DSA-65 + FIPS 203
	// ML-KEM-768 materials so the node runs a strict-PQ NodeID. When false,
	// only classical (TLS transport + BLS consensus) materials are produced —
	// matching the node's file-path model where PQ is an explicit opt-in.
	// MANDATORY in DERIVE mode: classical DERIVE churns the NodeID every boot
	// (see resolveByDerivation), so DERIVE requires StrictPQ.
	StrictPQ bool

	// BootstrapMnemonic seeds the ML-DSA-65 envelope-auth ServiceIdentity used
	// to sign KMS requests. Empty → the dial is anonymous, accepted only by a
	// KMS whose consensus-auth gate is disabled (dev/loopback) or trusted at
	// the network boundary (NetworkPolicy). Production KMS peers REQUIRE it.
	BootstrapMnemonic string

	// AllowEnvMnemonic permits the MNEMONIC environment variable to supersede
	// the configured KMS endpoint in DERIVE mode. This is a DEV-ONLY seam: a
	// loopback endpoint implies it too. With a real (non-loopback) endpoint and
	// this false, a configured endpoint FORCES the authenticated KMS read and a
	// stray MNEMONIC env is ignored (with a loud WARN) — so an on-box env var
	// cannot silently redirect the node's whole identity away from KMS.
	AllowEnvMnemonic bool

	// NodeLabel is an operator-supplied label (hostname, fleet index, …) bound
	// into a CUSTODY identity blob and re-checked on load. It is the
	// equivocation tripwire: a node whose configured label does not match the
	// stored blob's label refuses to boot, so two nodes accidentally sharing
	// one IdentityPath cannot both adopt the same validator key. Empty forgoes
	// the tripwire (a WARN is emitted); server-side path-scoped write authz
	// remains the primary control and MUST be confirmed on the KMS principal.
	NodeLabel string
}

// ErrKMSModeAmbiguous is returned when neither or both of MnemonicPath /
// IdentityPath are set — the two modes are mutually exclusive and one is
// required.
var ErrKMSModeAmbiguous = errors.New("staking: exactly one of KMS mnemonic-path or identity-path must be set")

// bootstrapServicePath is the stable service path under which the node's KMS
// envelope-auth identity is derived. Pinned so the same bootstrap mnemonic
// yields the same auth NodeID across pods and reboots.
const bootstrapServicePath = "luxd/staking-bootstrap"

// ResolveStakingIdentity loads (or, in custody mode with Save, creates) this
// node's complete staking identity from KMS. Fails CLOSED on any error.
//
// The returned *keys.StakingIdentity owns live secret material; the caller
// MUST Wipe() it once the typed staking keys have been parsed out of it.
func ResolveStakingIdentity(ctx context.Context, cfg NativeKMSConfig) (*keys.StakingIdentity, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("staking: KMS endpoint is required")
	}
	if cfg.Env == "" {
		return nil, errors.New("staking: KMS env is required")
	}
	haveMnemonic := strings.TrimSpace(cfg.MnemonicPath) != ""
	haveIdentity := strings.TrimSpace(cfg.IdentityPath) != ""
	if haveMnemonic == haveIdentity { // neither or both
		return nil, ErrKMSModeAmbiguous
	}

	// Build the envelope-auth identity (may be nil for dev/loopback KMS).
	authID, err := bootstrapIdentity(cfg.BootstrapMnemonic)
	if err != nil {
		return nil, err
	}
	if authID != nil {
		defer authID.Wipe()
	}

	if haveMnemonic {
		return resolveByDerivation(ctx, cfg, authID)
	}
	return resolveByCustody(ctx, cfg, authID)
}

// resolveByDerivation implements DERIVE mode: fetch the deploy mnemonic, derive
// this node's identity deterministically at ValidatorIndex.
//
// Fail-closed on two footguns:
//
//	M1 — classical DERIVE is REFUSED. DeriveValidatorFromMnemonic derives the
//	P-256 key deterministically, but the staking CERTIFICATE is ECDSA-signed
//	non-deterministically (Go's hedged signer), so its cert-derived NodeID
//	changes on every boot. A DERIVE node with a churning NodeID is not a stable
//	validator. Only strict-PQ DERIVE is stable (the ML-DSA-65 NodeID is
//	deterministic). We steer classical operators to CUSTODY (store/reload the
//	exact cert) or to strict-PQ DERIVE.
//
//	H1 — a configured production endpoint FORCES the authenticated KMS read.
//	kmsmnemonic.Load short-circuits on the MNEMONIC env before ever dialing
//	KMS; if honored unconditionally, an on-box env var would silently derive the
//	node's whole identity with no KMS auth and no distinguishing log. We honor
//	the env seam ONLY for dev — an explicit AllowEnvMnemonic or a loopback
//	endpoint — and otherwise call LoadFromKMS, which never short-circuits.
func resolveByDerivation(ctx context.Context, cfg NativeKMSConfig, authID *keys.ServiceIdentity) (*keys.StakingIdentity, error) {
	if !cfg.StrictPQ {
		return nil, errors.New("staking: classical DERIVE produces a NEW NodeID on every boot " +
			"(the ECDSA staking certificate is signed non-deterministically) — refusing. Set " +
			"--staking-kms-strict-pq for a deterministic ML-DSA-65 NodeID, or use CUSTODY mode " +
			"(--staking-kms-identity-path) to store and reload a stable classical certificate")
	}

	mnemonic, err := loadDeployMnemonic(ctx, cfg, authID)
	if err != nil {
		return nil, fmt.Errorf("staking: load deploy mnemonic from KMS: %w", err)
	}
	// The mnemonic is a root secret; keep it on the stack, wipe when done.
	defer wipeString(&mnemonic)

	vk, err := keys.DeriveValidatorFromMnemonic(mnemonic, cfg.ValidatorIndex)
	if err != nil {
		return nil, fmt.Errorf("staking: derive validator key: %w", err)
	}
	defer wipeValidatorKey(vk)

	pq, err := keys.DeriveValidatorPQ(mnemonic, cfg.ValidatorIndex)
	if err != nil {
		return nil, fmt.Errorf("staking: derive strict-PQ validator key: %w", err)
	}
	defer pq.Wipe()
	return keys.StakingIdentityFromValidatorKey(vk, pq), nil
}

// loadDeployMnemonic implements the H1 precedence: honor the MNEMONIC env
// short-circuit ONLY when env override is allowed (dev-only: explicit
// AllowEnvMnemonic or a loopback endpoint); otherwise force the authenticated
// KMS read via LoadFromKMS so a configured endpoint cannot be silently
// superseded by an on-box env var. Either way a stray env with a configured
// endpoint is surfaced with a loud WARN.
func loadDeployMnemonic(ctx context.Context, cfg NativeKMSConfig, authID *keys.ServiceIdentity) (string, error) {
	envSet := strings.TrimSpace(os.Getenv("MNEMONIC")) != ""
	if cfg.AllowEnvMnemonic || isLoopbackEndpoint(cfg.Endpoint) {
		if envSet {
			log.Warn("staking: MNEMONIC env supersedes the configured KMS endpoint — DEV-ONLY path (loopback endpoint or --staking-allow-env-mnemonic)",
				"endpoint", cfg.Endpoint)
		}
		return kmsmnemonic.Load(ctx, cfg.Endpoint, cfg.Env, cfg.MnemonicPath, authID)
	}
	if envSet {
		log.Warn("staking: MNEMONIC env present but IGNORED — a configured KMS endpoint forces the authenticated KMS read (set --staking-allow-env-mnemonic to permit the env seam in dev)",
			"endpoint", cfg.Endpoint)
	}
	return kmsmnemonic.LoadFromKMS(ctx, cfg.Endpoint, cfg.Env, cfg.MnemonicPath, authID)
}

// isLoopbackEndpoint reports whether the KMS endpoint host is a loopback
// address or "localhost" — the dev/test signal under which the MNEMONIC env
// seam is permitted without an explicit flag. Anything else is production.
func isLoopbackEndpoint(endpoint string) bool {
	host := strings.TrimSpace(endpoint)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// resolveByCustody implements CUSTODY mode: GetAt the stored blob; when absent
// and Save is set, mint fresh + PutAt.
func resolveByCustody(ctx context.Context, cfg NativeKMSConfig, authID *keys.ServiceIdentity) (*keys.StakingIdentity, error) {
	client, err := dialCustody(ctx, cfg.Endpoint, authID)
	if err != nil {
		return nil, fmt.Errorf("staking: dial KMS: %w", err)
	}
	defer client.Close()

	dir, name := kmsmnemonic.SplitSecretPath(cfg.IdentityPath)
	raw, err := client.GetAt(ctx, dir, name, cfg.Env)
	switch {
	case err == nil:
		id, uerr := keys.UnmarshalStakingIdentity([]byte(raw))
		if uerr != nil {
			return nil, fmt.Errorf("staking: decode stored identity (path=%q): %w", cfg.IdentityPath, uerr)
		}
		// L3: refuse a blob minted for a different node (equivocation tripwire).
		if verr := verifyNodeLabel(cfg, id); verr != nil {
			id.Wipe()
			return nil, verr
		}
		return id, nil
	case errors.Is(err, zapclient.ErrNotFound):
		if !cfg.Save {
			return nil, fmt.Errorf("staking: no identity at KMS path %q and --staking-kms-save not set (refusing to run without a stable NodeID)", cfg.IdentityPath)
		}
		return mintAndSave(ctx, client, cfg, dir, name)
	default:
		return nil, fmt.Errorf("staking: read identity from KMS (path=%q): %w", cfg.IdentityPath, err)
	}
}

// mintAndSave creates a fresh identity and persists it. PutAt requires operator
// authority; a forbidden write fails closed with the KMS error surfaced.
func mintAndSave(ctx context.Context, client *zapclient.Client, cfg NativeKMSConfig, dir, name string) (*keys.StakingIdentity, error) {
	vk, err := keys.GenerateValidatorKey()
	if err != nil {
		return nil, fmt.Errorf("staking: generate classical key: %w", err)
	}
	defer wipeValidatorKey(vk)

	var pq *keys.PQValidatorKey
	if cfg.StrictPQ {
		pq, err = keys.GenerateValidatorPQ()
		if err != nil {
			return nil, fmt.Errorf("staking: generate strict-PQ key: %w", err)
		}
		defer pq.Wipe()
	}
	id := keys.StakingIdentityFromValidatorKey(vk, pq)
	// L3: bind the fresh identity to this node's operator-supplied label so a
	// second node pointed at the same IdentityPath is refused on load.
	if label := strings.TrimSpace(cfg.NodeLabel); label != "" {
		id.NodeLabel = []byte(label)
	} else {
		log.Warn("staking: minting a CUSTODY identity with no --staking-kms-node-label — a second node sharing this path could adopt it (equivocation); server-side path authz must gate writes",
			"path", cfg.IdentityPath)
	}

	blob, err := id.Marshal()
	if err != nil {
		id.Wipe()
		return nil, fmt.Errorf("staking: marshal identity: %w", err)
	}
	// PutAt base64-frames on the wire; the blob is binary-safe.
	if perr := client.PutAt(ctx, dir, name, cfg.Env, string(blob)); perr != nil {
		id.Wipe()
		zeroBytes(blob)
		return nil, fmt.Errorf("staking: save identity to KMS (path=%q): %w", cfg.IdentityPath, perr)
	}
	zeroBytes(blob)
	return id, nil
}

// verifyNodeLabel enforces the L3 equivocation tripwire on load: a stored
// custody identity is bound to the node label it was minted under, and a node
// whose configured label differs refuses to adopt it. This catches two nodes
// accidentally pointed at the same KMS identity path — which would let both
// sign as the same validator (equivocation, slashable). Server-side
// path-scoped write authorization is the PRIMARY control; this is defense in
// depth. The label is not secret, so a plain comparison is used.
func verifyNodeLabel(cfg NativeKMSConfig, id *keys.StakingIdentity) error {
	want := strings.TrimSpace(cfg.NodeLabel)
	got := strings.TrimSpace(string(id.NodeLabel))
	switch {
	case want == "" && got == "":
		log.Warn("staking: CUSTODY identity has no node-label binding — set --staking-kms-node-label so a second node cannot silently adopt this identity (equivocation tripwire); server-side path authz remains the primary control",
			"path", cfg.IdentityPath)
		return nil
	case want == got:
		return nil
	default:
		return fmt.Errorf("staking: stored identity at %q is bound to node label %q, not this node's %q — refusing (two nodes must not share one KMS identity path; confirm server-side path-scoped write authz)",
			cfg.IdentityPath, got, want)
	}
}

// dialCustody brings up an envelope-authenticated ZAP client to KMS. The
// custody blob carries live validator secrets, so the dial requires an
// established AEAD session (RequireSession) — it fails closed rather than
// transmit or accept the identity over a plaintext channel that an on-path
// attacker could have downgraded.
func dialCustody(ctx context.Context, addr string, authID *keys.ServiceIdentity) (*zapclient.Client, error) {
	kcfg := zapclient.Config{PeerAddr: addr, RequireSession: true}
	if authID != nil {
		kcfg.IdentityHeader = envelope.IdentityHeader{
			NodeID:      authID.NodeID,
			FullDigest:  authID.FullDigest,
			ServicePath: authID.ServicePath,
			PublicKey:   authID.PublicKey,
		}
		kcfg.Signer = authID
	}
	return zapclient.DialWithConfig(ctx, kcfg)
}

// bootstrapIdentity derives the ML-DSA-65 envelope-auth ServiceIdentity from
// the bootstrap mnemonic. Returns (nil, nil) when no mnemonic is supplied — an
// anonymous dial, accepted only by dev/loopback or network-boundary-trusted KMS.
func bootstrapIdentity(mnemonic string) (*keys.ServiceIdentity, error) {
	mnemonic = strings.TrimSpace(mnemonic)
	if mnemonic == "" {
		return nil, nil
	}
	id, err := keys.NewServiceIdentity(mnemonic, bootstrapServicePath)
	if err != nil {
		return nil, fmt.Errorf("staking: build KMS bootstrap identity: %w", err)
	}
	return id, nil
}

// wipeValidatorKey zeroes the secret components of a classical ValidatorKey.
func wipeValidatorKey(vk *keys.ValidatorKey) {
	if vk == nil {
		return
	}
	zeroBytes(vk.StakerKey)
	zeroBytes(vk.BLSSecretKey)
	zeroBytes(vk.ECPrivateKey)
}

// wipeString overwrites the backing bytes of a string best-effort, then clears
// the header. Go strings are immutable at the language level; this reaches the
// backing array via an unsafe-free copy-out is not possible, so we simply drop
// the reference. Documented as best-effort — the authoritative secret hygiene
// is that the mnemonic is never written to disk or logs.
func wipeString(s *string) {
	*s = ""
}

// zeroBytes overwrites a byte slice in place (best-effort).
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
