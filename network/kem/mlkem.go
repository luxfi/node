// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kem

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/cloudflare/circl/kem/mlkem/mlkem1024"
	"github.com/cloudflare/circl/kem/mlkem/mlkem768"
	"golang.org/x/crypto/sha3"
)

// Customization strings for the cSHAKE256 derivations performed by this
// package. Pinned at "_V1"; bumping the tag invalidates every prior derived
// key (which is the correct behaviour for a hard-fork of the encoding).
//
// NODE_AEAD_V1   — peer-to-peer session AEAD key
// NODE_DKG_V1    — DKG channel AEAD key (high-value, ML-KEM-1024 only)
// NODE_TRANSCRIPT_V1 — TupleHash256 customization for handshake transcript
const (
	customizationAEAD       = "NODE_AEAD_V1"
	customizationDKGAEAD    = "NODE_DKG_V1"
	customizationTranscript = "NODE_TRANSCRIPT_V1"

	// TranscriptHashSize is the byte width of the bound handshake
	// transcript hash returned by HashTranscript and stored in KEMSession.
	// 48 bytes = 384 bits matches HashSuiteSHA3NIST in consensus/config and
	// the chain-wide profile ProfileHash width.
	TranscriptHashSize = 48

	// SharedSecretSize is the FIPS 203 ML-KEM shared-secret width in
	// bytes. ML-KEM-768 and ML-KEM-1024 both emit 32 bytes (256 bits).
	SharedSecretSize = 32

	// AEADKeySize is the byte width of the derived AEAD session key.
	// 32 bytes targets ChaCha20-Poly1305 or AES-256-GCM.
	AEADKeySize = 32
)

// Errors returned by KEM session APIs. Each names exactly which contract
// was violated so callers can produce a precise diagnostic.
var (
	// ErrUnsupportedScheme is returned when the requested KeyExchangeID is
	// not a supported ML-KEM variant. Includes None and any forbidden
	// classical marker.
	ErrUnsupportedScheme = errors.New("kem: unsupported scheme")

	// ErrClassicalKEMForbidden is returned when a strict-PQ caller receives
	// or attempts to negotiate an explicit classical KEM marker
	// (X25519Unsafe / P256Unsafe / P384Unsafe).
	ErrClassicalKEMForbidden = errors.New("kem: classical KEM forbidden in strict-PQ mode")

	// ErrBadCiphertextSize is returned when the encapsulated ciphertext is
	// not the scheme's canonical CiphertextSize.
	ErrBadCiphertextSize = errors.New("kem: ciphertext size does not match scheme")

	// ErrBadPublicKeySize is returned when the peer's encapsulation key is
	// not the scheme's canonical PublicKeySize.
	ErrBadPublicKeySize = errors.New("kem: public key size does not match scheme")

	// ErrBadPrivateKeySize is returned when the caller-supplied
	// decapsulation key is not the scheme's canonical PrivateKeySize.
	ErrBadPrivateKeySize = errors.New("kem: private key size does not match scheme")

	// ErrEmptyTranscript is returned when transcript bytes are nil or
	// empty. The whole point of transcript binding is that the AEAD key
	// depends on every prior handshake message; a zero-length transcript
	// is a contract violation.
	ErrEmptyTranscript = errors.New("kem: transcript is empty")
)

