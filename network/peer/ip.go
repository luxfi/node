// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/wrappers"
)

var (
	errTimestampTooFarInFuture = errors.New("timestamp too far in the future")
	errTimestampTooFarInPast   = errors.New("timestamp too far in the past")
	errInvalidTLSSignature     = errors.New("invalid TLS signature")
	// errMissingMLDSASignature is returned by Verify when strict-PQ profile
	// is in force but the SignedIP carries no ML-DSA leg. Strict-PQ chains
	// refuse classical-only IP gossip because both TLS and BLS12-381 are
	// quantum-broken.
	errMissingMLDSASignature = errors.New("missing ML-DSA-65 signature under strict-PQ profile")
	// errMissingMLDSAPublicKey is returned by Verify when an ML-DSA signature
	// is present on the wire but the caller did not supply the validator's
	// ML-DSA public key needed to verify it. Configuration bug — the caller
	// must pass the pubkey when verification proceeds under strict-PQ.
	errMissingMLDSAPublicKey = errors.New("missing ML-DSA-65 public key for verification")
	// errInvalidMLDSASignature is returned by Verify when the ML-DSA-65
	// signature over the canonical UnsignedIP bytes fails to verify under
	// the supplied public key.
	errInvalidMLDSASignature = errors.New("invalid ML-DSA-65 signature")
)

// signedIPMLDSAContext is the FIPS 204 context string bound into every
// ML-DSA-65 signature over an UnsignedIP. Pinning a context here makes a
// captured IP-gossip signature un-replayable in any other Lux signing
// surface (transaction auth, UTXO Fx, peer handshake) even though they all
// share the same long-term ML-DSA-65 identity key.
var signedIPMLDSAContext = []byte("lux-peer-signed-ip-v1")

// UnsignedIP is used for a validator to claim an IP. The [Timestamp] is used to
// ensure that the most updated IP claim is tracked by peers for a given
// validator.
type UnsignedIP struct {
	AddrPort  netip.AddrPort
	Timestamp uint64
}

// Sign produces a classical-only SignedIP carrying TLS + BLS legs. Retained
// for backwards compatibility with permissive (non-strict-PQ) chains and
// with the existing test surface. Strict-PQ producers MUST call SignPQ.
func (ip *UnsignedIP) Sign(tlsSigner crypto.Signer, blsSigner bls.Signer) (*SignedIP, error) {
	return ip.SignPQ(tlsSigner, blsSigner, nil)
}

// SignPQ produces a SignedIP carrying every leg the caller can mint:
//
//   - TLS leg (classical, RSA/ECDSA staker cert) — always required.
//   - BLS12-381 leg (classical, proof-of-possession) — always required.
//   - ML-DSA-65 leg (FIPS 204 post-quantum) — minted when mldsaSigner is
//     non-nil.
//
// Both classical legs are quantum-broken; the ML-DSA leg is what gives a
// strict-PQ chain a witness over the IP gossip that survives a CRQC. The
// ML-DSA leg signs the same canonical UnsignedIP bytes the classical legs
// sign, bound under the "lux-peer-signed-ip-v1" FIPS 204 context so the
// signature cannot be replayed into another Lux signing surface.
//
// Backwards compat: passing mldsaSigner == nil produces a SignedIP with
// an empty MLDSASignature. Permissive chains accept this; strict-PQ chains
// refuse it at Verify time.
func (ip *UnsignedIP) SignPQ(
	tlsSigner crypto.Signer,
	blsSigner bls.Signer,
	mldsaSigner *mldsa.PrivateKey,
) (*SignedIP, error) {
	ipBytes := ip.bytes()
	tlsSignature, err := tlsSigner.Sign(
		rand.Reader,
		hash.ComputeHash256(ipBytes),
		crypto.SHA256,
	)
	if err != nil {
		return nil, err
	}

	blsSignature, err := blsSigner.SignProofOfPossession(ipBytes)
	if err != nil {
		return nil, err
	}

	var mldsaSignature []byte
	if mldsaSigner != nil {
		mldsaSignature, err = mldsaSigner.SignCtx(rand.Reader, ipBytes, signedIPMLDSAContext)
		if err != nil {
			return nil, fmt.Errorf("ml-dsa-65 sign: %w", err)
		}
	}

	return &SignedIP{
		UnsignedIP:        *ip,
		TLSSignature:      tlsSignature,
		BLSSignature:      blsSignature,
		BLSSignatureBytes: bls.SignatureToBytes(blsSignature),
		MLDSASignature:    mldsaSignature,
	}, nil
}

func (ip *UnsignedIP) bytes() []byte {
	p := wrappers.Packer{
		Bytes: make([]byte, net.IPv6len+wrappers.ShortLen+wrappers.LongLen),
	}
	addrBytes := ip.AddrPort.Addr().As16()
	p.PackFixedBytes(addrBytes[:])
	p.PackShort(ip.AddrPort.Port())
	p.PackLong(ip.Timestamp)
	return p.Bytes
}

// SignedIP is a wrapper of an UnsignedIP with the signature from a signer.
//
// Wire-format note: the ML-DSA leg is append-only on the gossip wire.
// Legacy peers that never set MLDSASignature send an empty bytes field;
// the receiver-side Verify treats absence as "classical-only" and refuses
// it only when a strict-PQ profile is in force (see VerifyUnderProfile).
// This preserves backwards compatibility with non-strict-PQ chains
// (testnet, devnet) while enabling strict-PQ chains to refuse the
// quantum-broken-only path.
type SignedIP struct {
	UnsignedIP
	TLSSignature      []byte
	BLSSignature      *bls.Signature
	BLSSignatureBytes []byte
	// MLDSASignature is the FIPS 204 ML-DSA-65 signature over the
	// canonical UnsignedIP bytes under the "lux-peer-signed-ip-v1"
	// context. Empty on classical-only SignedIPs produced by legacy
	// peers; required under strict-PQ profile.
	MLDSASignature []byte
}

