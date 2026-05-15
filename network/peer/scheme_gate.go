// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// scheme_gate.go — single cross-axis gate between an incoming peer's
// NodeIDScheme byte and the chain's pinned ValidatorSchemeID. The gate
// is the primitive; callers that turn a wire NodeID into a "this peer
// is admissible on this chain" decision call Classify and pass the
// site name so the refused-by log line names which boundary refused.
//
// Sites that funnel through this gate:
//
//	"handshake"      — peer TLS upgrade (network/peer/upgrader.go)
//	"proposer"       — proposervm block proposer attribution
//	"validator"      — platformvm validator registration
//	"mempool-sender" — mempool signed-tx submission
//
// Forward-only PQ: there is no transition window and no classical
// fallback. Every site funnels through Classify which admits only the
// pinned PQ NodeIDScheme byte (ML-DSA-65 today); anything else — a
// classical secp256k1 byte, a cross-PQ scheme like ML-DSA-87 on a chain
// pinning ML-DSA-65, or an unknown byte — is refused at the gate.

package peer

import (
	"errors"
	"fmt"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
)

// SchemeGate is the chain-scoped policy object that decides whether a
// peer's NodeIDScheme is admissible under the chain's pinned profile.
// One SchemeGate per chain; created at chain-bootstrap from the chain's
// ChainSecurityProfile and pinned for the chain's lifetime (no
// re-derivation in the hot path).
//
// Forward-only PQ: the gate has no transition window and no operator
// classical-compat escape hatch. Every Classify call admits only the
// pinned PQ scheme byte; classical secp256k1 NodeIDs and cross-PQ
// bytes are refused at every height.
type SchemeGate struct {
	// Profile is the chain's locked ChainSecurityProfile. The pinned
	// ValidatorSchemeID is read through profile.ValidatorSchemeID().
	Profile *consensusconfig.ChainSecurityProfile

	// ActivationHeight is preserved for symmetry with the per-chain
	// fork-height bookkeeping; the gate's policy is the same on both
	// sides of it (PQ-only). Reserved for downstream callers that want
	// a single field to log "where strict-PQ took effect" alongside
	// other hardfork heights — the gate itself does not branch on it.
	ActivationHeight uint64
}

// NewSchemeGate constructs a SchemeGate. The profile pointer MUST be
// non-nil and MUST have already passed Validate at chain-bootstrap;
// the gate does not re-validate the profile on each call.
func NewSchemeGate(
	profile *consensusconfig.ChainSecurityProfile,
	activationHeight uint64,
) (*SchemeGate, error) {
	if profile == nil {
		return nil, fmt.Errorf("%w: profile is nil", ErrSchemeGateConfig)
	}
	return &SchemeGate{
		Profile:          profile,
		ActivationHeight: activationHeight,
	}, nil
}

// Classify is the entry point for every cross-axis check. It takes the
// bare NodeID, the scheme byte derived by the caller (handshake / proposer
// / validator / mempool), the block height we are mounting against, and
// a site tag for the log line; it returns a TypedNodeID stamped with the
// scheme byte once the chain's policy admits the pair.
//
// Forward-only PQ: only the pinned PQ scheme byte is admitted. A
// classical secp256k1 byte, a cross-PQ byte (ML-DSA-87 on a chain
// pinning ML-DSA-65), or an unknown byte is refused at every height.
//
// site is a free-form tag ("handshake", "proposer", "validator",
// "mempool-sender" are the conventional values) included verbatim in
// the refused-by error so a log reader knows which boundary refused.
func (g *SchemeGate) Classify(
	nodeID ids.NodeID,
	derivedScheme ids.NodeIDScheme,
	height uint64,
	site string,
) (ids.TypedNodeID, error) {
	if !derivedScheme.IsKnown() {
		return ids.TypedNodeID{}, fmt.Errorf("%w: site=%s scheme=0x%02x",
			ErrSchemeGateUnknownScheme, site, byte(derivedScheme))
	}

	// PQ-only: defer to the profile's cross-axis check with the
	// classical-compat escape hatch pinned off. The profile refuses
	// classical schemes; this gate refuses them at every height.
	presentedSig := consensusconfig.SigSchemeID(derivedScheme)
	if err := g.Profile.AcceptsValidatorScheme(presentedSig, false); err != nil {
		return ids.TypedNodeID{}, fmt.Errorf("%w: site=%s height=%d: %w",
			ErrSchemeGateMismatch, site, height, err)
	}

	typed, err := ids.NewTypedNodeID(derivedScheme, nodeID)
	if err != nil {
		return ids.TypedNodeID{}, fmt.Errorf("%w: site=%s: %w",
			ErrSchemeGateMismatch, site, err)
	}
	return typed, nil
}

// Typed validation errors. Distinct from the consensus / ids errors so
// a log reader can tell exactly which boundary refused.
var (
	// ErrSchemeGateConfig — the gate could not be constructed
	// (nil profile, invalid configuration).
	ErrSchemeGateConfig = errors.New("peer: SchemeGate misconfigured")

	// ErrSchemeGateMismatch — a peer / proposer / validator /
	// mempool sender presented a NodeIDScheme byte the chain's
	// SchemeGate refuses. Wraps the underlying mismatch reason from
	// consensus/config.AcceptsValidatorScheme or ids.NewTypedNodeID.
	ErrSchemeGateMismatch = errors.New("peer: NodeIDScheme refused by SchemeGate")

	// ErrSchemeGateUnknownScheme — a wire input named a NodeIDScheme
	// byte this build does not understand. Always refused, regardless
	// of profile class or activation height.
	ErrSchemeGateUnknownScheme = errors.New("peer: NodeIDScheme byte is unknown")
)
