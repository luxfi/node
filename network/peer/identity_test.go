// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/kem"
)

// TestStakingIdentity_RoundTripsTheKeyOnDisk is the property that makes a node
// recognisable at all. The identity a node loads from its persistent staking
// key must be the same keypair it wrote — the public half is what its NodeID
// derives from, so any drift here and the validator set stops matching the
// peer that is actually talking.
func TestStakingIdentity_RoundTripsTheKeyOnDisk(t *testing.T) {
	require := require.New(t)

	pub, priv, err := mldsa65.GenerateKey(rand.Reader)
	require.NoError(err)

	pubBytes := packMLDSAPub(pub)
	privBytes, err := priv.MarshalBinary()
	require.NoError(err)

	nodeID, _, err := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, pubBytes)
	require.NoError(err)

	identity, err := NewLocalIdentityFromStakingKey(nodeID, pubBytes, privBytes)
	require.NoError(err)
	require.Equal(nodeID, identity.NodeID)
	require.Equal(pubBytes, packMLDSAPub(identity.Public),
		"the loaded public key must be the key on disk")

	// The loaded secret must actually sign for the loaded public key: a
	// mismatched pair would load without complaint and fail every handshake.
	sig := make([]byte, IdentitySignatureSize)
	require.NoError(mldsa65.SignTo(identity.Secret, []byte("message"), []byte("ctx"), false, sig))
	require.True(mldsa65.Verify(identity.Public, []byte("message"), []byte("ctx"), sig),
		"the loaded keypair must be a pair")

	// And it is usable as a handshake identity end to end.
	var chainID [chainIDSize]byte
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)
	init, _, err := InitiateHandshake(cfg, identity)
	require.NoError(err)
	require.NoError(validateRemoteInit(cfg, init))
	_, _, err = RespondHandshake(cfg, identity, init)
	require.NoError(err)
}

// TestStakingIdentity_RefusesKeyMaterialOfTheWrongWidth. Key bytes come off
// disk, and a truncated or padded file is the shape a half-written key has.
// Loading one and carrying on would give the node an identity nothing can
// verify — a validator that is silently invisible to its own network.
func TestStakingIdentity_RefusesKeyMaterialOfTheWrongWidth(t *testing.T) {
	require := require.New(t)

	pub, priv, err := mldsa65.GenerateKey(rand.Reader)
	require.NoError(err)
	pubBytes := packMLDSAPub(pub)
	privBytes, err := priv.MarshalBinary()
	require.NoError(err)
	nodeID := ids.GenerateTestNodeID()

	badPubs := map[string][]byte{
		"empty":     nil,
		"one short": pubBytes[:len(pubBytes)-1],
		"one long":  append(bytes.Clone(pubBytes), 0),
		"truncated": pubBytes[:32],
		"private key in the public slot": privBytes,
	}
	for name, b := range badPubs {
		_, err := NewLocalIdentityFromStakingKey(nodeID, b, privBytes)
		require.Error(err, "public key %q must be refused", name)
	}

	badPrivs := map[string][]byte{
		"empty":     nil,
		"one short": privBytes[:len(privBytes)-1],
		"one long":  append(bytes.Clone(privBytes), 0),
		"truncated": privBytes[:32],
		"public key in the private slot": pubBytes,
	}
	for name, b := range badPrivs {
		_, err := NewLocalIdentityFromStakingKey(nodeID, pubBytes, b)
		require.Error(err, "private key %q must be refused", name)
	}

	// The well-formed pair still loads, so the refusals above are about the
	// widths and not about the constructor refusing everything.
	_, err = NewLocalIdentityFromStakingKey(nodeID, pubBytes, privBytes)
	require.NoError(err)
}

