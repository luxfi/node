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

	"github.com/luxfi/crypto/kem"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/net/endpoints"
)

// Hybrid identity constants
const (
	// Magic number for hybrid identity files: "RNSH" (RNS Hybrid)
	hybridIdentityMagic   = 0x524E5348
	hybridIdentityVersion = 2

	// ML-DSA-65 key sizes (NIST Level 3, 192-bit security)
	mldsaPrivateKeySize = mldsa.MLDSA65PrivateKeySize // ~4032 bytes
	mldsaPublicKeySize  = mldsa.MLDSA65PublicKeySize  // ~1952 bytes
	mldsaSignatureSize  = mldsa.MLDSA65SignatureSize  // ~3309 bytes

)

var (
	// ErrHybridSignatureInvalid is returned when either signature component fails.
	ErrHybridSignatureInvalid = errors.New("hybrid signature verification failed")
	// ErrHybridDecapsulationFailed is returned when key decapsulation fails.
	ErrHybridDecapsulationFailed = errors.New("hybrid decapsulation failed")
	// ErrInvalidHybridIdentity is returned when hybrid identity data is malformed.
	ErrInvalidHybridIdentity = errors.New("invalid hybrid identity")
)


// HybridIdentity represents a post-quantum hybrid RNS identity.
// It combines classical (Ed25519/X25519) and post-quantum (ML-DSA-65/ML-KEM-768)
// algorithms for TLS 1.3-like hybrid security.
type HybridIdentity struct {
	// Classical keys
	edSeed       [ed25519SeedSize]byte
	edPrivateKey ed25519.PrivateKey
	edPublicKey  ed25519.PublicKey
	xPrivateKey  [x25519KeySize]byte
	xPublicKey   [x25519KeySize]byte

	// Post-quantum signing keys (ML-DSA-65)
	mldsaPrivateKey *mldsa.PrivateKey
	mldsaPublicKey  *mldsa.PublicKey

	// Hybrid KEM (X25519 + ML-KEM-768) - uses kem package
	hybridKEM        kem.KEM
	hybridKEMPrivate kem.PrivateKey
	hybridKEMPublic  kem.PublicKey

	// Cached destination hash
	destination [endpoints.RNSDestinationLen]byte
}

// HybridPublicIdentity represents the public portion of a hybrid identity.
// Used for verifying signatures and encapsulating secrets to remote peers.
type HybridPublicIdentity struct {
	// Classical public keys
	edPublicKey [ed25519PublicKeySize]byte
	xPublicKey  [x25519KeySize]byte

	// Post-quantum public keys
	mldsaPublicKey  *mldsa.PublicKey
	hybridKEMPublic kem.PublicKey

	// Cached destination hash
	destination [endpoints.RNSDestinationLen]byte
}

// NewHybridIdentity generates a new random hybrid identity.
func NewHybridIdentity() (*HybridIdentity, error) {
	// Generate Ed25519 seed
	seed := make([]byte, ed25519SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("failed to generate seed: %w", err)
	}

	// Generate ML-DSA-65 keys
	mldsaKey, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ML-DSA key: %w", err)
	}

	// Generate hybrid KEM keys (X25519 + ML-KEM-768)
	hybridKEM, err := kem.NewHybrid()
	if err != nil {
		return nil, fmt.Errorf("failed to create hybrid KEM instance: %w", err)
	}
	hybridPub, hybridPriv, err := hybridKEM.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate hybrid KEM keys: %w", err)
	}

	return newHybridIdentityFromComponents(seed, mldsaKey, hybridKEM, hybridPriv, hybridPub)
}

