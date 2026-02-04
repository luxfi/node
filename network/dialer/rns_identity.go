// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/curve25519"

	"github.com/luxfi/net/ips"
)

// RNS Identity Constants
const (
	// Ed25519 key sizes
	ed25519SeedSize       = 32
	ed25519PrivateKeySize = 64
	ed25519PublicKeySize  = 32
	ed25519SignatureSize  = 64

	// X25519 key sizes
	x25519KeySize = 32

	// Identity file magic and version for format identification
	rnsIdentityMagic   = 0x524E5349 // "RNSI" in big endian
	rnsIdentityVersion = 1

	// Total identity file size: magic(4) + version(4) + ed25519_seed(32) = 40 bytes
	rnsIdentityFileSize = 40
)

var (
	// ErrInvalidIdentity is returned when identity data is malformed.
	ErrInvalidIdentity = errors.New("invalid RNS identity")
	// ErrInvalidSignature is returned when signature verification fails.
	ErrInvalidSignature = errors.New("invalid signature")
	// ErrDecryptionFailed is returned when decryption fails.
	ErrDecryptionFailed = errors.New("decryption failed")
)

// RNSIdentity represents a Reticulum Network Stack identity.
// It consists of an Ed25519 keypair for signing and an X25519 keypair for encryption.
// The destination hash is derived from the public keys.
type RNSIdentity struct {
	// Ed25519 signing keys
	edPrivateKey ed25519.PrivateKey
	edPublicKey  ed25519.PublicKey

	// X25519 encryption keys (derived from Ed25519 seed)
	xPrivateKey [x25519KeySize]byte
	xPublicKey  [x25519KeySize]byte

	// Destination hash: truncated SHA-256 of (edPublicKey || xPublicKey)
	destination [ips.RNSDestinationLen]byte
}

// NewRNSIdentity generates a new random RNS identity.
func NewRNSIdentity() (*RNSIdentity, error) {
	seed := make([]byte, ed25519SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("failed to generate random seed: %w", err)
	}
	return newRNSIdentityFromSeed(seed)
}

// newRNSIdentityFromSeed creates an identity from a 32-byte seed.
// This is deterministic: same seed produces same identity.
func newRNSIdentityFromSeed(seed []byte) (*RNSIdentity, error) {
	if len(seed) != ed25519SeedSize {
		return nil, fmt.Errorf("%w: seed must be %d bytes", ErrInvalidIdentity, ed25519SeedSize)
	}

	id := &RNSIdentity{}

	// Generate Ed25519 keypair from seed
	id.edPrivateKey = ed25519.NewKeyFromSeed(seed)
	id.edPublicKey = id.edPrivateKey.Public().(ed25519.PublicKey)

	// Derive X25519 keypair from Ed25519 seed
	// Hash the seed to get X25519 private key (avoid key reuse attacks)
	xSeedHash := sha256.Sum256(append([]byte("x25519-derive:"), seed...))
	copy(id.xPrivateKey[:], xSeedHash[:])

	// Clamp X25519 private key per RFC 7748
	id.xPrivateKey[0] &= 248
	id.xPrivateKey[31] &= 127
	id.xPrivateKey[31] |= 64

	// Compute X25519 public key
	curve25519.ScalarBaseMult(&id.xPublicKey, &id.xPrivateKey)

	// Compute destination hash: first 16 bytes of SHA-256(edPublicKey || xPublicKey)
	id.computeDestination()

	return id, nil
}

// computeDestination calculates the 128-bit destination hash.
func (id *RNSIdentity) computeDestination() {
	h := sha256.New()
	h.Write(id.edPublicKey)
	h.Write(id.xPublicKey[:])
	digest := h.Sum(nil)
	copy(id.destination[:], digest[:ips.RNSDestinationLen])
}

// Destination returns the 128-bit destination hash.
// This uniquely identifies the identity on the Reticulum network.
func (id *RNSIdentity) Destination() [ips.RNSDestinationLen]byte {
	return id.destination
}

// Hash returns the identity hash (alias for Destination).
// Used by the RNS link protocol.
func (id *RNSIdentity) Hash() [ips.RNSDestinationLen]byte {
	return id.destination
}

