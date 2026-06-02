// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// deriveStakingNodeID returns the canonical node-identity NodeID for an
// ML-DSA-65 public key under the primary-identity domain (ids.Empty) — the
// exact derivation node.Node uses for MyNodeID via
// StakingConfig.DeriveNodeID(ids.Empty). A peer that proves possession of
// this key MUST present exactly this NodeID.
func deriveStakingNodeID(t *testing.T, li *LocalIdentity) ids.NodeID {
	t.Helper()
	nid, _, err := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, packMLDSAPub(li.Public))
	require.NoError(t, err)
	return nid
}

func newAdoptTestPeer(tlsNodeID ids.NodeID) *peer {
	return &peer{
		Config: &Config{Log: log.NewNoOpLogger()},
		id:     tlsNodeID,
	}
}

// TestAdoptVerifiedPQIdentity_BindsAndAdopts is the core of the strict-PQ
// block-production fix: when the NodeID a peer presents is the canonical
// derivation of the ML-DSA key it proved possession of, the peer's identity
// is switched from the transport-layer (TLS-cert) NodeID to the
// validator-set (ML-DSA) NodeID that consensus keys peers by.
func TestAdoptVerifiedPQIdentity_BindsAndAdopts(t *testing.T) {
	require := require.New(t)

	identity, err := NewLocalIdentity(ids.EmptyNodeID)
	require.NoError(err)
	mldsaNodeID := deriveStakingNodeID(t, identity)

	tlsNodeID := ids.GenerateTestNodeID()
	require.NotEqual(tlsNodeID, mldsaNodeID)

	p := newAdoptTestPeer(tlsNodeID)
	require.Equal(tlsNodeID, p.ID(), "precondition: peer starts on its TLS-cert NodeID")

	result := &HandshakeResult{
		PeerNodeID: mldsaNodeID,
		PeerMLDSA:  identity.Public,
	}

	require.NoError(p.adoptVerifiedPQIdentity(result))
	require.Equal(mldsaNodeID, p.ID(),
		"peer must adopt the ML-DSA validator NodeID after a bound handshake")
}

// TestAdoptVerifiedPQIdentity_RejectsUnboundNodeID covers the impersonation
// case the old ephemeral-key handshake silently allowed: a peer signs a
// SELF-ASSERTED NodeID (e.g. a real validator's) with a key that does NOT
// derive that NodeID. The binding check must reject it and leave the peer's
// identity untouched.
func TestAdoptVerifiedPQIdentity_RejectsUnboundNodeID(t *testing.T) {
	require := require.New(t)

	identity, err := NewLocalIdentity(ids.EmptyNodeID)
	require.NoError(err)

	tlsNodeID := ids.GenerateTestNodeID()
	forgedNodeID := ids.GenerateTestNodeID() // NOT derived from identity.Public
	require.NotEqual(forgedNodeID, deriveStakingNodeID(t, identity))

	p := newAdoptTestPeer(tlsNodeID)
	result := &HandshakeResult{
		PeerNodeID: forgedNodeID,
		PeerMLDSA:  identity.Public,
	}

	err = p.adoptVerifiedPQIdentity(result)
	require.Error(err)
	require.Contains(err.Error(), "identity binding failed")
	require.Equal(tlsNodeID, p.ID(),
		"a failed binding must NOT mutate the peer identity")
}

// TestAdoptVerifiedPQIdentity_NilResult guards the defensive checks so a
// malformed/absent handshake result can never silently leave the peer on a
// stale identity.
func TestAdoptVerifiedPQIdentity_NilResult(t *testing.T) {
	require := require.New(t)
	p := newAdoptTestPeer(ids.GenerateTestNodeID())
	require.Error(p.adoptVerifiedPQIdentity(nil))
	require.Error(p.adoptVerifiedPQIdentity(&HandshakeResult{PeerNodeID: ids.GenerateTestNodeID()}))
}
