// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/kem"
)

// newTestIdentities returns two fresh ML-DSA-65 identities and a shared
// chain ID. Used by every handshake test that needs a happy-path setup.
//
// Each identity's NodeID is derived FROM its own ML-DSA-65 public key via
// deriveMLDSANodeID — the same binding production enforces (the handshake
// now rejects a claimed NodeID that doesn't match the presented key, and
// adopts the key-derived NodeID as the authoritative peer identity). A
// random ids.GenerateTestNodeID() would no longer round-trip.
func newTestIdentities(t *testing.T) (initiator, responder *LocalIdentity, chainID [32]byte) {
	t.Helper()
	require := require.New(t)

	init, err := NewLocalIdentity(ids.EmptyNodeID)
	require.NoError(err)
	init.NodeID, err = deriveMLDSANodeID(packMLDSAPub(init.Public))
	require.NoError(err)

	resp, err := NewLocalIdentity(ids.EmptyNodeID)
	require.NoError(err)
	resp.NodeID, err = deriveMLDSANodeID(packMLDSAPub(resp.Public))
	require.NoError(err)

	_, err = rand.Read(chainID[:])
	require.NoError(err)
	return init, resp, chainID
}

// runHandshake drives a full INIT/RESP/FINISH on a strict-PQ config under
// the supplied KEM scheme. Returns both result objects.
func runHandshake(
	t *testing.T,
	initiator, responder *LocalIdentity,
	chainID [32]byte,
	scheme kem.KeyExchangeID,
) (*HandshakeResult, *HandshakeResult) {
	t.Helper()
	require := require.New(t)

	cfg := &HandshakeConfig{
		Profile:            ProfileStrictPQ,
		ChainID:            chainID,
		KEMScheme:          scheme,
		ForbidClassicalKEM: true,
	}

	init, kemSec, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)
	require.NotNil(init)
	require.NotEmpty(kemSec)

	resp, respResult, err := RespondHandshake(cfg, responder, init)
	require.NoError(err)
	require.NotNil(resp)
	require.NotNil(respResult)

	initResult, err := FinishInitiatorHandshake(cfg, initiator, init, resp, kemSec)
	require.NoError(err)
	require.NotNil(initResult)

	require.Equal(initResult.AEADKey, respResult.AEADKey,
		"both sides MUST derive the same AEAD key")

	return initResult, respResult
}

// TestHandshake_StrictPQ_MLKEM768_RoundTrip exercises the default
// production handshake: strict-PQ profile, ML-KEM-768.
func TestHandshake_StrictPQ_MLKEM768_RoundTrip(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	initResult, respResult := runHandshake(t, initiator, responder, chainID, kem.KeyExchangeMLKEM768)

	require.Equal(initiator.NodeID, respResult.PeerNodeID)
	require.Equal(responder.NodeID, initResult.PeerNodeID)
	require.Equal(kem.KeyExchangeMLKEM768, initResult.KEMSession.SchemeID)
}

// TestHandshake_StrictPQ_MLKEM1024_RoundTrip exercises the high-value
// DKG-grade handshake under ML-KEM-1024.
func TestHandshake_StrictPQ_MLKEM1024_RoundTrip(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	initResult, respResult := runHandshake(t, initiator, responder, chainID, kem.KeyExchangeMLKEM1024)

	require.Equal(kem.KeyExchangeMLKEM1024, initResult.KEMSession.SchemeID)
	require.Equal(kem.KeyExchangeMLKEM1024, respResult.KEMSession.SchemeID)

	// AEADKey under DKG customisation MUST differ from peer-AEAD customisation.
	dkgKey, err := initResult.KEMSession.DeriveDKGAEADKey()
	require.NoError(err)
	require.NotEqual(initResult.AEADKey, dkgKey)
}

// TestHandshake_StrictPQ_RefusesClassicalKEM asserts a strict-PQ peer
// rejects any peer offering an explicit classical KEM marker.
func TestHandshake_StrictPQ_RefusesClassicalKEM(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	cfg := &HandshakeConfig{
		Profile:            ProfileStrictPQ,
		ChainID:            chainID,
		KEMScheme:          kem.KeyExchangeMLKEM768,
		ForbidClassicalKEM: true,
	}

	// Local config trying to offer a classical KEM is refused at config
	// validation time, before any wire bytes leave the box.
	badCfg := *cfg
	badCfg.KEMScheme = kem.KeyExchangeX25519Unsafe
	_, _, err := InitiateHandshake(&badCfg, initiator)
	require.ErrorIs(err, ErrHandshakeClassicalKEM)

	// A peer that hand-crafts a classical KEM byte into an INIT is
	// refused by the responder.
	init, _, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)
	init.KEMScheme = kem.KeyExchangeX25519Unsafe

	_, _, err = RespondHandshake(cfg, responder, init)
	require.ErrorIs(err, ErrHandshakeClassicalKEM)
}

// TestHandshake_StrictPQ_RefusesECDSAIdentity asserts the handshake
// rejects an identity public key that isn't a valid ML-DSA-65 key. We
// simulate this by replacing MLDSAPub with arbitrary bytes of the wrong
// length (ECDSA-P256 sec1 width = 65 bytes; ML-DSA-65 pub = 1952 bytes).
func TestHandshake_StrictPQ_RefusesECDSAIdentity(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	cfg := &HandshakeConfig{
		Profile:            ProfileStrictPQ,
		ChainID:            chainID,
		KEMScheme:          kem.KeyExchangeMLKEM768,
		ForbidClassicalKEM: true,
	}

	init, _, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)

	// Replace ML-DSA-65 public key with a 65-byte ECDSA-P256 sec1-uncompressed
	// representation. The handshake MUST refuse before any signature work.
	init.MLDSAPub = make([]byte, 65)
	init.MLDSAPub[0] = 0x04 // sec1 uncompressed prefix

	_, _, err = RespondHandshake(cfg, responder, init)
	require.ErrorIs(err, ErrHandshakeBadIdentity)
}