// TestHandshakeConfig_RefusesAnInadmissibleLocalConfig pins the rule that a
// node cannot talk itself into a weak session. The KEM refusal is not only
// about what a peer offers — a node configured with a classical or unknown
// scheme must fail to start a handshake rather than offer one.
func TestHandshakeConfig_RefusesAnInadmissibleLocalConfig(t *testing.T) {
	require := require.New(t)
	identity, _, chainID := newTestIdentities(t)

	var noChain [chainIDSize]byte
	_ = noChain

	cases := map[string]*HandshakeConfig{
		"nil": nil,
		"classical KEM": {
			Profile: ProfileStrictPQ, ChainID: chainID,
			KEMScheme: kem.KeyExchangeX25519Unsafe, ForbidClassicalKEM: true,
		},
		"unspecified KEM": {
			Profile: ProfileStrictPQ, ChainID: chainID,
			KEMScheme: kem.KeyExchangeNone, ForbidClassicalKEM: true,
		},
		"no profile": {
			Profile: ProfileNone, ChainID: chainID,
			KEMScheme: kem.KeyExchangeMLKEM768, ForbidClassicalKEM: true,
		},
	}

	for name, cfg := range cases {
		_, _, err := InitiateHandshake(cfg, identity)
		require.Error(err, "config %q must not produce an INIT", name)

		if cfg != nil {
			_, _, err = RespondHandshake(cfg, identity, &HandshakeInit{})
			require.Error(err, "config %q must not produce a RESP", name)
		}
	}

	// A well-formed config still works, so the refusals are about the values.
	good := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)
	_, _, err := InitiateHandshake(good, identity)
	require.NoError(err)
}

// TestHandshakeResp_EveryCrossAxisCheckHolds walks the responder message field
// by field. The initiator has already committed to a profile, a chain and a
// KEM; the responder gets to echo those and nothing else. Each edit below is a
// responder trying to change one of them after the fact.
func TestHandshakeResp_EveryCrossAxisCheckHolds(t *testing.T) {
	initiator, responder, chainID := newTestIdentities(t)
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)

	init, _, err := InitiateHandshake(cfg, initiator)
	require.NoError(t, err)
	good, _, err := RespondHandshake(cfg, responder, init)
	require.NoError(t, err)
	require.NoError(t, validateRemoteResp(cfg, init, good), "precondition: the honest RESP passes")

	cases := map[string]struct {
		edit func(*HandshakeResp)
		want error
	}{
		"a protocol version we do not speak": {
			func(r *HandshakeResp) { r.ProtocolVersion = 0x02 }, ErrHandshakeBadVersion,
		},
		"version zero": {
			func(r *HandshakeResp) { r.ProtocolVersion = 0x00 }, ErrHandshakeBadVersion,
		},
		"a different security profile": {
			func(r *HandshakeResp) { r.Profile = ProfilePermissive }, ErrHandshakeProfileMismatch,
		},
		"no security profile": {
			func(r *HandshakeResp) { r.Profile = ProfileNone }, ErrHandshakeProfileMismatch,
		},
		"a different chain": {
			func(r *HandshakeResp) { r.ChainID[0] ^= 0xff }, ErrHandshakeChainMismatch,
		},
		"a classical KEM": {
			func(r *HandshakeResp) { r.KEMScheme = kem.KeyExchangeX25519Unsafe }, ErrHandshakeClassicalKEM,
		},
		"a KEM the initiator did not offer": {
			func(r *HandshakeResp) { r.KEMScheme = kem.KeyExchangeMLKEM1024 }, ErrHandshakeKEMScheme,
		},
		"the zero NodeID": {
			func(r *HandshakeResp) { r.NodeID = ids.EmptyNodeID }, ErrHandshakeNodeIDZero,
		},
		"an identity key one byte short": {
			func(r *HandshakeResp) { r.MLDSAPub = r.MLDSAPub[:len(r.MLDSAPub)-1] }, ErrHandshakeBadIdentity,
		},
		"no identity key": {
			func(r *HandshakeResp) { r.MLDSAPub = nil }, ErrHandshakeBadIdentity,
		},
		"a ciphertext one byte short": {
			func(r *HandshakeResp) { r.KEMCiphertext = r.KEMCiphertext[:len(r.KEMCiphertext)-1] }, ErrHandshakeKEMScheme,
		},
		"a ciphertext one byte long": {
			func(r *HandshakeResp) { r.KEMCiphertext = append(bytes.Clone(r.KEMCiphertext), 0) }, ErrHandshakeKEMScheme,
		},
		"no ciphertext": {
			func(r *HandshakeResp) { r.KEMCiphertext = nil }, ErrHandshakeKEMScheme,
		},
		"a signature one byte short": {
			func(r *HandshakeResp) { r.Sig = r.Sig[:len(r.Sig)-1] }, ErrHandshakeBadIdentity,
		},
		"no signature": {
			func(r *HandshakeResp) { r.Sig = nil }, ErrHandshakeBadIdentity,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			edited := *good
			edited.MLDSAPub = bytes.Clone(good.MLDSAPub)
			edited.KEMCiphertext = bytes.Clone(good.KEMCiphertext)
			edited.Sig = bytes.Clone(good.Sig)
			tc.edit(&edited)

			require.ErrorIs(t, validateRemoteResp(cfg, init, &edited), tc.want)
		})
	}

	require.Error(t, validateRemoteResp(cfg, init, nil), "an absent RESP is not a RESP")
}

