// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kem

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKEMSession_MLKEM768_RoundTrip exercises one full Initiator/Responder
// dance under ML-KEM-768 and asserts both sides converge on the same
// shared secret and AEAD key. This is the production-default round-trip.
func TestKEMSession_MLKEM768_RoundTrip(t *testing.T) {
	require := require.New(t)

	pub, priv, err := GenerateKEMKeypair(KeyExchangeMLKEM768, rand.Reader)
	require.NoError(err)
	require.Len(pub, 1184, "ML-KEM-768 public key must be 1184 bytes (FIPS 203)")

	transcript := []byte("test-transcript-mlkem768-roundtrip")

	initSess, ct, err := InitiateKEMSession(KeyExchangeMLKEM768, pub, transcript)
	require.NoError(err)
	require.NotNil(initSess)
	require.Len(ct, 1088, "ML-KEM-768 ciphertext must be 1088 bytes (FIPS 203)")
	require.Equal(KeyExchangeMLKEM768, initSess.SchemeID)

	respSess, err := RespondKEMSession(KeyExchangeMLKEM768, priv, ct, transcript)
	require.NoError(err)
	require.NotNil(respSess)

	require.Equal(initSess.SharedSecret, respSess.SharedSecret,
		"initiator and responder MUST converge on the same shared secret")
	require.Equal(initSess.TranscriptHash, respSess.TranscriptHash,
		"identical transcripts MUST produce identical TranscriptHash")

	initKey := initSess.DeriveAEADKey()
	respKey := respSess.DeriveAEADKey()
	require.Equal(initKey, respKey, "derived AEAD keys MUST match")
	require.Len(initKey[:], AEADKeySize)
}

// TestKEMSession_MLKEM1024_RoundTrip is the equivalent round-trip under
// the high-value parameter set. Same convergence invariants, larger keys
// and ciphertexts.
func TestKEMSession_MLKEM1024_RoundTrip(t *testing.T) {
	require := require.New(t)

	pub, priv, err := GenerateKEMKeypair(KeyExchangeMLKEM1024, rand.Reader)
	require.NoError(err)
	require.Len(pub, 1568, "ML-KEM-1024 public key must be 1568 bytes (FIPS 203)")

	transcript := []byte("test-transcript-mlkem1024-dkg-channel")

	initSess, ct, err := InitiateKEMSession(KeyExchangeMLKEM1024, pub, transcript)
	require.NoError(err)
	require.Len(ct, 1568, "ML-KEM-1024 ciphertext must be 1568 bytes (FIPS 203)")

	respSess, err := RespondKEMSession(KeyExchangeMLKEM1024, priv, ct, transcript)
	require.NoError(err)

	require.Equal(initSess.SharedSecret, respSess.SharedSecret)
	require.Equal(initSess.TranscriptHash, respSess.TranscriptHash)
	require.Equal(initSess.DeriveAEADKey(), respSess.DeriveAEADKey())
}

// TestKEMSession_DistinctTranscriptsDifferentKeys asserts the cross-axis
// security property: two KEM sessions built with the same shared-secret
// material but distinct transcripts MUST produce distinct AEAD keys. This
// is what makes the strict-PQ handshake downgrade-resistant: an attacker
// who can flip one transcript byte gets a key the honest peer never
// derived.
func TestKEMSession_DistinctTranscriptsDifferentKeys(t *testing.T) {
	require := require.New(t)

	pub, priv, err := GenerateKEMKeypair(KeyExchangeMLKEM768, rand.Reader)
	require.NoError(err)

	transcriptA := []byte("scenario-A-handshake-bytes-v1")
	transcriptB := []byte("scenario-B-handshake-bytes-v1")

	// Different transcripts, different ciphertexts (random KEM seed each call).
	sessA, ctA, err := InitiateKEMSession(KeyExchangeMLKEM768, pub, transcriptA)
	require.NoError(err)
	sessB, ctB, err := InitiateKEMSession(KeyExchangeMLKEM768, pub, transcriptB)
	require.NoError(err)

	// Sanity: distinct sessions have distinct shared secrets (the KEM is
	// randomized).
	require.NotEqual(sessA.SharedSecret, sessB.SharedSecret)

	// The distinguishing property under test: even if we had identical
	// shared secrets, distinct transcripts produce distinct AEAD keys.
	// Verify by constructing a synthetic session that holds sessA's secret
	// under transcriptB's hash.
	hybrid := &KEMSession{
		SchemeID:       sessA.SchemeID,
		SharedSecret:   sessA.SharedSecret,
		TranscriptHash: sessB.TranscriptHash,
	}
	require.NotEqual(sessA.DeriveAEADKey(), hybrid.DeriveAEADKey(),
		"same shared secret + different transcript MUST yield different AEAD keys")

	// And mirror: same transcript + different shared secret yields different keys.
	hybrid2 := &KEMSession{
		SchemeID:       sessA.SchemeID,
		SharedSecret:   sessB.SharedSecret,
		TranscriptHash: sessA.TranscriptHash,
	}
	require.NotEqual(sessA.DeriveAEADKey(), hybrid2.DeriveAEADKey(),
		"different shared secret + same transcript MUST yield different AEAD keys")

	// Responder-side decapsulation under the wrong transcript: shared
	// secret converges (KEM is transcript-free), but derived AEAD key
	// diverges. Use ctA but pass transcriptB.
	respWrong, err := RespondKEMSession(KeyExchangeMLKEM768, priv, ctA, transcriptB)
	require.NoError(err)
	require.Equal(sessA.SharedSecret, respWrong.SharedSecret,
		"KEM decapsulation is transcript-free; shared secret still converges")
	require.NotEqual(sessA.DeriveAEADKey(), respWrong.DeriveAEADKey(),
		"AEAD key MUST diverge when transcripts disagree")

	// Cross-traffic sanity: ctA != ctB (KEM is randomized).
	require.False(bytes.Equal(ctA, ctB), "two encapsulations against the same pub MUST differ")
}

