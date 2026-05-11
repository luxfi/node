// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kem

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDKGChannel_ForcesMLKEM1024 asserts the DKG channel adapter refuses
// any KEM other than ML-KEM-1024. This closes the cross-axis drift where
// a profile pins HighValueKEM=ML-KEM-1024 but a DKG attempt under
// ML-KEM-768 would otherwise succeed silently.
func TestDKGChannel_ForcesMLKEM1024(t *testing.T) {
	require := require.New(t)

	// DKGChannelScheme is the canonical constant; assert its byte value
	// is ML-KEM-1024 so a future refactor can't silently flip it.
	require.Equal(KeyExchangeMLKEM1024, DKGChannelScheme,
		"DKGChannelScheme MUST be ML-KEM-1024")

	pub1024, priv1024, err := GenerateKEMKeypair(KeyExchangeMLKEM1024, rand.Reader)
	require.NoError(err)

	transcript := []byte("dkg-channel-transcript")

	// Happy path: ML-KEM-1024 succeeds end-to-end.
	initSess, ct, err := InitiateDKGKEMSession(KeyExchangeMLKEM1024, pub1024, transcript)
	require.NoError(err)
	require.Equal(KeyExchangeMLKEM1024, initSess.SchemeID)

	respSess, err := RespondDKGKEMSession(KeyExchangeMLKEM1024, priv1024, ct, transcript)
	require.NoError(err)
	require.Equal(initSess.SharedSecret, respSess.SharedSecret)

	// DKG-flavour AEAD key MUST derive cleanly.
	initKey, err := initSess.DeriveDKGAEADKey()
	require.NoError(err)
	respKey, err := respSess.DeriveDKGAEADKey()
	require.NoError(err)
	require.Equal(initKey, respKey)

	// Bad path 1: caller passes ML-KEM-768 to the DKG adapter.
	pub768, priv768, err := GenerateKEMKeypair(KeyExchangeMLKEM768, rand.Reader)
	require.NoError(err)

	_, _, err = InitiateDKGKEMSession(KeyExchangeMLKEM768, pub768, transcript)
	require.ErrorIs(err, ErrDKGSchemeMismatch,
		"DKG adapter MUST refuse ML-KEM-768")

	_, err = RespondDKGKEMSession(KeyExchangeMLKEM768, priv768, make([]byte, 1088), transcript)
	require.ErrorIs(err, ErrDKGSchemeMismatch)

	// Bad path 2: caller passes a classical marker.
	_, _, err = InitiateDKGKEMSession(KeyExchangeX25519Unsafe, pub1024, transcript)
	require.ErrorIs(err, ErrDKGSchemeMismatch)

	// Bad path 3: caller passes None.
	_, _, err = InitiateDKGKEMSession(KeyExchangeNone, pub1024, transcript)
	require.ErrorIs(err, ErrDKGSchemeMismatch)
}

// TestAssertDKGCompliance exercises the boot-time cross-axis check that
// a pulsar / pulsar-m DKG session's scheme byte agrees with the strict-PQ
// profile's HighValueKEM byte. Both must be ML-KEM-1024; anything else is
// a configuration drift.
func TestAssertDKGCompliance(t *testing.T) {
	require := require.New(t)

	// Happy path: both ML-KEM-1024.
	require.NoError(AssertDKGCompliance(KeyExchangeMLKEM1024, KeyExchangeMLKEM1024))

	// Profile high-value byte wrong.
	require.ErrorIs(
		AssertDKGCompliance(KeyExchangeMLKEM1024, KeyExchangeMLKEM768),
		ErrDKGSchemeMismatch,
	)

	// Session byte wrong (profile correct).
	require.ErrorIs(
		AssertDKGCompliance(KeyExchangeMLKEM768, KeyExchangeMLKEM1024),
		ErrDKGSchemeMismatch,
	)

	// Both wrong.
	require.ErrorIs(
		AssertDKGCompliance(KeyExchangeX25519Unsafe, KeyExchangeMLKEM768),
		ErrDKGSchemeMismatch,
	)
}