// TestHandshake_PreservesNodeIdentitySignature asserts that the identity
// signature actually covers the transcript: flipping any byte in the
// INIT or RESP after signing causes the verifier to reject.
func TestHandshake_PreservesNodeIdentitySignature(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	cfg := &HandshakeConfig{
		Profile:            ProfileStrictPQ,
		ChainID:            chainID,
		KEMScheme:          kem.KeyExchangeMLKEM768,
		ForbidClassicalKEM: true,
	}

	// Tamper INIT after signing: flip one byte of the KEMPub.
	init, _, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)
	require.NotEmpty(init.Sig)
	init.KEMPub[0] ^= 0x01

	_, _, err = RespondHandshake(cfg, responder, init)
	require.ErrorIs(err, ErrHandshakeBadIdentity,
		"flipping a signed-over byte MUST cause initiator signature to fail")

	// Tamper RESP after signing: re-run a clean handshake, mutate resp.
	init2, kemSec2, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)
	resp, _, err := RespondHandshake(cfg, responder, init2)
	require.NoError(err)
	require.NotEmpty(resp.Sig)
	resp.KEMCiphertext[0] ^= 0x01

	_, err = FinishInitiatorHandshake(cfg, initiator, init2, resp, kemSec2)
	require.ErrorIs(err, ErrHandshakeBadIdentity,
		"flipping a signed-over byte MUST cause responder signature to fail")
}

// TestHandshake_RefusesProfileMismatch asserts two peers running
// different security profiles cannot complete the handshake.
func TestHandshake_RefusesProfileMismatch(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	initCfg := &HandshakeConfig{
		Profile:            ProfileStrictPQ,
		ChainID:            chainID,
		KEMScheme:          kem.KeyExchangeMLKEM768,
		ForbidClassicalKEM: true,
	}
	respCfg := *initCfg
	respCfg.Profile = ProfileFIPS

	init, _, err := InitiateHandshake(initCfg, initiator)
	require.NoError(err)

	_, _, err = RespondHandshake(&respCfg, responder, init)
	require.ErrorIs(err, ErrHandshakeProfileMismatch)
}

// TestHandshake_RefusesChainMismatch asserts two peers on different
// chains cannot complete the handshake.
func TestHandshake_RefusesChainMismatch(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	var otherChain [32]byte
	_, err := rand.Read(otherChain[:])
	require.NoError(err)

	initCfg := &HandshakeConfig{
		Profile:            ProfileStrictPQ,
		ChainID:            chainID,
		KEMScheme:          kem.KeyExchangeMLKEM768,
		ForbidClassicalKEM: true,
	}
	respCfg := *initCfg
	respCfg.ChainID = otherChain

	init, _, err := InitiateHandshake(initCfg, initiator)
	require.NoError(err)

	_, _, err = RespondHandshake(&respCfg, responder, init)
	require.ErrorIs(err, ErrHandshakeChainMismatch)
}

// TestHandshake_RefusesResponderKEMDowngrade asserts the initiator
// rejects a responder that echoes back a different KEM scheme (e.g.
// downgrades ML-KEM-1024 to ML-KEM-768).
func TestHandshake_RefusesResponderKEMDowngrade(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	cfg := &HandshakeConfig{
		Profile:            ProfileStrictPQ,
		ChainID:            chainID,
		KEMScheme:          kem.KeyExchangeMLKEM1024,
		ForbidClassicalKEM: true,
	}

	init, kemSec, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)
	resp, _, err := RespondHandshake(cfg, responder, init)
	require.NoError(err)

	// Tamper the scheme byte in the responder message; the initiator MUST
	// catch the downgrade before deriving any key material.
	resp.KEMScheme = kem.KeyExchangeMLKEM768

	_, err = FinishInitiatorHandshake(cfg, initiator, init, resp, kemSec)
	require.ErrorIs(err, ErrHandshakeKEMScheme)
}

// TestHandshake_DistinctSessionsDistinctKeys asserts two independent
// handshakes between the same pair derive distinct AEAD keys (the KEM
// randomness alone is enough to differentiate; the transcript binding
// reinforces it).
func TestHandshake_DistinctSessionsDistinctKeys(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	a1, _ := runHandshake(t, initiator, responder, chainID, kem.KeyExchangeMLKEM768)
	a2, _ := runHandshake(t, initiator, responder, chainID, kem.KeyExchangeMLKEM768)
	require.NotEqual(a1.AEADKey, a2.AEADKey,
		"independent sessions MUST yield distinct AEAD keys")
}

// TestHandshake_NodeIDZeroRefused asserts the handshake rejects a peer
// presenting the zero NodeID.
func TestHandshake_NodeIDZeroRefused(t *testing.T) {
	require := require.New(t)
	initiator, responder, chainID := newTestIdentities(t)

	cfg := &HandshakeConfig{
		Profile:            ProfileStrictPQ,
		ChainID:            chainID,
		KEMScheme:          kem.KeyExchangeMLKEM768,
		ForbidClassicalKEM: true,
	}

	init, _, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)
	init.NodeID = ids.EmptyNodeID

	_, _, err = RespondHandshake(cfg, responder, init)
	require.ErrorIs(err, ErrHandshakeNodeIDZero)
}
