// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package auth is the canonical credential-policy gate for P-chain and
// X-chain mempool admission.
//
// Under a strict-PQ ChainSecurityProfile (RequireTypedTxAuth=true) a
// classical secp256k1 credential is not admissible unless the originator
// address is on the ClassicalCompatRegistry allow-list. The registry
// exists so a chain mid-migration can carry over a small, named set of
// legacy operators (signed by chain governance) without weakening the
// post-quantum bar for everyone else.
//
// One package, one entry point: EnforceCredentialPolicy. Callers (the
// P-chain and X-chain mempool Add hooks) invoke it once per inbound tx;
// admitting a tx that doesn't satisfy the policy returns
// ErrLegacyCredentialUnderStrictPQ so the caller can drop it with a
// specific reason.
//
// This package is intentionally small — it owns the gate, nothing else.
// The PQ tx-type variants, the mldsafx signer visitors, and the
// validator-registration changes live in their own packages; they all
// route through this gate at the mempool boundary so the rule is
// enforced exactly once.
package auth

import (
	"errors"

	"github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/utxo/secp256k1fx"
)

// ErrLegacyCredentialUnderStrictPQ is returned when a transaction's
// credentials include a secp256k1fx.Credential while the chain's
// security profile pins RequireTypedTxAuth=true and the originator
// address is NOT in the ClassicalCompatRegistry. Strict-PQ chains MUST
// drop the tx and refuse to gossip it.
var ErrLegacyCredentialUnderStrictPQ = errors.New("auth: classical secp256k1 credential refused under strict-PQ profile")

// ErrNilProfile is returned when EnforceCredentialPolicy is called with a
// nil profile. Callers are expected to wire the chain's profile into the
// mempool at construction time; a nil profile is a wiring bug, not a
// runtime condition.
var ErrNilProfile = errors.New("auth: nil ChainSecurityProfile")

// ClassicalCompatRegistry is the allow-list of addresses that may still
// sign with classical (secp256k1) credentials on a chain that otherwise
// pins RequireTypedTxAuth=true. The registry is OPT-IN: a chain that
// supplies nil refuses every classical credential outright.
//
// Implementations MUST be safe for concurrent reads. Writers are the
// chain governance pathway (a typed admin tx that mutates the registry
// atomically); this interface intentionally exposes only the read path
// so a misbehaving caller cannot widen the allow-list at mempool time.
type ClassicalCompatRegistry interface {
	// IsAllowed reports whether addr is permitted to submit a tx whose
	// credentials include a classical secp256k1fx.Credential. The
	// addr is the originator address bound to the tx (typically the
	// first signer; chain semantics decide).
	IsAllowed(addr ids.ShortID) bool
}

// EnforceCredentialPolicy is the canonical mempool gate. Returns nil iff
// the credential set is admissible under profile. Otherwise returns the
// specific error so the mempool can record the drop reason.
//
//   - profile == nil → ErrNilProfile (programmer error, fail loud).
//   - profile.RequireTypedTxAuth == false → admit unconditionally
//     (the classical-compat profile path).
//   - profile.RequireTypedTxAuth == true:
//       - every credential is a PQ credential (mldsafx.Credential or
//         any non-secp256k1fx.Credential type) → admit.
//       - at least one credential is *secp256k1fx.Credential and
//         registry == nil → refuse (no allow-list configured).
//       - at least one credential is *secp256k1fx.Credential and
//         registry.IsAllowed(originator) == false → refuse.
//       - otherwise → admit.
//
// The `originator` argument is the address bound to the tx by the chain
// (P-chain: first input's address; X-chain: first input owner). Callers
// MUST resolve it before invoking; this function does not parse tx
// structure.
func EnforceCredentialPolicy(
	creds []verify.Verifiable,
	profile *config.ChainSecurityProfile,
	registry ClassicalCompatRegistry,
	originator ids.ShortID,
) error {
	if profile == nil {
		return ErrNilProfile
	}
	if !profile.RequireTypedTxAuth {
		return nil
	}
	for _, c := range creds {
		if _, isClassical := c.(*secp256k1fx.Credential); !isClassical {
			continue
		}
		if registry == nil {
			return ErrLegacyCredentialUnderStrictPQ
		}
		if !registry.IsAllowed(originator) {
			return ErrLegacyCredentialUnderStrictPQ
		}
	}
	return nil
}

// staticRegistry is a frozen ClassicalCompatRegistry constructed from a
// fixed set of addresses. Useful in tests and for chains whose genesis
// hard-codes the allow-list before the governance path is online.
type staticRegistry struct {
	allowed map[ids.ShortID]struct{}
}

// NewStaticClassicalCompatRegistry returns a ClassicalCompatRegistry
// containing exactly the supplied addresses. The slice is copied; the
// caller is free to reuse the input. The returned registry is safe for
// concurrent reads.
func NewStaticClassicalCompatRegistry(addrs []ids.ShortID) ClassicalCompatRegistry {
	r := &staticRegistry{allowed: make(map[ids.ShortID]struct{}, len(addrs))}
	for _, a := range addrs {
		r.allowed[a] = struct{}{}
	}
	return r
}

// IsAllowed reports membership in the static allow-list.
func (r *staticRegistry) IsAllowed(addr ids.ShortID) bool {
	if r == nil {
		return false
	}
	_, ok := r.allowed[addr]
	return ok
}
