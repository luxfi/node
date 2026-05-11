// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

// TestSchemeGate_NewSchemeGate_RejectsNilProfile — construction is
// fail-closed: a nil profile produces ErrSchemeGateConfig and never a
// half-formed gate.
func TestSchemeGate_NewSchemeGate_RejectsNilProfile(t *testing.T) {
	require := require.New(t)
	_, err := NewSchemeGate(nil, false, 0)
	require.ErrorIs(err, ErrSchemeGateConfig)
}

// TestSchemeGate_StrictPQ_AcceptsMatchedScheme — under a strict-PQ
// profile with no transition window, an ML-DSA-65 NodeID is accepted
// regardless of the site tag passed in.
func TestSchemeGate_StrictPQ_AcceptsMatchedScheme(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.LuxStrictPQ(), false, 0)
	require.NoError(err)

	id := ids.NodeID{0xaa, 0xbb}
	for _, site := range []string{
		"handshake", "proposer", "validator", "mempool-sender",
	} {
		typed, err := g.Classify(id, ids.NodeIDSchemeMLDSA65, 100, site)
		require.NoError(err, "site=%s", site)
		require.Equal(ids.NodeIDSchemeMLDSA65, typed.Scheme)
		require.Equal(id, typed.NodeID)
	}
}

// TestSchemeGate_StrictPQ_PostActivation_RejectsClassical — once
// past the activation height a strict-PQ chain refuses a classical
// NodeID at every site, even when ClassicalCompatUnsafe=true.
func TestSchemeGate_StrictPQ_PostActivation_RejectsClassical(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.LuxStrictPQ(), true, 50)
	require.NoError(err)

	id := ids.NodeID{0x11, 0x22}
	for _, site := range []string{
		"handshake", "proposer", "validator", "mempool-sender",
	} {
		_, err := g.Classify(id, ids.NodeIDSchemeSecp256k1, 100 /* past activation */, site)
		require.ErrorIs(err, ErrSchemeGateMismatch, "site=%s", site)
	}
}

// TestSchemeGate_StrictPQ_PreActivation_AcceptsBothSchemes — during
// the transition window the strict-PQ gate accepts BOTH the pinned PQ
// scheme and the classical scheme. The window is the migration path.
func TestSchemeGate_StrictPQ_PreActivation_AcceptsBothSchemes(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.LuxStrictPQ(), false, 100)
	require.NoError(err)

	id := ids.NodeID{0x33, 0x44}

	// Pinned PQ scheme always accepted, including in the window.
	typed, err := g.Classify(id, ids.NodeIDSchemeMLDSA65, 50, "handshake")
	require.NoError(err)
	require.Equal(ids.NodeIDSchemeMLDSA65, typed.Scheme)

	// Classical accepted in the window even on strict-PQ.
	typed, err = g.Classify(id, ids.NodeIDSchemeSecp256k1, 50, "handshake")
	require.NoError(err)
	require.Equal(ids.NodeIDSchemeSecp256k1, typed.Scheme)

	// Cross-PQ scheme (ML-DSA-87 on a chain pinning ML-DSA-65) is
	// still refused inside the window: the transition window admits
	// the named-classical fallback, not "any other PQ byte".
	_, err = g.Classify(id, ids.NodeIDSchemeMLDSA87, 50, "handshake")
	require.ErrorIs(err, ErrSchemeGateMismatch)
}

// TestSchemeGate_StrictPQ_PreActivation_RejectsClassicalAfterCutover
// — exactly at ActivationHeight the transition window closes; the
// classical scheme is refused at that height and beyond.
func TestSchemeGate_StrictPQ_PreActivation_RejectsClassicalAfterCutover(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.LuxStrictPQ(), false, 100)
	require.NoError(err)

	id := ids.NodeID{0x55}

	// At ActivationHeight: closed.
	_, err = g.Classify(id, ids.NodeIDSchemeSecp256k1, 100, "handshake")
	require.ErrorIs(err, ErrSchemeGateMismatch)

	// Past ActivationHeight: closed.
	_, err = g.Classify(id, ids.NodeIDSchemeSecp256k1, 101, "handshake")
	require.ErrorIs(err, ErrSchemeGateMismatch)
}

// TestSchemeGate_Permissive_AcceptsClassicalUnderUnsafeFlag — a
// permissive (testnet/devnet) profile with the operator-set unsafe
// flag accepts classical NodeIDs at every height. Without the flag,
// classical is refused even on permissive.
func TestSchemeGate_Permissive_AcceptsClassicalUnderUnsafeFlag(t *testing.T) {
	require := require.New(t)

	id := ids.NodeID{0x77}

	// With the flag: classical accepted post-activation on permissive.
	gOn, err := NewSchemeGate(consensusconfig.LuxPermissive(), true, 0)
	require.NoError(err)
	_, err = gOn.Classify(id, ids.NodeIDSchemeSecp256k1, 1000, "handshake")
	require.NoError(err)

	// Without the flag: classical refused even on permissive.
	gOff, err := NewSchemeGate(consensusconfig.LuxPermissive(), false, 0)
	require.NoError(err)
	_, err = gOff.Classify(id, ids.NodeIDSchemeSecp256k1, 1000, "handshake")
	require.ErrorIs(err, ErrSchemeGateMismatch)
}

// TestSchemeGate_RejectsUnknownSchemeByte — an unknown scheme byte is
// refused at every height, regardless of profile class or operator flag.
// The transition window does NOT widen the byte set.
func TestSchemeGate_RejectsUnknownSchemeByte(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.LuxStrictPQ(), true, 1000)
	require.NoError(err)

	id := ids.NodeID{0x99}

	// Inside the transition window.
	_, err = g.Classify(id, ids.NodeIDScheme(0x91), 500, "handshake")
	require.ErrorIs(err, ErrSchemeGateUnknownScheme)

	// Past the activation height.
	_, err = g.Classify(id, ids.NodeIDScheme(0x91), 2000, "handshake")
	require.ErrorIs(err, ErrSchemeGateUnknownScheme)
}

// TestSchemeGate_PinsProfileScheme — the gate's pinned scheme reads
// through the profile's ValidatorSchemeID. A profile-byte change
// changes the gate's acceptance set without any gate-level edit.
func TestSchemeGate_PinsProfileScheme(t *testing.T) {
	require := require.New(t)

	p := consensusconfig.LuxStrictPQ()
	g, err := NewSchemeGate(p, false, 0)
	require.NoError(err)

	id := ids.NodeID{0x01}

	// Pinned scheme accepted.
	_, err = g.Classify(id, ids.NodeIDScheme(p.ValidatorSchemeID()), 0, "handshake")
	require.NoError(err)

	// Cross-PQ scheme refused.
	otherPQ := ids.NodeIDSchemeMLDSA87
	if p.ValidatorSchemeID() == consensusconfig.SigSchemeMLDSA87 {
		otherPQ = ids.NodeIDSchemeMLDSA65
	}
	_, err = g.Classify(id, otherPQ, 0, "handshake")
	require.ErrorIs(err, ErrSchemeGateMismatch)
}

// TestSchemeGate_SiteTagIncludedInError — the site tag passed to
// Classify must appear in the returned error so a log reader can
// identify which boundary refused.
func TestSchemeGate_SiteTagIncludedInError(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.LuxStrictPQ(), false, 0)
	require.NoError(err)

	_, err = g.Classify(ids.NodeID{0x01}, ids.NodeIDSchemeSecp256k1, 1, "proposer")
	require.Error(err)
	require.Contains(err.Error(), "site=proposer")
}
