// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package kem implements post-quantum key encapsulation for the Lux node peer
// handshake. ML-KEM-768 (FIPS 203, NIST PQ Cat 3) is the default session KEM;
// ML-KEM-1024 (NIST PQ Cat 5) is mandatory for high-value validator and DKG
// channels.
//
// The session shared secret is derived through one FIPS 203 encapsulate /
// decapsulate dance and then bound to the handshake transcript via cSHAKE256
// with customization "LUX_NODE_AEAD_V1". The bound key is the AEAD session
// key used by every subsequent frame on the connection.
//
// One and only one KEM family is admitted on a strict-PQ profile: ML-KEM.
// Classical X25519 / ECDH are present in the wire enum solely as forbidden
// markers so audit tooling can name a misconfiguration precisely; a strict-PQ
// peer with ForbidClassicalKEM=true refuses to handshake against any peer
// offering KeyExchangeX25519Unsafe.
//
// KeyExchangeID is a type alias of config.KeyExchangeID; the canonical wire
// bytes (0x01 = ML-KEM-768, 0x02 = ML-KEM-1024, 0x90 = X25519Unsafe) live
// in the consensus config package and are shared with protocol/auth. Closes
// the "three disjoint KEM ID registries" drift named in the spec/code audit
// under Bug 3.
package kem

import (
	"github.com/luxfi/consensus/config"
)

// KeyExchangeID is the wire byte that identifies the key-exchange / KEM
// scheme a peer offers for session-key establishment. Type-aliased to
// config.KeyExchangeID so the wire format and predicates (String,
// IsPostQuantum, IsForbiddenInPQMode) are shared with the consensus
// security profile.
//
// Canonical numbering:
//
//	0x00 — Invalid / unspecified (rejected by every strict-PQ verifier)
//	0x01 — ML-KEM-768  (FIPS 203 Cat 3; production default)
//	0x02 — ML-KEM-1024 (FIPS 203 Cat 5; DKG / high-value channels)
//	0x90 — X25519Unsafe (classical; explicit forbidden marker in strict-PQ)
//
// The forbidden markers are NEVER produced by a strict-PQ node; they exist
// solely so a strict-PQ peer that receives one can refuse with a precise
// diagnostic instead of dropping silently.
//
// ML-KEM-512 (NIST Cat 1) is intentionally not exported — it sits below the
// strict-PQ floor; the node does not negotiate Cat 1 sessions.
type KeyExchangeID = config.KeyExchangeID

const (
	// KeyExchangeNone — alias for config.KeyExchangeInvalid (0x00). Rejected
	// by every strict-PQ peer.
	KeyExchangeNone = config.KeyExchangeInvalid

	// KeyExchangeMLKEM768 — FIPS 203 ML-KEM-768 (NIST PQ Cat 3, byte 0x01).
	// The production default for peer-to-peer session keys on the Lux
	// primary network. 1184-byte public key, 1088-byte ciphertext, 32-byte
	// shared secret.
	KeyExchangeMLKEM768 = config.KeyExchangeMLKEM768

	// KeyExchangeMLKEM1024 — FIPS 203 ML-KEM-1024 (NIST PQ Cat 5, byte 0x02).
	// Mandatory for high-value validator channels and Pulsar DKG sessions.
	// 1568-byte public key, 1568-byte ciphertext, 32-byte shared secret.
	KeyExchangeMLKEM1024 = config.KeyExchangeMLKEM1024

	// KeyExchangeX25519Unsafe — classical X25519 (byte 0x90). Carried as an
	// explicit forbidden marker; a strict-PQ peer refuses to handshake
	// against any peer offering this byte.
	KeyExchangeX25519Unsafe = config.KeyExchangeX25519Unsafe
)

// SharedSecretBits reports the shared-secret width emitted by scheme in
// bits. Both ML-KEM-768 and ML-KEM-1024 emit 256-bit secrets per FIPS 203.
// Returns 0 for invalid and forbidden markers.
//
// Free function rather than a method because KeyExchangeID is a type alias
// of config.KeyExchangeID; methods can only be declared in the package that
// defines the underlying type.
func SharedSecretBits(scheme KeyExchangeID) uint16 {
	switch scheme {
	case KeyExchangeMLKEM768, KeyExchangeMLKEM1024:
		return 256
	}
	return 0
}

// NISTCategory reports the NIST PQ security category for scheme. Cat 3
// targets AES-192 / SHA-384 strength; Cat 5 targets AES-256 / SHA-512.
// Returns 0 for invalid and forbidden markers.
//
// Free function for the same reason as SharedSecretBits.
func NISTCategory(scheme KeyExchangeID) uint8 {
	switch scheme {
	case KeyExchangeMLKEM768:
		return 3
	case KeyExchangeMLKEM1024:
		return 5
	}
	return 0
}