// KEMSession is the result of one successful ML-KEM encapsulate /
// decapsulate. It carries the scheme byte, the raw 256-bit shared secret,
// and the 384-bit running transcript hash (cSHAKE256 / TupleHash256 over
// every handshake message produced before the KEM round, in canonical
// order). The AEAD key is derived from SharedSecret + TranscriptHash via
// cSHAKE256 customisation "NODE_AEAD_V1".
//
// Field invariants enforced by package APIs:
//
//   - SchemeID ∈ {MLKEM768, MLKEM1024}; any other scheme returns
//     ErrUnsupportedScheme at construction time.
//   - SharedSecret is the raw FIPS 203 ML-KEM.Decapsulate / Encapsulate
//     output. Length is always exactly SharedSecretSize.
//   - TranscriptHash is the 48-byte SHA3-384 (cSHAKE256) commitment over
//     the handshake transcript. Two distinct transcripts MUST produce
//     two distinct TranscriptHash values.
//
// KEMSession does not zero its SharedSecret on drop because Go does not
// provide a deterministic destructor; callers that need defensive zeroing
// must do it explicitly after deriving the AEAD key.
type KEMSession struct {
	// SchemeID is the FIPS 203 ML-KEM scheme this session was negotiated
	// under. Strict-PQ peers admit only MLKEM768 (default) and MLKEM1024
	// (high-value / DKG).
	SchemeID KeyExchangeID

	// SharedSecret is the 256-bit raw KEM output. Treat as confidential;
	// derive subordinate keys through cSHAKE256 (see DeriveAEADKey), do
	// not use directly as an AEAD key.
	SharedSecret [SharedSecretSize]byte

	// TranscriptHash is the 384-bit cSHAKE256 / TupleHash256 commitment to
	// the handshake transcript at the point of KEM completion. The hash
	// is BOUND into DeriveAEADKey so a flipped transcript byte produces a
	// distinct AEAD key — that is, an attacker who downgrades a single
	// handshake field gets a key the honest peer never derived.
	TranscriptHash [TranscriptHashSize]byte
}

// HashTranscript returns the canonical cSHAKE256 / TupleHash256 commitment
// to msgs in the order they appear. Customization is
// "NODE_TRANSCRIPT_V1" so two different handshake protocols cannot
// produce the same hash from the same byte content.
//
// The encoding is SP 800-185 TupleHash256: each input is left-encoded with
// its bit-length prefix, the right-encoded total output bit-length is
// appended, and the result is fed to cSHAKE256 with the function-name
// "TupleHash" and customisation "NODE_TRANSCRIPT_V1". This matches
// the helper used by ChainSecurityProfile.ComputeHash, so the same
// transcript hash byte-encodes identically across the node and consensus.
func HashTranscript(msgs ...[]byte) [TranscriptHashSize]byte {
	var x []byte
	for _, msg := range msgs {
		x = append(x, encodeStringSP800185(msg)...)
	}
	x = append(x, rightEncodeSP800185(uint64(TranscriptHashSize)*8)...)

	h := sha3.NewCShake256([]byte("TupleHash"), []byte(customizationTranscript))
	_, _ = h.Write(x)
	out := make([]byte, TranscriptHashSize)
	_, _ = h.Read(out)

	var digest [TranscriptHashSize]byte
	copy(digest[:], out)
	return digest
}

// GenerateKEMKeypair returns a fresh (encapsulation-key, decapsulation-key)
// pair for scheme. randSrc may be nil, in which case crypto/rand.Reader is
// used. Returns ErrUnsupportedScheme for anything outside the supported
// ML-KEM family.
//
// Sizes:
//
//	MLKEM768   pub=1184 priv=2400 bytes
//	MLKEM1024  pub=1568 priv=3168 bytes
func GenerateKEMKeypair(scheme KeyExchangeID, randSrc io.Reader) (pub, priv []byte, err error) {
	if randSrc == nil {
		randSrc = rand.Reader
	}
	if scheme.IsForbiddenInPQMode() {
		return nil, nil, fmt.Errorf("%w: scheme=%s", ErrClassicalKEMForbidden, scheme)
	}
	switch scheme {
	case KeyExchangeMLKEM768:
		ek, dk, kerr := mlkem768.GenerateKeyPair(randSrc)
		if kerr != nil {
			return nil, nil, fmt.Errorf("kem: mlkem768 GenerateKeyPair: %w", kerr)
		}
		pkBytes := make([]byte, mlkem768.PublicKeySize)
		ek.Pack(pkBytes)
		skBytes := make([]byte, mlkem768.PrivateKeySize)
		dk.Pack(skBytes)
		return pkBytes, skBytes, nil
	case KeyExchangeMLKEM1024:
		ek, dk, kerr := mlkem1024.GenerateKeyPair(randSrc)
		if kerr != nil {
			return nil, nil, fmt.Errorf("kem: mlkem1024 GenerateKeyPair: %w", kerr)
		}
		pkBytes := make([]byte, mlkem1024.PublicKeySize)
		ek.Pack(pkBytes)
		skBytes := make([]byte, mlkem1024.PrivateKeySize)
		dk.Pack(skBytes)
		return pkBytes, skBytes, nil
	default:
		return nil, nil, fmt.Errorf("%w: scheme=%s", ErrUnsupportedScheme, scheme)
	}
}

