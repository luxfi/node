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
	_, err := NewSchemeGate(nil, 0)
	require.ErrorIs(err, ErrSchemeGateConfig)
}

// TestSchemeGate_StrictPQ_AcceptsMatchedScheme — under a strict-PQ
// profile an ML-DSA-65 NodeID is accepted at every site at every
// height.
func TestSchemeGate_StrictPQ_AcceptsMatchedScheme(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.StrictPQ(), 0)
	require.NoError(err)

	id := ids.NodeID{0xaa, 0xbb}
	for _, site := range []string{
		"handshake", "proposer", "validator", "mempool-sender",
	} {
		for _, height := range []uint64{0, 100, 1_000_000} {
			typed, err := g.Classify(id, ids.NodeIDSchemeMLDSA65, height, site)
			require.NoError(err, "site=%s height=%d", site, height)
			require.Equal(ids.NodeIDSchemeMLDSA65, typed.Scheme)
			require.Equal(id, typed.NodeID)
		}
	}
}

// TestSchemeGate_StrictPQ_RejectsClassicalAtEveryHeight — a strict-PQ
// chain refuses a classical secp256k1 NodeID at every site and every
// height. There is no transition window in forward-only PQ mode.
func TestSchemeGate_StrictPQ_RejectsClassicalAtEveryHeight(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.StrictPQ(), 0)
	require.NoError(err)

	id := ids.NodeID{0x11, 0x22}
	for _, site := range []string{
		"handshake", "proposer", "validator", "mempool-sender",
	} {
		for _, height := range []uint64{0, 50, 1_000_000} {
			_, err := g.Classify(id, ids.NodeIDSchemeSecp256k1, height, site)
			require.ErrorIs(err, ErrSchemeGateMismatch,
				"site=%s height=%d", site, height)
		}
	}
}

// TestSchemeGate_RejectsCrossPQScheme — a chain pinning ML-DSA-65 must
// refuse an ML-DSA-87 NodeID. Cross-PQ bytes are never admissible.
func TestSchemeGate_RejectsCrossPQScheme(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.StrictPQ(), 0)
	require.NoError(err)

	_, err = g.Classify(ids.NodeID{0x33}, ids.NodeIDSchemeMLDSA87, 0, "handshake")
	require.ErrorIs(err, ErrSchemeGateMismatch)
}

// TestSchemeGate_RejectsUnknownSchemeByte — an unknown scheme byte is
// refused at every height. The PQ-only rule does not widen the byte
// set.
func TestSchemeGate_RejectsUnknownSchemeByte(t *testing.T) {
	require := require.New(t)

	g, err := NewSchemeGate(consensusconfig.StrictPQ(), 1000)
	require.NoError(err)

	id := ids.NodeID{0x99}

	_, err = g.Classify(id, ids.NodeIDScheme(0x91), 500, "handshake")
	require.ErrorIs(err, ErrSchemeGateUnknownScheme)

	_, err = g.Classify(id, ids.NodeIDScheme(0x91), 2000, "handshake")
	require.ErrorIs(err, ErrSchemeGateUnknownScheme)
}

// TestSchemeGate_PinsProfileScheme — the gate's pinned scheme reads
// through the profile's ValidatorSchemeID. A profile-byte change
// changes the gate's acceptance set without any gate-level edit.
func TestSchemeGate_PinsProfileScheme(t *testing.T) {
	require := require.New(t)

	p := consensusconfig.StrictPQ()
	g, err := NewSchemeGate(p, 0)
	require.NoError(err)

	id := ids.NodeID{0x01}

	_, err = g.Classify(id, ids.NodeIDScheme(p.ValidatorSchemeID()), 0, "handshake")
	require.NoError(err)

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

	g, err := NewSchemeGate(consensusconfig.StrictPQ(), 0)
	require.NoError(err)

	_, err = g.Classify(ids.NodeID{0x01}, ids.NodeIDSchemeSecp256k1, 1, "proposer")
	require.Error(err)
	require.Contains(err.Error(), "site=proposer")
}