// SigningPublicKey returns the Ed25519 public key as a slice.
// Used for signature verification in handshakes.
func (id *RNSIdentity) SigningPublicKey() []byte {
	return id.edPublicKey
}

// X25519PublicKey returns the X25519 public key as a fixed-size array.
// Used for key exchange in handshakes.
func (id *RNSIdentity) X25519PublicKey() [x25519KeySize]byte {
	return id.xPublicKey
}

// X25519Exchange performs ECDH key exchange with the peer's X25519 public key.
// Returns a 32-byte shared secret.
func (id *RNSIdentity) X25519Exchange(peerPublicKey [x25519KeySize]byte) ([x25519KeySize]byte, error) {
	var sharedSecret [x25519KeySize]byte
	curve25519.ScalarMult(&sharedSecret, &id.xPrivateKey, &peerPublicKey)

	// Check for low-order points
	if isZero(sharedSecret[:]) {
		return sharedSecret, ErrDecryptionFailed
	}

	return sharedSecret, nil
}

// Sign creates an Ed25519 signature over the message.
func (id *RNSIdentity) Sign(message []byte) []byte {
	return ed25519.Sign(id.edPrivateKey, message)
}

// Verify checks an Ed25519 signature against this identity's public key.
func (id *RNSIdentity) Verify(message, signature []byte) bool {
	if len(signature) != ed25519SignatureSize {
		return false
	}
	return ed25519.Verify(id.edPublicKey, message, signature)
}

// VerifyWithPublicKey verifies a signature using an external public key.
// This is a static method for verifying signatures from other identities.
func VerifyWithPublicKey(publicKey, message, signature []byte) bool {
	if len(publicKey) != ed25519PublicKeySize || len(signature) != ed25519SignatureSize {
		return false
	}
	return ed25519.Verify(publicKey, message, signature)
}

// VerifyWithPubKey is an alias for VerifyWithPublicKey.
// Used by the RNS link protocol.
func VerifyWithPubKey(publicKey, message, signature []byte) bool {
	return VerifyWithPublicKey(publicKey, message, signature)
}

// Encrypt performs X25519 key exchange with the recipient's public key
// and returns the ephemeral public key and shared secret.
// The caller should use the shared secret with an AEAD cipher.
func (id *RNSIdentity) Encrypt(recipientXPublicKey []byte) (ephemeralPub []byte, sharedSecret []byte, err error) {
	if len(recipientXPublicKey) != x25519KeySize {
		return nil, nil, fmt.Errorf("%w: invalid recipient public key size", ErrInvalidIdentity)
	}

	// Generate ephemeral X25519 keypair
	var ephemeralPrivate, ephemeralPublic [x25519KeySize]byte
	if _, err := rand.Read(ephemeralPrivate[:]); err != nil {
		return nil, nil, fmt.Errorf("failed to generate ephemeral key: %w", err)
	}

	// Clamp ephemeral private key
	ephemeralPrivate[0] &= 248
	ephemeralPrivate[31] &= 127
	ephemeralPrivate[31] |= 64

	// Compute ephemeral public key
	curve25519.ScalarBaseMult(&ephemeralPublic, &ephemeralPrivate)

	// Compute shared secret: ECDH(ephemeralPrivate, recipientPublic)
	var recipientPub [x25519KeySize]byte
	copy(recipientPub[:], recipientXPublicKey)

	var secret [x25519KeySize]byte
	curve25519.ScalarMult(&secret, &ephemeralPrivate, &recipientPub)

	// Check for low-order points (all zeros)
	if isZero(secret[:]) {
		return nil, nil, ErrDecryptionFailed
	}

	// Derive final shared secret via HKDF-like construction
	// Hash: ephemeralPublic || secret to prevent related-key attacks
	finalSecret := sha256.Sum256(append(ephemeralPublic[:], secret[:]...))

	return ephemeralPublic[:], finalSecret[:], nil
}