// PublicKeySize returns the canonical encapsulation-key byte width for
// scheme. Useful for fixed-width framing on the wire.
func PublicKeySize(scheme KeyExchangeID) (int, error) {
	switch scheme {
	case KeyExchangeMLKEM768:
		return mlkem768.PublicKeySize, nil
	case KeyExchangeMLKEM1024:
		return mlkem1024.PublicKeySize, nil
	}
	return 0, fmt.Errorf("%w: scheme=%s", ErrUnsupportedScheme, scheme)
}

// CiphertextSize returns the canonical KEM ciphertext byte width for
// scheme.
func CiphertextSize(scheme KeyExchangeID) (int, error) {
	switch scheme {
	case KeyExchangeMLKEM768:
		return mlkem768.CiphertextSize, nil
	case KeyExchangeMLKEM1024:
		return mlkem1024.CiphertextSize, nil
	}
	return 0, fmt.Errorf("%w: scheme=%s", ErrUnsupportedScheme, scheme)
}

// InitiateKEMSession performs the initiator side of a one-shot ML-KEM
// session establishment. peerKEMPub is the responder's encapsulation key
// (already received on the wire); transcript is the bound running hash
// over every handshake message produced before this call.
//
// Returns the resulting KEMSession plus the encapsulation ciphertext to
// send to the responder. The caller is responsible for binding the
// ciphertext into the post-KEM transcript before deriving subordinate
// keys (the cSHAKE256 customisation inside DeriveAEADKey hard-binds the
// transcript bytes the caller passes here).
func InitiateKEMSession(
	scheme KeyExchangeID,
	peerKEMPub []byte,
	transcript []byte,
) (*KEMSession, []byte, error) {
	if scheme.IsForbiddenInPQMode() {
		return nil, nil, fmt.Errorf("%w: scheme=%s", ErrClassicalKEMForbidden, scheme)
	}
	if len(transcript) == 0 {
		return nil, nil, ErrEmptyTranscript
	}

	switch scheme {
	case KeyExchangeMLKEM768:
		if len(peerKEMPub) != mlkem768.PublicKeySize {
			return nil, nil, fmt.Errorf("%w: scheme=%s want=%d got=%d",
				ErrBadPublicKeySize, scheme, mlkem768.PublicKeySize, len(peerKEMPub))
		}
		var pk mlkem768.PublicKey
		if err := pk.Unpack(peerKEMPub); err != nil {
			return nil, nil, fmt.Errorf("kem: mlkem768 Unpack pub: %w", err)
		}
		ct := make([]byte, mlkem768.CiphertextSize)
		ssBuf := make([]byte, mlkem768.SharedKeySize)
		pk.EncapsulateTo(ct, ssBuf, nil)
		return finishSession(scheme, ssBuf, transcript), ct, nil

	case KeyExchangeMLKEM1024:
		if len(peerKEMPub) != mlkem1024.PublicKeySize {
			return nil, nil, fmt.Errorf("%w: scheme=%s want=%d got=%d",
				ErrBadPublicKeySize, scheme, mlkem1024.PublicKeySize, len(peerKEMPub))
		}
		var pk mlkem1024.PublicKey
		if err := pk.Unpack(peerKEMPub); err != nil {
			return nil, nil, fmt.Errorf("kem: mlkem1024 Unpack pub: %w", err)
		}
		ct := make([]byte, mlkem1024.CiphertextSize)
		ssBuf := make([]byte, mlkem1024.SharedKeySize)
		pk.EncapsulateTo(ct, ssBuf, nil)
		return finishSession(scheme, ssBuf, transcript), ct, nil

	default:
		return nil, nil, fmt.Errorf("%w: scheme=%s", ErrUnsupportedScheme, scheme)
	}
}