// newHybridIdentityFromComponents creates a hybrid identity from raw components.
func newHybridIdentityFromComponents(
	seed []byte,
	mldsaKey *mldsa.PrivateKey,
	hybridKEM kem.KEM,
	hybridPriv kem.PrivateKey,
	hybridPub kem.PublicKey,
) (*HybridIdentity, error) {
	if len(seed) != ed25519SeedSize {
		return nil, fmt.Errorf("%w: seed must be %d bytes", ErrInvalidHybridIdentity, ed25519SeedSize)
	}

	id := &HybridIdentity{
		mldsaPrivateKey:  mldsaKey,
		mldsaPublicKey:   mldsaKey.PublicKey,
		hybridKEM:        hybridKEM,
		hybridKEMPrivate: hybridPriv,
		hybridKEMPublic:  hybridPub,
	}

	// Copy seed
	copy(id.edSeed[:], seed)

	// Derive Ed25519 keys
	id.edPrivateKey = ed25519.NewKeyFromSeed(seed)
	id.edPublicKey = id.edPrivateKey.Public().(ed25519.PublicKey)

	// Derive X25519 keys from Ed25519 seed
	xSeedHash := sha256.Sum256(append([]byte("x25519-derive:"), seed...))
	copy(id.xPrivateKey[:], xSeedHash[:])

	// Clamp X25519 private key per RFC 7748
	id.xPrivateKey[0] &= 248
	id.xPrivateKey[31] &= 127
	id.xPrivateKey[31] |= 64

	// Compute X25519 public key
	curve25519.ScalarBaseMult(&id.xPublicKey, &id.xPrivateKey)

	// Compute hybrid destination
	id.computeDestination()

	return id, nil
}

// computeDestination calculates the 128-bit destination hash.
// Uses SHA-256(Ed25519_pubkey || ML-DSA_pubkey) truncated to 128 bits.
// This ensures destination changes if either classical or PQ key is compromised.
func (id *HybridIdentity) computeDestination() {
	h := sha256.New()
	h.Write(id.edPublicKey)
	h.Write(id.mldsaPublicKey.Bytes())
	digest := h.Sum(nil)
	copy(id.destination[:], digest[:endpoints.RNSDestinationLen])
}

// IsHybrid returns true, indicating this is a hybrid identity.
func (id *HybridIdentity) IsHybrid() bool {
	return true
}

// Destination returns the 128-bit destination hash.
func (id *HybridIdentity) Destination() [endpoints.RNSDestinationLen]byte {
	return id.destination
}

// Hash returns the identity hash (alias for Destination).
func (id *HybridIdentity) Hash() [endpoints.RNSDestinationLen]byte {
	return id.destination
}

// Sign creates a hybrid signature (Ed25519 || ML-DSA-65).
// Both signatures must verify for the hybrid signature to be valid.
func (id *HybridIdentity) Sign(message []byte) ([]byte, error) {
	// Classical Ed25519 signature
	edSig := ed25519.Sign(id.edPrivateKey, message)

	// Post-quantum ML-DSA-65 signature
	mldsaSig, err := id.mldsaPrivateKey.Sign(rand.Reader, message, nil)
	if err != nil {
		return nil, fmt.Errorf("ML-DSA signing failed: %w", err)
	}

	// Concatenate: Ed25519 (64 bytes) || ML-DSA (variable, ~3309 bytes)
	sig := make([]byte, 0, len(edSig)+len(mldsaSig))
	sig = append(sig, edSig...)
	sig = append(sig, mldsaSig...)

	return sig, nil
}

// SignEd25519 signs a message with Ed25519 only (for backward compatibility).
func (id *HybridIdentity) SignEd25519(message []byte) []byte {
	return ed25519.Sign(id.edPrivateKey, message)
}

// SignMLDSA signs a message with ML-DSA-65 only.
func (id *HybridIdentity) SignMLDSA(message []byte) ([]byte, error) {
	return id.mldsaPrivateKey.Sign(rand.Reader, message, nil)
}

// Verify checks a hybrid signature using AND logic.
// Both the Ed25519 and ML-DSA-65 signatures must verify.
func (id *HybridIdentity) Verify(message, signature []byte) bool {
	if len(signature) < ed25519SignatureSize+mldsaSignatureSize {
		return false
	}

	// Split signature components
	edSig := signature[:ed25519SignatureSize]
	mldsaSig := signature[ed25519SignatureSize:]

	// Verify Ed25519
	if !ed25519.Verify(id.edPublicKey, message, edSig) {
		return false
	}

	// Verify ML-DSA-65
	if !id.mldsaPublicKey.VerifySignature(message, mldsaSig) {
		return false
	}

	return true
}