// Decrypt recovers the shared secret from an ephemeral public key.
// The sender should have used Encrypt() to generate the ephemeral key.
func (id *RNSIdentity) Decrypt(ephemeralPublicKey []byte) (sharedSecret []byte, err error) {
	if len(ephemeralPublicKey) != x25519KeySize {
		return nil, fmt.Errorf("%w: invalid ephemeral public key size", ErrInvalidIdentity)
	}

	var ephemeralPub [x25519KeySize]byte
	copy(ephemeralPub[:], ephemeralPublicKey)

	// Compute shared secret: ECDH(xPrivateKey, ephemeralPublic)
	var secret [x25519KeySize]byte
	curve25519.ScalarMult(&secret, &id.xPrivateKey, &ephemeralPub)

	// Check for low-order points
	if isZero(secret[:]) {
		return nil, ErrDecryptionFailed
	}

	// Derive final shared secret (must match Encrypt)
	finalSecret := sha256.Sum256(append(ephemeralPublicKey, secret[:]...))

	return finalSecret[:], nil
}

// PublicKey returns the Ed25519 public key (32 bytes).
func (id *RNSIdentity) PublicKey() []byte {
	return id.edPublicKey
}

// EncryptionPublicKey returns the X25519 public key (32 bytes).
func (id *RNSIdentity) EncryptionPublicKey() []byte {
	return id.xPublicKey[:]
}

// Save persists the identity to a file.
// Only the seed is stored; keys are derived on load.
func (id *RNSIdentity) Save(path string) error {
	// Extract seed from Ed25519 private key (first 32 bytes)
	seed := id.edPrivateKey.Seed()

	data := make([]byte, rnsIdentityFileSize)
	binary.BigEndian.PutUint32(data[0:4], rnsIdentityMagic)
	binary.BigEndian.PutUint32(data[4:8], rnsIdentityVersion)
	copy(data[8:40], seed)

	// Write with restrictive permissions (owner read/write only)
	return os.WriteFile(path, data, 0600)
}

// LoadRNSIdentity loads an identity from a file.
func LoadRNSIdentity(path string) (*RNSIdentity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// Return raw error for not-found checks
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read identity file: %w", err)
	}

	if len(data) != rnsIdentityFileSize {
		return nil, fmt.Errorf("%w: invalid file size", ErrInvalidIdentity)
	}

	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != rnsIdentityMagic {
		return nil, fmt.Errorf("%w: invalid magic number", ErrInvalidIdentity)
	}

	version := binary.BigEndian.Uint32(data[4:8])
	if version != rnsIdentityVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidIdentity, version)
	}

	seed := data[8:40]
	return newRNSIdentityFromSeed(seed)
}

// LoadOrGenerateIdentity loads an identity from file or generates a new one.
// If the file does not exist, a new identity is generated and saved.
// If path is empty, a new ephemeral identity is generated (not saved).
func LoadOrGenerateIdentity(path string) (*RNSIdentity, error) {
	if path == "" {
		// Generate ephemeral identity
		return NewRNSIdentity()
	}

	// Try to load existing identity
	id, err := LoadRNSIdentity(path)
	if err == nil {
		return id, nil
	}

	// If file doesn't exist, generate and save new identity
	if os.IsNotExist(err) || errors.Is(err, ErrInvalidIdentity) {
		id, genErr := NewRNSIdentity()
		if genErr != nil {
			return nil, genErr
		}
		if saveErr := id.Save(path); saveErr != nil {
			return nil, fmt.Errorf("failed to save new identity: %w", saveErr)
		}
		return id, nil
	}

	return nil, err
}

// PublicIdentity represents a read-only identity from public keys.
// This is used to verify signatures and encrypt messages to a remote identity.
type PublicIdentity struct {
	edPublicKey [ed25519PublicKeySize]byte
	xPublicKey  [x25519KeySize]byte
	destination [ips.RNSDestinationLen]byte
}