// RespondKEMSession performs the responder side: decapsulate the
// initiator's ciphertext under the responder's decapsulation key and
// produce the shared session.
//
// transcript MUST be byte-identical to the transcript the initiator used.
// If the two sides disagree by even one byte, their derived AEAD keys
// diverge and the first encrypted frame fails authentication.
func RespondKEMSession(
	scheme KeyExchangeID,
	ourKEMSec []byte,
	peerCiphertext []byte,
	transcript []byte,
) (*KEMSession, error) {
	if scheme.IsForbiddenInPQMode() {
		return nil, fmt.Errorf("%w: scheme=%s", ErrClassicalKEMForbidden, scheme)
	}
	if len(transcript) == 0 {
		return nil, ErrEmptyTranscript
	}

	switch scheme {
	case KeyExchangeMLKEM768:
		if len(ourKEMSec) != mlkem768.PrivateKeySize {
			return nil, fmt.Errorf("%w: scheme=%s want=%d got=%d",
				ErrBadPrivateKeySize, scheme, mlkem768.PrivateKeySize, len(ourKEMSec))
		}
		if len(peerCiphertext) != mlkem768.CiphertextSize {
			return nil, fmt.Errorf("%w: scheme=%s want=%d got=%d",
				ErrBadCiphertextSize, scheme, mlkem768.CiphertextSize, len(peerCiphertext))
		}
		var sk mlkem768.PrivateKey
		if err := sk.Unpack(ourKEMSec); err != nil {
			return nil, fmt.Errorf("kem: mlkem768 Unpack priv: %w", err)
		}
		ssBuf := make([]byte, mlkem768.SharedKeySize)
		sk.DecapsulateTo(ssBuf, peerCiphertext)
		return finishSession(scheme, ssBuf, transcript), nil

	case KeyExchangeMLKEM1024:
		if len(ourKEMSec) != mlkem1024.PrivateKeySize {
			return nil, fmt.Errorf("%w: scheme=%s want=%d got=%d",
				ErrBadPrivateKeySize, scheme, mlkem1024.PrivateKeySize, len(ourKEMSec))
		}
		if len(peerCiphertext) != mlkem1024.CiphertextSize {
			return nil, fmt.Errorf("%w: scheme=%s want=%d got=%d",
				ErrBadCiphertextSize, scheme, mlkem1024.CiphertextSize, len(peerCiphertext))
		}
		var sk mlkem1024.PrivateKey
		if err := sk.Unpack(ourKEMSec); err != nil {
			return nil, fmt.Errorf("kem: mlkem1024 Unpack priv: %w", err)
		}
		ssBuf := make([]byte, mlkem1024.SharedKeySize)
		sk.DecapsulateTo(ssBuf, peerCiphertext)
		return finishSession(scheme, ssBuf, transcript), nil

	default:
		return nil, fmt.Errorf("%w: scheme=%s", ErrUnsupportedScheme, scheme)
	}
}

// finishSession assembles a KEMSession from a raw KEM shared secret and a
// bound transcript. Internal helper; the public Initiate/Respond entry
// points dispatch through it so the SchemeID byte and the transcript
// commitment are written once, in one place.
func finishSession(scheme KeyExchangeID, ssBuf, transcript []byte) *KEMSession {
	s := &KEMSession{SchemeID: scheme}
	copy(s.SharedSecret[:], ssBuf)
	s.TranscriptHash = HashTranscript(transcript)
	return s
}

