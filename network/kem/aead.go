// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kem

import (
	"crypto/cipher"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// AEADID is the wire byte identifying the authenticated cipher used on
// post-handshake frames. Only FIPS-approved primitives are admitted; the
// production default is ChaCha20-Poly1305 (RFC 8439) because the Lux
// strict-PQ profile pairs lattice KEM with a stream cipher that is not
// vulnerable to the same lattice attack surface.
//
// Numbering:
//
//	0x00 — None (rejected)
//	0x01 — ChaCha20-Poly1305 (RFC 8439, FIPS 140 module-approved)
type AEADID uint8

const (
	AEADNone             AEADID = 0x00
	AEADChaCha20Poly1305 AEADID = 0x01
)

// String returns the canonical wire name.
func (a AEADID) String() string {
	switch a {
	case AEADNone:
		return "none"
	case AEADChaCha20Poly1305:
		return "chacha20-poly1305"
	default:
		return fmt.Sprintf("aead(0x%02x)", uint8(a))
	}
}

// ErrUnsupportedAEAD is returned by NewAEAD when an unknown / forbidden
// AEAD byte is requested.
var ErrUnsupportedAEAD = errors.New("kem: unsupported AEAD")

// NewAEAD constructs a cipher.AEAD bound to key. Caller produces key via
// KEMSession.DeriveAEADKey or KEMSession.DeriveDKGAEADKey. Returns
// ErrUnsupportedAEAD for any byte other than AEADChaCha20Poly1305.
//
// This is the single dispatch point that maps an AEADID byte to a
// concrete cipher; transport code routes through it instead of importing
// chacha20poly1305 directly so a future AEAD addition (e.g. AES-256-GCM
// at 0x02) lands in one place.
func NewAEAD(id AEADID, key []byte) (cipher.AEAD, error) {
	switch id {
	case AEADChaCha20Poly1305:
		if len(key) != chacha20poly1305.KeySize {
			return nil, fmt.Errorf("kem: ChaCha20-Poly1305 key size=%d want=%d",
				len(key), chacha20poly1305.KeySize)
		}
		aead, err := chacha20poly1305.New(key)
		if err != nil {
			return nil, fmt.Errorf("kem: chacha20poly1305.New: %w", err)
		}
		return aead, nil
	default:
		return nil, fmt.Errorf("%w: id=%s", ErrUnsupportedAEAD, id)
	}
}
