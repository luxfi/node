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
// The hardfork-style activation block lets a chain accept BOTH classical
// and ML-DSA NodeIDs during a transition window, then switch the
// strict-PQ profile to ML-DSA-only at a configured activation block.

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
// ChainSecurityProfile and the operator's classical-compat flag, and
// pinned for the chain's lifetime (no re-derivation in the hot path).
//
// The migration window is expressed as an activation height: until
// ActivationHeight, the gate accepts BOTH the pinned PQ scheme and the
// classical secp256k1 scheme (the chain is in "mixed-scheme" mode);
// from ActivationHeight onward, strict-PQ chains refuse classical
// regardless of the operator flag.
//
// ActivationHeight == 0 means "the gate is already active at genesis"
// — the right value for a fresh strict-PQ chain. A chain migrating
// from classical to PQ sets ActivationHeight to the block at which the
// hardfork takes effect.
type SchemeGate struct {
	// Profile is the chain's locked ChainSecurityProfile. The pinned
	// ValidatorSchemeID is read through profile.ValidatorSchemeID().
	Profile *consensusconfig.ChainSecurityProfile

	// ClassicalCompatUnsafe mirrors the operator's
	// LUX_CLASSICAL_COMPAT_UNSAFE knob. Strict-PQ profiles refuse
	// classical schemes regardless of this flag (defence in depth);
	// permissive profiles honour it.
	ClassicalCompatUnsafe bool

	// ActivationHeight is the block height at which the gate stops
	// accepting classical NodeIDs on strict-PQ chains. Before
	// ActivationHeight, the gate is in "transition" mode and accepts
	// both. At or after ActivationHeight, the strict-PQ rule applies.
	// Zero means "no transition window — strict from genesis".
	ActivationHeight uint64
}

// NewSchemeGate constructs a SchemeGate. The profile pointer MUST be
// non-nil and MUST have already passed Validate at chain-bootstrap;
// the gate does not re-validate the profile on each call.
func NewSchemeGate(
	profile *consensusconfig.ChainSecurityProfile,
	classicalCompatUnsafe bool,
	activationHeight uint64,
) (*SchemeGate, error) {
	if profile == nil {
		return nil, fmt.Errorf("%w: profile is nil", ErrSchemeGateConfig)
	}
	return &SchemeGate{
		Profile:               profile,
		ClassicalCompatUnsafe: classicalCompatUnsafe,
		ActivationHeight:      activationHeight,
	}, nil
}

// Classify is the entry point for every cross-axis check. It takes the
// bare NodeID, the scheme byte derived by the caller (handshake / proposer
// / validator / mempool), the block height we are mounting against, and
// a site tag for the log line; it returns a TypedNodeID stamped with the
// scheme byte once the chain's policy admits the pair.
//
// The classical-secp256k1 input is what the existing TLS upgrader
// produces today; the gate stamps it with the matching scheme byte and
// runs the cross-axis check. Strict-PQ chains past their activation
// height refuse the classical NodeID here; that is the migration
// enforcement point.
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

	// Transition window: accept the pinned PQ byte AND the named
	// classical byte. Any byte outside that pair (e.g. ML-DSA-87 on a
	// chain that pins ML-DSA-65) is still refused as a cross-scheme
	// mismatch.
	if height < g.ActivationHeight {
		pinned := ids.NodeIDScheme(g.Profile.ValidatorSchemeID())
		if derivedScheme != pinned && derivedScheme != ids.NodeIDSchemeSecp256k1 {
			return ids.TypedNodeID{}, fmt.Errorf("%w: site=%s presented=%s pinned=%s height=%d (transition)",
				ErrSchemeGateMismatch,
				site, derivedScheme, pinned, height)
		}
		typed, err := ids.NewTypedNodeID(derivedScheme, nodeID)
		if err != nil {
			return ids.TypedNodeID{}, fmt.Errorf("%w: site=%s: %w",
				ErrSchemeGateMismatch, site, err)
		}
		return typed, nil
	}

	// Post-activation: defer to the profile's cross-axis check. The
	// profile's strict-PQ class refuses classical even under the
	// operator flag.
	presentedSig := consensusconfig.SigSchemeID(derivedScheme)
	if err := g.Profile.AcceptsValidatorScheme(
		presentedSig, g.ClassicalCompatUnsafe,
	); err != nil {
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