// NewPublicIdentity creates a public identity from Ed25519 and X25519 public keys.
func NewPublicIdentity(edPublicKey, xPublicKey []byte) (*PublicIdentity, error) {
	if len(edPublicKey) != ed25519PublicKeySize {
		return nil, fmt.Errorf("%w: Ed25519 public key must be %d bytes", ErrInvalidIdentity, ed25519PublicKeySize)
	}
	if len(xPublicKey) != x25519KeySize {
		return nil, fmt.Errorf("%w: X25519 public key must be %d bytes", ErrInvalidIdentity, x25519KeySize)
	}

	pi := &PublicIdentity{}
	copy(pi.edPublicKey[:], edPublicKey)
	copy(pi.xPublicKey[:], xPublicKey)

	// Compute destination hash
	h := sha256.New()
	h.Write(pi.edPublicKey[:])
	h.Write(pi.xPublicKey[:])
	digest := h.Sum(nil)
	copy(pi.destination[:], digest[:ips.RNSDestinationLen])

	return pi, nil
}

// Destination returns the 128-bit destination hash.
func (pi *PublicIdentity) Destination() [ips.RNSDestinationLen]byte {
	return pi.destination
}

// Verify checks an Ed25519 signature against this identity's public key.
func (pi *PublicIdentity) Verify(message, signature []byte) bool {
	if len(signature) != ed25519SignatureSize {
		return false
	}
	return ed25519.Verify(pi.edPublicKey[:], message, signature)
}

// EncryptionPublicKey returns the X25519 public key.
func (pi *PublicIdentity) EncryptionPublicKey() []byte {
	return pi.xPublicKey[:]
}

// PublicKey returns the Ed25519 public key.
func (pi *PublicIdentity) PublicKey() []byte {
	return pi.edPublicKey[:]
}

// MarshalBinary serializes the public identity to bytes.
// Format: edPublicKey (32) || xPublicKey (32) = 64 bytes
func (pi *PublicIdentity) MarshalBinary() ([]byte, error) {
	data := make([]byte, ed25519PublicKeySize+x25519KeySize)
	copy(data[0:ed25519PublicKeySize], pi.edPublicKey[:])
	copy(data[ed25519PublicKeySize:], pi.xPublicKey[:])
	return data, nil
}

// UnmarshalPublicIdentity deserializes a public identity from bytes.
func UnmarshalPublicIdentity(data []byte) (*PublicIdentity, error) {
	if len(data) != ed25519PublicKeySize+x25519KeySize {
		return nil, fmt.Errorf("%w: expected %d bytes", ErrInvalidIdentity, ed25519PublicKeySize+x25519KeySize)
	}
	return NewPublicIdentity(data[:ed25519PublicKeySize], data[ed25519PublicKeySize:])
}

// isZero checks if a byte slice is all zeros (constant-time).
func isZero(b []byte) bool {
	var acc byte
	for _, v := range b {
		acc |= v
	}
	return acc == 0
}

// DestinationFromPublicKeys computes the destination hash from public keys.
// This is useful for computing destinations without creating full identity objects.
func DestinationFromPublicKeys(edPublicKey, xPublicKey []byte) ([ips.RNSDestinationLen]byte, error) {
	var dest [ips.RNSDestinationLen]byte

	if len(edPublicKey) != ed25519PublicKeySize {
		return dest, fmt.Errorf("%w: Ed25519 public key must be %d bytes", ErrInvalidIdentity, ed25519PublicKeySize)
	}
	if len(xPublicKey) != x25519KeySize {
		return dest, fmt.Errorf("%w: X25519 public key must be %d bytes", ErrInvalidIdentity, x25519KeySize)
	}

	h := sha256.New()
	h.Write(edPublicKey)
	h.Write(xPublicKey)
	digest := h.Sum(nil)
	copy(dest[:], digest[:ips.RNSDestinationLen])

	return dest, nil
}

// Compile-time interface satisfaction check
var _ io.Closer = (*RNSIdentity)(nil)

// Close clears sensitive key material from memory.
// This should be called when the identity is no longer needed.
func (id *RNSIdentity) Close() error {
	// Zero out private keys
	for i := range id.edPrivateKey {
		id.edPrivateKey[i] = 0
	}
	for i := range id.xPrivateKey {
		id.xPrivateKey[i] = 0
	}
	return nil
}