// TestHandshake_SignatureCoversEveryFieldItClaimsTo. The size checks above run
// before the signature; this is the check that the signature itself is not
// merely present. Flip any signed field of an otherwise valid RESP and the
// initiator must refuse, or a relay could rewrite a handshake it is carrying.
func TestHandshake_SignatureCoversEveryFieldItClaimsTo(t *testing.T) {
	initiator, responder, chainID := newTestIdentities(t)
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)

	init, kemSec, err := InitiateHandshake(cfg, initiator)
	require.NoError(t, err)
	good, _, err := RespondHandshake(cfg, responder, init)
	require.NoError(t, err)
	_, err = FinishInitiatorHandshake(cfg, initiator, init, good, kemSec)
	require.NoError(t, err, "precondition: the honest handshake completes")

	// Each edit keeps every width legal, so the size checks pass and only the
	// signature can refuse.
	edits := map[string]func(*HandshakeResp){
		"the responder's NodeID": func(r *HandshakeResp) { r.NodeID = ids.GenerateTestNodeID() },
		"the responder's identity key": func(r *HandshakeResp) {
			other, err := NewLocalIdentity(ids.EmptyNodeID)
			require.NoError(t, err)
			r.MLDSAPub = packMLDSAPub(other.Public)
		},
		"a byte of the ciphertext": func(r *HandshakeResp) { r.KEMCiphertext[0] ^= 0x01 },
		"a byte of the signature":  func(r *HandshakeResp) { r.Sig[0] ^= 0x01 },
	}

	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			edited := *good
			edited.MLDSAPub = bytes.Clone(good.MLDSAPub)
			edited.KEMCiphertext = bytes.Clone(good.KEMCiphertext)
			edited.Sig = bytes.Clone(good.Sig)
			edit(&edited)

			_, err := FinishInitiatorHandshake(cfg, initiator, init, &edited, kemSec)
			require.Error(t, err, "editing %s must be refused", name)
		})
	}
}

// TestHandshake_InitiatorSignatureIsNotAResponderSignature is the role
// separation stated directly. Both roles sign with the same long-term key over
// overlapping bytes; only the FIPS 204 context string tells them apart, and
// without it a captured INIT signature would authorise a RESP.
func TestHandshake_InitiatorSignatureIsNotAResponderSignature(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)

	init, _, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)
	resp, _, err := RespondHandshake(cfg, responder, init)
	require.NoError(err)

	// The initiator's signature verifies under the initiator context and
	// nowhere else.
	prefix := init.transcriptPrefix(roleInitiator)
	require.True(mldsa65.Verify(initiator.Public, prefix,
		[]byte("NODE_PQ_HANDSHAKE_V1/initiator"), init.Sig))
	require.False(mldsa65.Verify(initiator.Public, prefix,
		[]byte("NODE_PQ_HANDSHAKE_V1/responder"), init.Sig),
		"an initiator signature must not verify as a responder's")

	respPrefix := resp.transcriptPrefix(init, roleResponder)
	require.True(mldsa65.Verify(responder.Public, respPrefix,
		[]byte("NODE_PQ_HANDSHAKE_V1/responder"), resp.Sig))
	require.False(mldsa65.Verify(responder.Public, respPrefix,
		[]byte("NODE_PQ_HANDSHAKE_V1/initiator"), resp.Sig),
		"a responder signature must not verify as an initiator's")
}