// TestKEMSession_RefusesClassicalKEM asserts strict-PQ peers refuse to
// accept the classical KEM marker. This is the wire-level enforcement of
// HIP-0077 "no classical primitives in strict-PQ mode". The canonical
// config.KeyExchangeID enum exposes a single classical marker
// (X25519Unsafe at 0x90); the P-256 / P-384 markers this package once
// declared separately are collapsed onto X25519Unsafe there.
func TestKEMSession_RefusesClassicalKEM(t *testing.T) {
	require := require.New(t)

	scheme := KeyExchangeX25519Unsafe
	t.Run(scheme.String(), func(t *testing.T) {
		_, _, err := GenerateKEMKeypair(scheme, rand.Reader)
		require.ErrorIs(err, ErrClassicalKEMForbidden)

		_, _, err = InitiateKEMSession(scheme, nil, []byte("x"))
		require.ErrorIs(err, ErrClassicalKEMForbidden)

		_, err = RespondKEMSession(scheme, nil, nil, []byte("x"))
		require.ErrorIs(err, ErrClassicalKEMForbidden)
	})
}

// TestKEMSession_RefusesEmptyTranscript asserts the binding requirement:
// an empty transcript MUST fail at construction time, not silently bind
// to a zero-length tuple.
func TestKEMSession_RefusesEmptyTranscript(t *testing.T) {
	require := require.New(t)

	pub, priv, err := GenerateKEMKeypair(KeyExchangeMLKEM768, rand.Reader)
	require.NoError(err)

	_, _, err = InitiateKEMSession(KeyExchangeMLKEM768, pub, nil)
	require.ErrorIs(err, ErrEmptyTranscript)

	_, err = RespondKEMSession(KeyExchangeMLKEM768, priv, make([]byte, 1088), nil)
	require.ErrorIs(err, ErrEmptyTranscript)
}

// TestKEMSession_RefusesBadSizes asserts mis-sized public keys, private
// keys, and ciphertexts are rejected with a precise error rather than
// silently truncated.
func TestKEMSession_RefusesBadSizes(t *testing.T) {
	require := require.New(t)

	_, _, err := InitiateKEMSession(KeyExchangeMLKEM768, make([]byte, 100), []byte("x"))
	require.ErrorIs(err, ErrBadPublicKeySize)

	_, err = RespondKEMSession(KeyExchangeMLKEM768, make([]byte, 100), make([]byte, 1088), []byte("x"))
	require.ErrorIs(err, ErrBadPrivateKeySize)

	_, priv, gerr := GenerateKEMKeypair(KeyExchangeMLKEM768, rand.Reader)
	require.NoError(gerr)
	_, err = RespondKEMSession(KeyExchangeMLKEM768, priv, make([]byte, 100), []byte("x"))
	require.ErrorIs(err, ErrBadCiphertextSize)
}