// DeriveAEADKey returns the 256-bit AEAD session key bound to this KEM
// session's shared secret AND its transcript hash. Uses cSHAKE256 with
// customisation "NODE_AEAD_V1"; the function name is "KEMDerive".
//
// Two sessions with the same SharedSecret but different TranscriptHash
// produce two different keys. Two sessions with the same TranscriptHash
// but different SharedSecret also produce two different keys. This is
// the property the strict-PQ profile depends on for downgrade resistance.
func (s *KEMSession) DeriveAEADKey() [AEADKeySize]byte {
	h := sha3.NewCShake256([]byte("KEMDerive"), []byte(customizationAEAD))
	_, _ = h.Write([]byte{byte(s.SchemeID)})
	_, _ = h.Write(s.SharedSecret[:])
	_, _ = h.Write(s.TranscriptHash[:])

	out := make([]byte, AEADKeySize)
	_, _ = h.Read(out)

	var key [AEADKeySize]byte
	copy(key[:], out)
	return key
}

// DeriveDKGAEADKey returns the 256-bit AEAD session key bound to this KEM
// session under the DKG-channel customisation "NODE_DKG_V1". A DKG
// channel session is required by the strict-PQ profile to negotiate
// ML-KEM-1024; this function refuses to derive a key on any other scheme
// so a misconfigured caller fails loud at key derivation rather than
// silently downgrading.
//
// Returns ErrUnsupportedScheme if the session was negotiated under
// anything other than KeyExchangeMLKEM1024.
func (s *KEMSession) DeriveDKGAEADKey() ([AEADKeySize]byte, error) {
	var zero [AEADKeySize]byte
	if s.SchemeID != KeyExchangeMLKEM1024 {
		return zero, fmt.Errorf("%w: DKG channels require ML-KEM-1024, got %s",
			ErrUnsupportedScheme, s.SchemeID)
	}

	h := sha3.NewCShake256([]byte("KEMDerive"), []byte(customizationDKGAEAD))
	_, _ = h.Write([]byte{byte(s.SchemeID)})
	_, _ = h.Write(s.SharedSecret[:])
	_, _ = h.Write(s.TranscriptHash[:])

	out := make([]byte, AEADKeySize)
	_, _ = h.Read(out)

	var key [AEADKeySize]byte
	copy(key[:], out)
	return key, nil
}

// encodeStringSP800185 is the SP 800-185 §2.3 left_encode prefix + raw
// bytes. Used by HashTranscript; matches the helper in
// consensus/config/security_profile.go byte-for-byte so the two encodings
// produce identical transcript bytes for identical inputs.
func encodeStringSP800185(s []byte) []byte {
	out := leftEncodeSP800185(uint64(len(s)) * 8)
	out = append(out, s...)
	return out
}

func leftEncodeSP800185(x uint64) []byte {
	if x == 0 {
		return []byte{0x01, 0x00}
	}
	var buf [8]byte
	for i := 0; i < 8; i++ {
		buf[7-i] = byte(x >> (8 * i))
	}
	i := 0
	for i < 7 && buf[i] == 0 {
		i++
	}
	out := make([]byte, 0, 9-i)
	out = append(out, byte(8-i))
	out = append(out, buf[i:]...)
	return out
}

func rightEncodeSP800185(x uint64) []byte {
	if x == 0 {
		return []byte{0x00, 0x01}
	}
	var buf [8]byte
	for i := 0; i < 8; i++ {
		buf[7-i] = byte(x >> (8 * i))
	}
	i := 0
	for i < 7 && buf[i] == 0 {
		i++
	}
	out := make([]byte, 0, 9-i)
	out = append(out, buf[i:]...)
	out = append(out, byte(8-i))
	return out
}
