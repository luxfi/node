// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/node/staking"
)

// TestSignedIP_RoundTripsPQ verifies that a SignedIP minted with the
// ML-DSA-65 leg round-trips correctly: TLS leg verifies, ML-DSA leg
// verifies, and the strict-PQ profile gate is satisfied.
func TestSignedIP_RoundTripsPQ(t *testing.T) {
	require := require.New(t)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	tlsKey := tlsCert.PrivateKey.(crypto.Signer)
	blsKey, err := localsigner.New()
	require.NoError(err)

	mldsaPriv, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	require.NoError(err)
	mldsaPub := mldsaPriv.PublicKey

	now := time.Now()
	unsigned := UnsignedIP{
		AddrPort:  netip.MustParseAddrPort("1.2.3.4:9650"),
		Timestamp: uint64(now.Unix()),
	}

	signed, err := unsigned.SignPQ(tlsKey, blsKey, mldsaPriv)
	require.NoError(err)
	require.NotEmpty(signed.TLSSignature)
	require.NotEmpty(signed.BLSSignatureBytes)
	require.NotEmpty(signed.MLDSASignature, "ML-DSA leg must be present after SignPQ with a real signer")

	// Classical-only verifier accepts the cert + TLS sig.
	require.NoError(signed.Verify(cert, now))

	// Strict-PQ verifier accepts when both legs are present and the
	// ML-DSA public key matches.
	require.NoError(signed.VerifyUnderProfile(cert, mldsaPub, now, true))

	// Tampering with the ML-DSA signature byte flips the gate.
	tampered := *signed
	tampered.MLDSASignature = append([]byte(nil), signed.MLDSASignature...)
	tampered.MLDSASignature[0] ^= 0xFF
	require.ErrorIs(tampered.VerifyUnderProfile(cert, mldsaPub, now, true), errInvalidMLDSASignature)

	// A different ML-DSA public key fails verification.
	otherPriv, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	require.NoError(err)
	require.ErrorIs(
		signed.VerifyUnderProfile(cert, otherPriv.PublicKey, now, true),
		errInvalidMLDSASignature,
	)
}

// TestSignedIP_RefusesMissingMLDSAUnderStrictPQ verifies that a classical-only
// SignedIP (TLS + BLS, no ML-DSA leg) is refused by the strict-PQ verifier.
// This is the property the task asks us to enforce: both classical legs
// (TLS, BLS12-381) are quantum-broken, so the strict-PQ chain refuses any
// gossip that lacks the post-quantum witness.
func TestSignedIP_RefusesMissingMLDSAUnderStrictPQ(t *testing.T) {
	require := require.New(t)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	tlsKey := tlsCert.PrivateKey.(crypto.Signer)
	blsKey, err := localsigner.New()
	require.NoError(err)

	mldsaPriv, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	require.NoError(err)
	mldsaPub := mldsaPriv.PublicKey

	now := time.Now()
	unsigned := UnsignedIP{
		AddrPort:  netip.MustParseAddrPort("1.2.3.4:9650"),
		Timestamp: uint64(now.Unix()),
	}

	// Classical-only sign — no ML-DSA leg.
	classicalOnly, err := unsigned.Sign(tlsKey, blsKey)
	require.NoError(err)
	require.Empty(classicalOnly.MLDSASignature)

	// Strict-PQ verifier refuses the classical-only sig.
	err = classicalOnly.VerifyUnderProfile(cert, mldsaPub, now, true)
	require.ErrorIs(err, errMissingMLDSASignature)

	// And refuses again when the caller forgets to supply the pubkey
	// (different failure mode, same outcome — refusal).
	signedWithLeg, err := unsigned.SignPQ(tlsKey, blsKey, mldsaPriv)
	require.NoError(err)
	require.ErrorIs(
		signedWithLeg.VerifyUnderProfile(cert, nil, now, true),
		errMissingMLDSAPublicKey,
	)
}

// TestSignedIP_AcceptsClassicalOnlyUnderPermissive verifies backwards
// compatibility: a non-strict-PQ chain (testnet / devnet / permissive
// profile) still accepts a SignedIP that carries only the classical TLS
// + BLS legs and no ML-DSA leg. This is what keeps a freshly-built strict-
// PQ binary from breaking peer-handshake with the existing testnet fleet.
func TestSignedIP_AcceptsClassicalOnlyUnderPermissive(t *testing.T) {
	require := require.New(t)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	tlsKey := tlsCert.PrivateKey.(crypto.Signer)
	blsKey, err := localsigner.New()
	require.NoError(err)

	now := time.Now()
	unsigned := UnsignedIP{
		AddrPort:  netip.MustParseAddrPort("1.2.3.4:9650"),
		Timestamp: uint64(now.Unix()),
	}

	// Classical-only signer (no ML-DSA key wired) — mirrors the
	// permissive testnet binary that has not yet rolled the PQ identity
	// key onto its validator nodes.
	classicalOnly, err := unsigned.Sign(tlsKey, blsKey)
	require.NoError(err)
	require.Empty(classicalOnly.MLDSASignature)

	// Permissive verifier (strictPQ == false, nil pubkey) accepts.
	require.NoError(classicalOnly.VerifyUnderProfile(cert, nil, now, false))

	// And the legacy Verify path keeps working (peers that bypass the
	// profile-aware verifier — e.g. test rigs).
	require.NoError(classicalOnly.Verify(cert, now))

	// Defence in depth: when a permissive verifier happens to have a
	// pubkey on hand AND the peer supplied a sig, the leg is still
	// validated. We synthesise a valid PQ sig to confirm.
	mldsaPriv, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	require.NoError(err)
	withPQ, err := unsigned.SignPQ(tlsKey, blsKey, mldsaPriv)
	require.NoError(err)
	require.NoError(withPQ.VerifyUnderProfile(cert, mldsaPriv.PublicKey, now, false))
}
