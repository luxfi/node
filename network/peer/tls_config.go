// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"io"

	"github.com/luxfi/node/staking"
)

var (
	ErrNoCertsSent        = errors.New("no certificates sent by peer")
	ErrEmptyCert          = errors.New("certificate sent by peer is empty")
	ErrEmptyPublicKey     = errors.New("no public key sent by peer")
	ErrCurveMismatch      = errors.New("only P256 is allowed for ECDSA")
	ErrUnsupportedKeyType = errors.New("key type is not supported")
	ErrTLS13Required      = errors.New("TLS 1.3 is required")
)

// TLSConfig returns the TLS config that will allow secure connections to other
// peers.
//
// It is safe, and typically expected, for [keyLogWriter] to be [nil].
// [keyLogWriter] should only be enabled for debugging.
//
// F103 KEM semantics — TLS curve preference:
//
// CurvePreferences pins the IANA-registered hybrid X25519MLKEM768
// (curve ID 0x11ec) as the only acceptable curve. This is STRICTLY
// STRONGER than pure ML-KEM-768: an attacker must break both X25519
// AND ML-KEM-768 to derive the session key. The chain-wide
// ChainSecurityProfile pins KeyExchangeMLKEM768 — the post-quantum
// component required on the wire — and the hybrid satisfies that
// requirement (it CONTAINS ML-KEM-768). ForbidClassicalKEM in the
// profile refuses a pure-classical curve (e.g. X25519 alone) at the
// application layer; it does NOT refuse a hybrid that includes
// ML-KEM-768. See consensus/config.KeyExchangeID for the full
// rationale.
//
// Decision recorded by F103: keep the hybrid as the only TLS-layer
// KEM. Adding a separate "pure vs hybrid" enum byte to the profile
// would multiply downstream verifier obligations without strengthening
// the posture; the audit signed off on the single-byte KeyExchangeID
// surface and the hybrid is mathematically stronger than pure.
func TLSConfig(cert tls.Certificate, keyLogWriter io.Writer) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		// We do not use the TLS CA functionality to authenticate a
		// hostname. We only require an authenticated channel based on the
		// peer's public key. Therefore, we can safely skip CA verification.
		//
		// During our security audit by Quantstamp, this was investigated
		// and confirmed to be safe and correct.
		InsecureSkipVerify: true, //#nosec G402
		MinVersion:         tls.VersionTLS13,
		MaxVersion:         tls.VersionTLS13,
		CurvePreferences:   []tls.CurveID{tls.X25519MLKEM768},
		KeyLogWriter:       keyLogWriter,
		VerifyConnection:   ValidatePQConnection,
	}
}

// ValidatePQConnection enforces TLS 1.3 and validates the peer certificate.
func ValidatePQConnection(cs tls.ConnectionState) error {
	if cs.Version != tls.VersionTLS13 {
		return ErrTLS13Required
	}
	return ValidateCertificate(cs)
}

// ValidateCertificate validates TLS certificates according their public keys on the leaf certificate in the certification chain.
func ValidateCertificate(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return ErrNoCertsSent
	}

	if cs.PeerCertificates[0] == nil {
		return ErrEmptyCert
	}

	pk := cs.PeerCertificates[0].PublicKey

	switch key := pk.(type) {
	case *ecdsa.PublicKey:
		if key == nil {
			return ErrEmptyPublicKey
		}
		if key.Curve != elliptic.P256() {
			return ErrCurveMismatch
		}
		return nil
	case *rsa.PublicKey:
		return staking.ValidateRSAPublicKeyIsWellFormed(key)
	default:
		return ErrUnsupportedKeyType
	}
}