// TestKEMSession_RefusesUnsupportedScheme asserts KeyExchangeNone and any
// unknown byte fail with ErrUnsupportedScheme at construction time. ML-KEM
// at NIST Cat 1 is no longer named in the canonical enum (below the
// strict-PQ floor); an arbitrary unknown byte stands in for it here.
func TestKEMSession_RefusesUnsupportedScheme(t *testing.T) {
	require := require.New(t)

	_, _, err := GenerateKEMKeypair(KeyExchangeNone, rand.Reader)
	require.ErrorIs(err, ErrUnsupportedScheme)

	// Arbitrary unallocated byte stands in for "scheme not in the canonical
	// numbering" — the runtime cannot tell the difference between a retired
	// ML-KEM-512 byte and a never-allocated value.
	unknown := KeyExchangeID(0x7F)
	_, _, err = GenerateKEMKeypair(unknown, rand.Reader)
	require.ErrorIs(err, ErrUnsupportedScheme)

	_, _, err = InitiateKEMSession(unknown, make([]byte, 800), []byte("x"))
	require.ErrorIs(err, ErrUnsupportedScheme)
}

// TestHashTranscript_Deterministic asserts the TupleHash256 binding is
// deterministic and length-prefixed: rearranging the same byte content
// across two distinct messages produces a different hash.
func TestHashTranscript_Deterministic(t *testing.T) {
	require := require.New(t)

	h1 := HashTranscript([]byte("hello"), []byte("world"))
	h2 := HashTranscript([]byte("hello"), []byte("world"))
	require.Equal(h1, h2, "same inputs MUST produce same hash")

	h3 := HashTranscript([]byte("helloworld"))
	require.NotEqual(h1, h3, "TupleHash MUST distinguish ['hello','world'] from ['helloworld']")

	h4 := HashTranscript([]byte("world"), []byte("hello"))
	require.NotEqual(h1, h4, "TupleHash MUST be order-sensitive")
}

// TestDeriveDKGAEADKey_OnlyMLKEM1024 asserts the DKG-channel customisation
// refuses to derive a key for any scheme other than ML-KEM-1024. This
// closes the "high-value channel silently used Cat 3 instead of Cat 5"
// drift path.
func TestDeriveDKGAEADKey_OnlyMLKEM1024(t *testing.T) {
	require := require.New(t)

	pub, _, err := GenerateKEMKeypair(KeyExchangeMLKEM768, rand.Reader)
	require.NoError(err)
	sess, _, err := InitiateKEMSession(KeyExchangeMLKEM768, pub, []byte("x"))
	require.NoError(err)

	_, err = sess.DeriveDKGAEADKey()
	require.ErrorIs(err, ErrUnsupportedScheme,
		"DKG AEAD derivation MUST refuse ML-KEM-768 sessions")

	pub2, _, err := GenerateKEMKeypair(KeyExchangeMLKEM1024, rand.Reader)
	require.NoError(err)
	sess2, _, err := InitiateKEMSession(KeyExchangeMLKEM1024, pub2, []byte("x"))
	require.NoError(err)
	key, err := sess2.DeriveDKGAEADKey()
	require.NoError(err)
	require.Len(key[:], AEADKeySize)

	// DKG key MUST differ from the peer-AEAD key derived from the same
	// session (different customisation strings).
	peerKey := sess2.DeriveAEADKey()
	require.NotEqual(key, peerKey,
		"DKG and peer AEAD customisations MUST produce different keys")
}

// TestKeyExchangeID_Predicates spot-checks the small predicate helpers so
// a future numbering change cannot silently flip a wire byte from PQ to
// classical without breaking a test. SharedSecretBits and NISTCategory
// are free functions in this package (KeyExchangeID is a type alias of
// config.KeyExchangeID so methods can only live in the config package).
func TestKeyExchangeID_Predicates(t *testing.T) {
	require := require.New(t)

	require.True(KeyExchangeMLKEM768.IsPostQuantum())
	require.True(KeyExchangeMLKEM1024.IsPostQuantum())
	require.False(KeyExchangeNone.IsPostQuantum())
	require.False(KeyExchangeX25519Unsafe.IsPostQuantum())

	require.True(KeyExchangeX25519Unsafe.IsForbiddenInPQMode())
	require.False(KeyExchangeMLKEM768.IsForbiddenInPQMode())

	require.EqualValues(256, SharedSecretBits(KeyExchangeMLKEM768))
	require.EqualValues(256, SharedSecretBits(KeyExchangeMLKEM1024))
	require.EqualValues(0, SharedSecretBits(KeyExchangeNone))

	require.EqualValues(3, NISTCategory(KeyExchangeMLKEM768))
	require.EqualValues(5, NISTCategory(KeyExchangeMLKEM1024))

	// Canonical wire bytes — 0x01 and 0x02, as the consensus config declares them.
	require.EqualValues(0x01, uint8(KeyExchangeMLKEM768))
	require.EqualValues(0x02, uint8(KeyExchangeMLKEM1024))
	require.EqualValues(0x90, uint8(KeyExchangeX25519Unsafe))
}