// HybridEncapsulate performs hybrid key encapsulation using X25519 + ML-KEM-768.
// Returns (ciphertext, sharedSecret) where sharedSecret is derived via HKDF
// from both classical and post-quantum secrets.
func (id *HybridIdentity) HybridEncapsulate(recipientPub *HybridPublicIdentity) ([]byte, []byte, error) {
	// Use the kem package's hybrid encapsulation
	hybridKEM, err := kem.NewHybrid()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create hybrid KEM: %w", err)
	}

	ciphertext, sharedSecret, err := hybridKEM.Encapsulate(recipientPub.hybridKEMPublic)
	if err != nil {
		return nil, nil, fmt.Errorf("hybrid encapsulation failed: %w", err)
	}

	return ciphertext, sharedSecret, nil
}

// HybridDecapsulate recovers the shared secret from hybrid ciphertext.
func (id *HybridIdentity) HybridDecapsulate(ciphertext []byte) ([]byte, error) {
	// Use the kem package's hybrid decapsulation
	sharedSecret, err := id.hybridKEM.Decapsulate(id.hybridKEMPrivate, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHybridDecapsulationFailed, err)
	}

	return sharedSecret, nil
}

// ToClassicalIdentity extracts the classical (Ed25519/X25519) portion
// for backward compatibility with legacy peers.
func (id *HybridIdentity) ToClassicalIdentity() (*RNSIdentity, error) {
	return newRNSIdentityFromSeed(id.edSeed[:])
}

// PublicIdentity returns the public portion of this identity.
func (id *HybridIdentity) PublicIdentity() (*HybridPublicIdentity, error) {
	pub := &HybridPublicIdentity{
		mldsaPublicKey:  id.mldsaPublicKey,
		hybridKEMPublic: id.hybridKEMPublic,
	}
	copy(pub.edPublicKey[:], id.edPublicKey)
	copy(pub.xPublicKey[:], id.xPublicKey[:])
	copy(pub.destination[:], id.destination[:])
	return pub, nil
}

// SigningPublicKey returns the Ed25519 public key.
func (id *HybridIdentity) SigningPublicKey() []byte {
	return id.edPublicKey
}

// X25519PublicKey returns the X25519 public key.
func (id *HybridIdentity) X25519PublicKey() [x25519KeySize]byte {
	return id.xPublicKey
}

// MLDSAPublicKey returns the ML-DSA-65 public key.
func (id *HybridIdentity) MLDSAPublicKey() []byte {
	return id.mldsaPublicKey.Bytes()
}

// HybridKEMPublicKey returns the hybrid KEM (X25519 + ML-KEM-768) public key.
func (id *HybridIdentity) HybridKEMPublicKey() []byte {
	return id.hybridKEMPublic.Bytes()
}

// Save persists the hybrid identity to a file.
// Format: Magic(4) || Version(4) || Ed25519Seed(32) || MLDSAPriv(~4032) || HybridKEMPriv(X25519+MLKEM)
func (id *HybridIdentity) Save(path string) error {
	mldsaBytes := id.mldsaPrivateKey.Bytes()
	hybridKEMBytes := id.hybridKEMPrivate.Bytes()

	// Calculate total size
	totalSize := 4 + 4 + ed25519SeedSize + len(mldsaBytes) + len(hybridKEMBytes)
	data := make([]byte, totalSize)

	offset := 0

	// Magic
	binary.BigEndian.PutUint32(data[offset:], hybridIdentityMagic)
	offset += 4

	// Version
	binary.BigEndian.PutUint32(data[offset:], hybridIdentityVersion)
	offset += 4

	// Ed25519 seed
	copy(data[offset:], id.edSeed[:])
	offset += ed25519SeedSize

	// ML-DSA private key
	copy(data[offset:], mldsaBytes)
	offset += len(mldsaBytes)

	// Hybrid KEM private key
	copy(data[offset:], hybridKEMBytes)

	return os.WriteFile(path, data, 0600)
}

