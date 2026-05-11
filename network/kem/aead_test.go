// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kem

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewAEAD_RoundTrip exercises the end-to-end "derive key → encrypt →
// decrypt" pipeline using ChaCha20-Poly1305 over a KEM-derived key.
// This is the actual code path a peer takes after a successful PQ
// handshake.
func TestNewAEAD_RoundTrip(t *testing.T) {
	require := require.New(t)

	pub, priv, err := GenerateKEMKeypair(KeyExchangeMLKEM768, rand.Reader)
	require.NoError(err)

	transcript := []byte("aead-roundtrip-transcript")
	initSess, ct, err := InitiateKEMSession(KeyExchangeMLKEM768, pub, transcript)
	require.NoError(err)
	respSess, err := RespondKEMSession(KeyExchangeMLKEM768, priv, ct, transcript)
	require.NoError(err)

	initKey := initSess.DeriveAEADKey()
	respKey := respSess.DeriveAEADKey()
	require.Equal(initKey, respKey)

	initAEAD, err := NewAEAD(AEADChaCha20Poly1305, initKey[:])
	require.NoError(err)
	respAEAD, err := NewAEAD(AEADChaCha20Poly1305, respKey[:])
	require.NoError(err)

	nonce := make([]byte, initAEAD.NonceSize())
	_, err = rand.Read(nonce)
	require.NoError(err)

	plaintext := []byte("hello, lux strict-PQ peer")
	aad := []byte("lux-node/v1/peer-frame")

	ctOut := initAEAD.Seal(nil, nonce, plaintext, aad)
	require.NotEqual(plaintext, ctOut)

	got, err := respAEAD.Open(nil, nonce, ctOut, aad)
	require.NoError(err)
	require.Equal(plaintext, got)
}

// TestNewAEAD_RefusesUnknown asserts unknown AEAD bytes are refused with
// the precise ErrUnsupportedAEAD error.
func TestNewAEAD_RefusesUnknown(t *testing.T) {
	require := require.New(t)

	_, err := NewAEAD(AEADNone, make([]byte, 32))
	require.ErrorIs(err, ErrUnsupportedAEAD)

	_, err = NewAEAD(AEADID(0xAA), make([]byte, 32))
	require.ErrorIs(err, ErrUnsupportedAEAD)
}

// TestNewAEAD_RefusesBadKeySize asserts a mis-sized key is rejected
// before crypto runs.
func TestNewAEAD_RefusesBadKeySize(t *testing.T) {
	require := require.New(t)

	_, err := NewAEAD(AEADChaCha20Poly1305, make([]byte, 16))
	require.Error(err)
}