// Verify checks the classical (TLS) leg only. Retained for backwards
// compatibility with callers that do not have access to the chain's
// security profile or the validator's ML-DSA-65 public key. Strict-PQ
// callers MUST go through VerifyUnderProfile so the ML-DSA leg gets
// checked.
//
// Returns nil if:
//   - [ip.Timestamp] is within the allowed clock skew range (not too far
//     in past or future).
//   - [ip.TLSSignature] is a valid signature over [ip.UnsignedIP] from
//     [cert].
//
// [maxTimestamp] defines the maximum allowed timestamp (current time +
// MaxClockDifference). The minimum allowed timestamp is inferred as
// (maxTimestamp - reasonableClockSkewWindow) to prevent replay attacks.
// We use a conservative 10-minute window to account for various
// MaxClockDifference configurations while still protecting against
// replay attacks.
func (ip *SignedIP) Verify(
	cert *staking.Certificate,
	maxTimestamp time.Time,
) error {
	return ip.VerifyUnderProfile(cert, nil, maxTimestamp, false)
}

// VerifyUnderProfile is the profile-aware verifier. It always checks the
// TLS leg (classical), and additionally:
//
//   - When strictPQ == true, requires a non-empty MLDSASignature and
//     verifies it under mldsaPubKey. mldsaPubKey MUST be supplied; passing
//     a nil pubkey under strict-PQ is a configuration error (returns
//     errMissingMLDSAPublicKey). The strict-PQ profile is what every
//     Lux-derived strict-PQ chain pins (SigSchemeMLDSA65); permissive
//     profiles (testnet, devnet) call this with strictPQ == false.
//
//   - When strictPQ == false but MLDSASignature is present AND mldsaPubKey
//     is non-nil, the ML-DSA leg is still verified (defence in depth —
//     the wire bytes are already there, refusing to validate them would
//     waste evidence). Absence under permissive is tolerated.
//
// The BLS leg is verified by the caller separately (peer.shouldDisconnect
// runs BLS validation against the validator's BLS public key tracked in
// the platformvm validator set, not against the staker cert; that path
// is unchanged).
func (ip *SignedIP) VerifyUnderProfile(
	cert *staking.Certificate,
	mldsaPubKey *mldsa.PublicKey,
	maxTimestamp time.Time,
	strictPQ bool,
) error {
	maxUnixTimestamp := uint64(maxTimestamp.Unix())
	if ip.Timestamp > maxUnixTimestamp {
		return fmt.Errorf("%w: timestamp %d > maxTimestamp %d", errTimestampTooFarInFuture, ip.Timestamp, maxUnixTimestamp)
	}

	// Prevent replay attacks by rejecting timestamps too far in the past.
	// We use a conservative 10-minute total window. This accommodates:
	// - Default MaxClockDifference of 1 minute (2-minute total window)
	// - Larger MaxClockDifference configurations (up to 5 minutes)
	// - Edge cases where maxTimestamp might be slightly in the past due to test timing
	//
	// For example, with MaxClockDifference = 1 minute:
	// - Current time: 12:00
	// - maxTimestamp: 12:01
	// - minTimestamp: 11:51 (maxTimestamp - 10 min)
	// This prevents replay of IPs older than 10 minutes.
	const reasonableClockSkewWindow = 10 * time.Minute
	minTimestamp := maxTimestamp.Add(-reasonableClockSkewWindow)
	minUnixTimestamp := uint64(minTimestamp.Unix())
	if ip.Timestamp < minUnixTimestamp {
		return fmt.Errorf("%w: timestamp %d < minTimestamp %d", errTimestampTooFarInPast, ip.Timestamp, minUnixTimestamp)
	}

	ipBytes := ip.UnsignedIP.bytes()
	if err := staking.CheckSignature(
		cert,
		ipBytes,
		ip.TLSSignature,
	); err != nil {
		return fmt.Errorf("%w: %w", errInvalidTLSSignature, err)
	}

	// ML-DSA leg. Three cases:
	//
	//   strictPQ && empty sig          → refuse (errMissingMLDSASignature)
	//   strictPQ && nil pubkey         → refuse (errMissingMLDSAPublicKey)
	//   sig present && pubkey present  → verify; refuse on failure
	//   !strictPQ && empty sig         → tolerate (classical-only OK)
	//
	// We never refuse a permissive chain that supplies a pubkey but the
	// peer chose not to sign with ML-DSA — that combination is normal on
	// a mixed network and the strictPQ flag is what enforces the
	// "must have a sig" policy.
	if strictPQ {
		if len(ip.MLDSASignature) == 0 {
			return errMissingMLDSASignature
		}
		if mldsaPubKey == nil {
			return errMissingMLDSAPublicKey
		}
	}
	if len(ip.MLDSASignature) > 0 && mldsaPubKey != nil {
		if !mldsaPubKey.VerifySignatureCtx(ipBytes, ip.MLDSASignature, signedIPMLDSAContext) {
			return errInvalidMLDSASignature
		}
	}
	return nil
}