// LoadHybridIdentity loads a hybrid identity from a file.
// Note: The hybrid KEM keys are regenerated since the kem package doesn't expose
// PrivateKeyFromBytes. This means loaded identities get new KEM keys. For
// production use, consider adding key deserialization to the kem package.
func LoadHybridIdentity(path string) (*HybridIdentity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read identity file: %w", err)
	}

	// Minimum size check: magic + version + seed
	if len(data) < 8+ed25519SeedSize {
		return nil, fmt.Errorf("%w: file too small", ErrInvalidHybridIdentity)
	}

	// Check magic
	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != hybridIdentityMagic {
		return nil, fmt.Errorf("%w: invalid magic number", ErrInvalidHybridIdentity)
	}

	// Check version
	version := binary.BigEndian.Uint32(data[4:8])
	if version != hybridIdentityVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidHybridIdentity, version)
	}

	offset := 8

	// Extract Ed25519 seed
	seed := data[offset : offset+ed25519SeedSize]
	offset += ed25519SeedSize

	// Extract ML-DSA private key
	mldsaPrivBytes := data[offset : offset+mldsaPrivateKeySize]
	// Remaining bytes are hybrid KEM private key (not used due to API limitation)

	// Reconstruct ML-DSA key
	mldsaKey, err := mldsa.PrivateKeyFromBytes(mldsa.MLDSA65, mldsaPrivBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ML-DSA key: %w", err)
	}

	// Regenerate hybrid KEM keys (API limitation - no deserialization support)
	hybridKEM, err := kem.NewHybrid()
	if err != nil {
		return nil, fmt.Errorf("failed to create hybrid KEM instance: %w", err)
	}
	hybridPub, hybridPriv, err := hybridKEM.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate hybrid KEM keys: %w", err)
	}

	return newHybridIdentityFromComponents(seed, mldsaKey, hybridKEM, hybridPriv, hybridPub)
}