// TestHandshake_TranscriptSeparatesEveryField pins the encoding property that
// makes the transcript meaningful: two handshakes that differ anywhere must
// produce different transcript bytes. A concatenation without length prefixes
// would let a long NodeID and a short key spell the same string as a short
// NodeID and a long key, and one signature would then cover both.
func TestHandshake_TranscriptSeparatesEveryField(t *testing.T) {
	require := require.New(t)
	base, _, chainID := newTestIdentities(t)
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)

	init, _, err := InitiateHandshake(cfg, base)
	require.NoError(err)

	seen := map[string]string{"original": string(init.transcriptPrefix(roleInitiator))}
	edits := map[string]func(*HandshakeInit){
		"version":  func(h *HandshakeInit) { h.ProtocolVersion ^= 0xff },
		"profile":  func(h *HandshakeInit) { h.Profile = ProfilePermissive },
		"chain":    func(h *HandshakeInit) { h.ChainID[31] ^= 0x01 },
		"kem":      func(h *HandshakeInit) { h.KEMScheme = kem.KeyExchangeMLKEM1024 },
		"nodeID":   func(h *HandshakeInit) { h.NodeID = ids.GenerateTestNodeID() },
		"identity": func(h *HandshakeInit) { h.MLDSAPub[0] ^= 0x01 },
		"kem key":  func(h *HandshakeInit) { h.KEMPub[0] ^= 0x01 },
	}

	for name, edit := range edits {
		edited := *init
		edited.MLDSAPub = bytes.Clone(init.MLDSAPub)
		edited.KEMPub = bytes.Clone(init.KEMPub)
		edit(&edited)

		encoded := string(edited.transcriptPrefix(roleInitiator))
		for otherName, other := range seen {
			require.NotEqual(other, encoded,
				"changing the %s must change the transcript (collides with %q)", name, otherName)
		}
		seen[name] = encoded
	}

	// The AEAD binding must be just as separating: two handshakes that agree
	// on everything but one field must not derive the same session key.
	require.Len(seen, len(edits)+1)
}

// TestHandshake_AEADBindingCommitsToBothIdentities. The session key must
// depend on WHO the two peers are, not only on the KEM secret. Otherwise a
// relay that forwards a KEM exchange between two honest peers ends up holding
// the same key they do.
func TestHandshake_AEADBindingCommitsToBothIdentities(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)

	init, _, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)
	resp, _, err := RespondHandshake(cfg, responder, init)
	require.NoError(err)

	base := bindAEADTranscript(init, resp)

	imposter, err := NewLocalIdentity(ids.EmptyNodeID)
	require.NoError(err)

	swapped := *resp
	swapped.MLDSAPub = packMLDSAPub(imposter.Public)
	require.NotEqual(base, bindAEADTranscript(init, &swapped),
		"the binding must name the responder's identity key")

	swappedInit := *init
	swappedInit.MLDSAPub = packMLDSAPub(imposter.Public)
	require.NotEqual(base, bindAEADTranscript(&swappedInit, resp),
		"the binding must name the initiator's identity key")

	otherChain := *init
	otherChain.ChainID[0] ^= 0xff
	require.NotEqual(base, bindAEADTranscript(&otherChain, resp),
		"the binding must name the chain")

	otherProfile := *init
	otherProfile.Profile = ProfilePermissive
	require.NotEqual(base, bindAEADTranscript(&otherProfile, resp),
		"the binding must name the security profile")
}

// TestProfileID_NamesEveryValue keeps the diagnostic honest. These strings are
// what an operator reads when two nodes refuse each other, and an unknown byte
// has to print as itself rather than collapse into a known name — otherwise a
// peer offering 0x7f reads in the log as one offering none.
func TestProfileID_NamesEveryValue(t *testing.T) {
	require := require.New(t)

	require.Equal("none", ProfileNone.String())
	require.Equal("strict-pq", ProfileStrictPQ.String())
	require.Equal("permissive", ProfilePermissive.String())
	require.Equal("fips", ProfileFIPS.String())
	require.Equal("profile(0x7f)", ProfileID(0x7f).String())
	require.Equal("profile(0xff)", ProfileID(0xff).String())

	// Strictness is a property of the profile, not of a config flag that
	// could be set inconsistently with it.
	require.True(ProfileStrictPQ.IsStrict())
	require.True(ProfileFIPS.IsStrict())
	require.False(ProfileNone.IsStrict())
	require.False(ProfilePermissive.IsStrict())
	require.False(ProfileID(0x7f).IsStrict(), "an unrecognised profile is not strict by accident")

	require.Equal("initiator", roleInitiator.String())
	require.Equal("responder", roleResponder.String())
	require.Equal("role(0x00)", HandshakeRole(0).String())

	require.Equal("unknown", ChainUnknown.String())
	require.Equal("compatible", ChainCompatible.String())
	require.Equal("incompatible", ChainIncompatible.String())
	require.Equal("unknown", ChainState(99).String(),
		"a state this build does not know reads as unknown, which is the permissive one")
}
