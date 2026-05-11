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
// KeyExchangeID is declared in this package because the consensus toolkit at
// v1.23.2 does not yet export it from config/security_profile.go; once
// consensus rolls a release that ships it, this declaration becomes the
// canonical alias and the node consumes it from there. The wire bytes are
// chosen to be forward-compatible with the consensus block (0x60..0x6F for
// post-quantum KEMs; 0xF0..0xFF for explicit forbidden markers).
package kem

import "fmt"

// KeyExchangeID is the wire byte that identifies the key-exchange / KEM
// scheme a peer offers for session-key establishment. Orthogonal to
// SigSchemeID (identity / threshold finality) and HashSuiteID (transcript
// binding family).
//
// Numbering blocks:
//
//	0x00       — None / unspecified (rejected by every strict-PQ verifier)
//	0x60..0x6F — Post-quantum KEM (FIPS 203, ML-KEM family)
//	               0x61 = ML-KEM-512  (NIST PQ Cat 1; reserved, not used by node today)
//	               0x62 = ML-KEM-768  (NIST PQ Cat 3; **production default**)
//	               0x63 = ML-KEM-1024 (NIST PQ Cat 5; DKG / high-value channels)
//	0xF0..0xFF — Classical / forbidden in PQ mode (explicit markers)
//	               0xF0 = X25519        (RFC 7748; classical DH)
//	               0xF1 = P-256 ECDH    (FIPS 186-4 curve; classical)
//	               0xF2 = P-384 ECDH    (FIPS 186-4 curve; classical)
//
// The forbidden markers are NEVER produced by a strict-PQ node; they exist
// solely so a strict-PQ peer that receives one can refuse with a precise
// diagnostic instead of dropping silently.
type KeyExchangeID uint8

const (
	// KeyExchangeNone — no KEM declared. Rejected by every strict-PQ peer.
	KeyExchangeNone KeyExchangeID = 0x00

	// KeyExchangeMLKEM512 — FIPS 203 ML-KEM-512 (NIST PQ Cat 1). Reserved
	// for future use; the node does not currently negotiate this scheme
	// because Cat 1 is below the strict-PQ floor.
	KeyExchangeMLKEM512 KeyExchangeID = 0x61

	// KeyExchangeMLKEM768 — FIPS 203 ML-KEM-768 (NIST PQ Cat 3). The
	// production default for peer-to-peer session keys on the Lux primary
	// network. 1184-byte public key, 1088-byte ciphertext, 32-byte shared
	// secret.
	KeyExchangeMLKEM768 KeyExchangeID = 0x62

	// KeyExchangeMLKEM1024 — FIPS 203 ML-KEM-1024 (NIST PQ Cat 5).
	// Mandatory for high-value validator channels and Pulsar DKG sessions.
	// 1568-byte public key, 1568-byte ciphertext, 32-byte shared secret.
	KeyExchangeMLKEM1024 KeyExchangeID = 0x63

	// KeyExchangeX25519Unsafe — classical X25519. Carried as an explicit
	// forbidden marker; a strict-PQ peer refuses to handshake against any
	// peer offering this byte.
	KeyExchangeX25519Unsafe KeyExchangeID = 0xF0

	// KeyExchangeP256Unsafe — classical NIST P-256 ECDH. Forbidden marker.
	KeyExchangeP256Unsafe KeyExchangeID = 0xF1

	// KeyExchangeP384Unsafe — classical NIST P-384 ECDH. Forbidden marker.
	KeyExchangeP384Unsafe KeyExchangeID = 0xF2
)

// String returns the canonical wire name.
func (k KeyExchangeID) String() string {
	switch k {
	case KeyExchangeNone:
		return "none"
	case KeyExchangeMLKEM512:
		return "ml-kem-512"
	case KeyExchangeMLKEM768:
		return "ml-kem-768"
	case KeyExchangeMLKEM1024:
		return "ml-kem-1024"
	case KeyExchangeX25519Unsafe:
		return "x25519-classical-forbidden-in-pq"
	case KeyExchangeP256Unsafe:
		return "p256-ecdh-classical-forbidden-in-pq"
	case KeyExchangeP384Unsafe:
		return "p384-ecdh-classical-forbidden-in-pq"
	default:
		return fmt.Sprintf("key-exchange(0x%02x)", uint8(k))
	}
}

// IsPostQuantum reports whether this KEM is acceptable on a strict-PQ
// profile. Only the FIPS 203 ML-KEM family at NIST PQ Cat 3+ qualifies
// for production today; Cat 1 (ML-KEM-512) is reserved but below the
// strict-PQ floor.
func (k KeyExchangeID) IsPostQuantum() bool {
	switch k {
	case KeyExchangeMLKEM768, KeyExchangeMLKEM1024:
		return true
	}
	return false
}

// IsForbiddenInPQMode reports whether this KEM carries the explicit
// classical-forbidden marker. Strict-PQ peers refuse any peer offering
// such a byte regardless of context.
func (k KeyExchangeID) IsForbiddenInPQMode() bool {
	switch k {
	case KeyExchangeX25519Unsafe,
		KeyExchangeP256Unsafe,
		KeyExchangeP384Unsafe:
		return true
	}
	return false
}

// SharedSecretBits reports the shared-secret width emitted by this KEM in
// bits. Both ML-KEM-768 and ML-KEM-1024 emit 256-bit secrets per FIPS 203.
// Returns 0 for None and forbidden markers.
func (k KeyExchangeID) SharedSecretBits() uint16 {
	switch k {
	case KeyExchangeMLKEM768, KeyExchangeMLKEM1024:
		return 256
	}
	return 0
}

// NISTCategory reports the NIST PQ security category for this KEM. Cat 3
// targets AES-192 / SHA-384 strength; Cat 5 targets AES-256 / SHA-512.
// Returns 0 for None and forbidden markers.
func (k KeyExchangeID) NISTCategory() uint8 {
	switch k {
	case KeyExchangeMLKEM768:
		return 3
	case KeyExchangeMLKEM1024:
		return 5
	}
	return 0
}