// LoadOrGenerateHybridIdentity loads or generates a hybrid identity.
func LoadOrGenerateHybridIdentity(path string) (*HybridIdentity, error) {
	if path == "" {
		return NewHybridIdentity()
	}

	id, err := LoadHybridIdentity(path)
	if err == nil {
		return id, nil
	}

	if os.IsNotExist(err) || errors.Is(err, ErrInvalidHybridIdentity) {
		id, genErr := NewHybridIdentity()
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

// Close clears sensitive key material from memory.
func (id *HybridIdentity) Close() error {
	// Zero Ed25519 seed and private key
	for i := range id.edSeed {
		id.edSeed[i] = 0
	}
	for i := range id.edPrivateKey {
		id.edPrivateKey[i] = 0
	}
	// Zero X25519 private key
	for i := range id.xPrivateKey {
		id.xPrivateKey[i] = 0
	}
	// Note: ML-DSA and ML-KEM keys should also be zeroed in production
	return nil
}

// --- HybridPublicIdentity methods ---

// NewHybridPublicIdentity creates a public identity from raw public keys.
func NewHybridPublicIdentity(
	edPubKey, xPubKey []byte,
	mldsaPubKey *mldsa.PublicKey,
	hybridKEMPubKey kem.PublicKey,
) (*HybridPublicIdentity, error) {
	if len(edPubKey) != ed25519PublicKeySize {
		return nil, fmt.Errorf("%w: Ed25519 public key must be %d bytes", ErrInvalidHybridIdentity, ed25519PublicKeySize)
	}
	if len(xPubKey) != x25519KeySize {
		return nil, fmt.Errorf("%w: X25519 public key must be %d bytes", ErrInvalidHybridIdentity, x25519KeySize)
	}

	pub := &HybridPublicIdentity{
		mldsaPublicKey:  mldsaPubKey,
		hybridKEMPublic: hybridKEMPubKey,
	}
	copy(pub.edPublicKey[:], edPubKey)
	copy(pub.xPublicKey[:], xPubKey)

	// Compute destination
	pub.computeDestination()

	return pub, nil
}

// computeDestination calculates the destination for a public identity.
func (pub *HybridPublicIdentity) computeDestination() {
	h := sha256.New()
	h.Write(pub.edPublicKey[:])
	h.Write(pub.mldsaPublicKey.Bytes())
	digest := h.Sum(nil)
	copy(pub.destination[:], digest[:endpoints.RNSDestinationLen])
}

// Destination returns the 128-bit destination hash.
func (pub *HybridPublicIdentity) Destination() [endpoints.RNSDestinationLen]byte {
	return pub.destination
}

// Verify checks a hybrid signature using AND logic.
func (pub *HybridPublicIdentity) Verify(message, signature []byte) bool {
	if len(signature) < ed25519SignatureSize+mldsaSignatureSize {
		return false
	}

	// Split signature components
	edSig := signature[:ed25519SignatureSize]
	mldsaSig := signature[ed25519SignatureSize:]

	// Verify Ed25519
	if !ed25519.Verify(pub.edPublicKey[:], message, edSig) {
		return false
	}

	// Verify ML-DSA-65
	if !pub.mldsaPublicKey.VerifySignature(message, mldsaSig) {
		return false
	}

	return true
}

// SigningPublicKey returns the Ed25519 public key.
func (pub *HybridPublicIdentity) SigningPublicKey() []byte {
	return pub.edPublicKey[:]
}

// X25519PublicKey returns the X25519 public key.
func (pub *HybridPublicIdentity) X25519PublicKey() [x25519KeySize]byte {
	return pub.xPublicKey
}

// MLDSAPublicKey returns the ML-DSA-65 public key.
func (pub *HybridPublicIdentity) MLDSAPublicKey() []byte {
	return pub.mldsaPublicKey.Bytes()
}

// HybridKEMPublicKey returns the hybrid KEM (X25519 + ML-KEM-768) public key.
func (pub *HybridPublicIdentity) HybridKEMPublicKey() []byte {
	return pub.hybridKEMPublic.Bytes()
}

// MarshalBinary serializes the hybrid public identity.
// Format: Ed25519Pub(32) || X25519Pub(32) || MLDSAPub(~1952) || HybridKEMPub(X25519+MLKEM)
func (pub *HybridPublicIdentity) MarshalBinary() ([]byte, error) {
	mldsaBytes := pub.mldsaPublicKey.Bytes()
	hybridKEMBytes := pub.hybridKEMPublic.Bytes()

	totalSize := ed25519PublicKeySize + x25519KeySize + len(mldsaBytes) + len(hybridKEMBytes)
	data := make([]byte, 0, totalSize)

	data = append(data, pub.edPublicKey[:]...)
	data = append(data, pub.xPublicKey[:]...)
	data = append(data, mldsaBytes...)
	data = append(data, hybridKEMBytes...)

	return data, nil
}

// UnmarshalHybridPublicIdentity deserializes a hybrid public identity.
func UnmarshalHybridPublicIdentity(data []byte) (*HybridPublicIdentity, error) {
	// Minimum size: Ed25519(32) + X25519(32) + ML-DSA(~1952) + HybridKEM(32+1184)
	minSize := ed25519PublicKeySize + x25519KeySize + mldsaPublicKeySize
	if len(data) < minSize {
		return nil, fmt.Errorf("%w: data too short", ErrInvalidHybridIdentity)
	}

	offset := 0

	// Ed25519 public key
	edPubKey := data[offset : offset+ed25519PublicKeySize]
	offset += ed25519PublicKeySize

	// X25519 public key
	xPubKey := data[offset : offset+x25519KeySize]
	offset += x25519KeySize

	// ML-DSA public key
	mldsaPubBytes := data[offset : offset+mldsaPublicKeySize]
	offset += mldsaPublicKeySize

	// Hybrid KEM public key (remaining bytes)
	hybridKEMPubBytes := data[offset:]

	// Parse ML-DSA public key
	mldsaPubKey, err := mldsa.PublicKeyFromBytes(mldsaPubBytes, mldsa.MLDSA65)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ML-DSA public key: %w", err)
	}

	// Wrap hybrid KEM public key bytes
	hybridKEMPubKey := &hybridKEMPublicKeyWrapper{data: hybridKEMPubBytes}

	return NewHybridPublicIdentity(edPubKey, xPubKey, mldsaPubKey, hybridKEMPubKey)
}

// hybridKEMPublicKeyWrapper wraps KEM public key bytes to satisfy kem.PublicKey interface.
// This is used for both ML-KEM and hybrid KEM public keys when deserializing.
type hybridKEMPublicKeyWrapper struct {
	data []byte
}

func (w *hybridKEMPublicKeyWrapper) Bytes() []byte {
	return w.data
}

func (w *hybridKEMPublicKeyWrapper) Equal(other kem.PublicKey) bool {
	if other == nil {
		return false
	}
	otherBytes := other.Bytes()
	if len(w.data) != len(otherBytes) {
		return false
	}
	for i := range w.data {
		if w.data[i] != otherBytes[i] {
			return false
		}
	}
	return true
}

// Compile-time interface satisfaction checks
var (
	_ io.Closer = (*HybridIdentity)(nil)
)
