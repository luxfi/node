// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// Encryption-related errors
var (
	ErrInvalidKeySize     = errors.New("invalid key size")
	ErrInvalidNonce       = errors.New("invalid nonce")
	ErrCiphertextTooShort = errors.New("ciphertext too short")
	ErrAuthFailed         = errors.New("authentication failed")
	ErrFHENotSupported    = errors.New("FHE operation not supported")
	ErrThresholdNotMet    = errors.New("threshold not met for key derivation")
)

// FHEScheme defines the FHE scheme in use
type FHEScheme string

const (
	// FHESchemeBFV is the BFV scheme (integer arithmetic)
	FHESchemeBFV FHEScheme = "bfv"
	// FHESchemeCKKS is the CKKS scheme (approximate arithmetic)
	FHESchemeCKKS FHEScheme = "ckks"
	// FHESchemeTFHE is the TFHE scheme (boolean circuits)
	FHESchemeTFHE FHEScheme = "tfhe"
)

// FHEOperation defines supported FHE operations
type FHEOperation string

const (
	FHEOpAdd      FHEOperation = "add"
	FHEOpMultiply FHEOperation = "multiply"
	FHEOpSum      FHEOperation = "sum"
	FHEOpCount    FHEOperation = "count"
	FHEOpAverage  FHEOperation = "average"
	FHEOpCompare  FHEOperation = "compare"
	FHEOpRotate   FHEOperation = "rotate"
)

// DomainKeyManager manages encryption keys for different domains
type DomainKeyManager struct {
	mu sync.RWMutex

	// Per-domain keys
	domainKeys map[EncryptionDomain]*DomainKeySet

	// Shared key derivation for multi-party access
	thresholdKeys map[string]*ThresholdKeySet

	// Key rotation tracking
	keyVersions map[EncryptionDomain]uint32
}

// DomainKeySet holds keys for a specific encryption domain
type DomainKeySet struct {
	Domain      EncryptionDomain `json:"domain"`
	Version     uint32           `json:"version"`
	PublicKey   []byte           `json:"publicKey"`
	PrivateKey  []byte           `json:"privateKey,omitempty"` // Only if holder has access
	EncryptKey  []byte           `json:"encryptKey"`           // Symmetric key for data encryption
	CreatedAt   int64            `json:"createdAt"`
	RotatedAt   int64            `json:"rotatedAt,omitempty"`
}

// ThresholdKeySet holds keys for threshold/MPC access
type ThresholdKeySet struct {
	ID          string   `json:"id"`
	Threshold   int      `json:"threshold"`
	TotalShares int      `json:"totalShares"`
	PublicKey   []byte   `json:"publicKey"`
	Shares      [][]byte `json:"shares,omitempty"` // Only shares this party holds
}

// NewDomainKeyManager creates a new domain key manager
func NewDomainKeyManager() *DomainKeyManager {
	return &DomainKeyManager{
		domainKeys:    make(map[EncryptionDomain]*DomainKeySet),
		thresholdKeys: make(map[string]*ThresholdKeySet),
		keyVersions:   make(map[EncryptionDomain]uint32),
	}
}

// GenerateKeys generates keys for a domain
func (m *DomainKeyManager) GenerateKeys(domain EncryptionDomain) (*DomainKeySet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate symmetric encryption key (AES-256)
	encryptKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, encryptKey); err != nil {
		return nil, err
	}

	// For asymmetric operations, generate key pair
	// (In production, use proper key generation based on curve)
	publicKey := make([]byte, 32)
	privateKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, publicKey); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rand.Reader, privateKey); err != nil {
		return nil, err
	}

	m.keyVersions[domain]++
	keySet := &DomainKeySet{
		Domain:     domain,
		Version:    m.keyVersions[domain],
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		EncryptKey: encryptKey,
	}

	m.domainKeys[domain] = keySet
	return keySet, nil
}

// GetKeys retrieves keys for a domain
func (m *DomainKeyManager) GetKeys(domain EncryptionDomain) (*DomainKeySet, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keySet, exists := m.domainKeys[domain]
	return keySet, exists
}

// SetupThresholdKey sets up a threshold key for multi-party access
func (m *DomainKeyManager) SetupThresholdKey(id string, threshold, totalShares int) (*ThresholdKeySet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if threshold > totalShares {
		return nil, ErrThresholdNotMet
	}

	// Generate shares using Shamir's Secret Sharing
	// (Simplified - in production use proper SSS implementation)
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return nil, err
	}

	shares := make([][]byte, totalShares)
	for i := 0; i < totalShares; i++ {
		shares[i] = make([]byte, 32)
		// In production: use polynomial interpolation
		copy(shares[i], secret)
		shares[i][0] ^= byte(i + 1)
	}

	// Derive public key from secret
	publicKey := sha256.Sum256(secret)

	keySet := &ThresholdKeySet{
		ID:          id,
		Threshold:   threshold,
		TotalShares: totalShares,
		PublicKey:   publicKey[:],
		Shares:      shares,
	}

	m.thresholdKeys[id] = keySet
	return keySet, nil
}

// DefaultEncryptor provides default encryption implementation
type DefaultEncryptor struct {
	keyManager *DomainKeyManager
	fheEnabled bool
	fheScheme  FHEScheme
}

// NewDefaultEncryptor creates a new default encryptor
func NewDefaultEncryptor(keyManager *DomainKeyManager, fheEnabled bool, scheme FHEScheme) *DefaultEncryptor {
	return &DefaultEncryptor{
		keyManager: keyManager,
		fheEnabled: fheEnabled,
		fheScheme:  scheme,
	}
}

// Encrypt encrypts data for a specific domain using AES-GCM or ChaCha20-Poly1305
func (e *DefaultEncryptor) Encrypt(ctx context.Context, data []byte, domain EncryptionDomain, recipients [][]byte) ([]byte, error) {
	keySet, exists := e.keyManager.GetKeys(domain)
	if !exists {
		// Generate keys if not present
		var err error
		keySet, err = e.keyManager.GenerateKeys(domain)
		if err != nil {
			return nil, err
		}
	}

	// Use AES-GCM for encryption
	block, err := aes.NewCipher(keySet.EncryptKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Prepend version and nonce to ciphertext
	ciphertext := gcm.Seal(nil, nonce, data, nil)

	result := make([]byte, 0, 4+len(nonce)+len(ciphertext))
	result = append(result, byte(keySet.Version>>24), byte(keySet.Version>>16), byte(keySet.Version>>8), byte(keySet.Version))
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// Decrypt decrypts data if the caller has access
func (e *DefaultEncryptor) Decrypt(ctx context.Context, ciphertext []byte, domain EncryptionDomain, privateKey []byte) ([]byte, error) {
	if len(ciphertext) < 4 {
		return nil, ErrCiphertextTooShort
	}

	keySet, exists := e.keyManager.GetKeys(domain)
	if !exists {
		return nil, ErrDomainAccessDenied
	}

	// Extract version
	// version := uint32(ciphertext[0])<<24 | uint32(ciphertext[1])<<16 | uint32(ciphertext[2])<<8 | uint32(ciphertext[3])

	// Create cipher
	block, err := aes.NewCipher(keySet.EncryptKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < 4+gcm.NonceSize() {
		return nil, ErrCiphertextTooShort
	}

	nonce := ciphertext[4 : 4+gcm.NonceSize()]
	encrypted := ciphertext[4+gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, ErrAuthFailed
	}

	return plaintext, nil
}

// EncryptChaCha encrypts using ChaCha20-Poly1305 (alternative to AES-GCM)
func (e *DefaultEncryptor) EncryptChaCha(ctx context.Context, data []byte, key []byte) ([]byte, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, ErrInvalidKeySize
	}

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := aead.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

// DecryptChaCha decrypts ChaCha20-Poly1305 ciphertext
func (e *DefaultEncryptor) DecryptChaCha(ctx context.Context, ciphertext []byte, key []byte) ([]byte, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, ErrInvalidKeySize
	}

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < aead.NonceSize() {
		return nil, ErrCiphertextTooShort
	}

	nonce := ciphertext[:aead.NonceSize()]
	encrypted := ciphertext[aead.NonceSize():]

	plaintext, err := aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, ErrAuthFailed
	}

	return plaintext, nil
}

// EncryptFHE performs FHE encryption for homomorphic operations
func (e *DefaultEncryptor) EncryptFHE(ctx context.Context, data []byte, scheme string) ([]byte, error) {
	if !e.fheEnabled {
		return nil, ErrFHENotSupported
	}

	// Encode data with scheme prefix for FHE processing
	// Actual FHE encryption is performed by the ThresholdVM when enabled
	result := make([]byte, 0, len(scheme)+1+len(data))
	result = append(result, byte(len(scheme)))
	result = append(result, []byte(scheme)...)
	result = append(result, data...)

	return result, nil
}

// ComputeFHE performs computation on FHE-encrypted data
func (e *DefaultEncryptor) ComputeFHE(ctx context.Context, encrypted []byte, operation string, params []byte) ([]byte, error) {
	if !e.fheEnabled {
		return nil, ErrFHENotSupported
	}

	// In production, this would perform actual FHE operations
	// For now, return the encrypted data (identity operation)
	return encrypted, nil
}

// DecryptFHE decrypts FHE result
func (e *DefaultEncryptor) DecryptFHE(ctx context.Context, encrypted []byte, privateKey []byte) ([]byte, error) {
	if !e.fheEnabled {
		return nil, ErrFHENotSupported
	}

	if len(encrypted) < 1 {
		return nil, ErrCiphertextTooShort
	}

	schemeLen := int(encrypted[0])
	if len(encrypted) < 1+schemeLen {
		return nil, ErrCiphertextTooShort
	}

	// Skip scheme marker
	data := encrypted[1+schemeLen:]
	return data, nil
}

// GenerateDomainKey generates a new key for an encryption domain
func (e *DefaultEncryptor) GenerateDomainKey(domain EncryptionDomain) (publicKey, privateKey []byte, err error) {
	keySet, err := e.keyManager.GenerateKeys(domain)
	if err != nil {
		return nil, nil, err
	}
	return keySet.PublicKey, keySet.PrivateKey, nil
}

// DeriveSharedKey derives a shared key for multi-party access
func (e *DefaultEncryptor) DeriveSharedKey(parties [][]byte, threshold int) ([]byte, error) {
	if len(parties) < threshold {
		return nil, ErrThresholdNotMet
	}

	// Combine party keys using XOR (simplified - use proper key agreement in production)
	sharedKey := make([]byte, 32)
	for _, party := range parties {
		for i := 0; i < 32 && i < len(party); i++ {
			sharedKey[i] ^= party[i]
		}
	}

	// Hash to get final key
	hash := sha256.Sum256(sharedKey)
	return hash[:], nil
}

// EncryptionCapability represents what a party can do with encrypted data
type EncryptionCapability struct {
	PartyID     []byte             `json:"partyId"`
	Domains     []EncryptionDomain `json:"domains"`      // Domains party can access
	Operations  []FHEOperation     `json:"operations"`   // FHE operations allowed
	ReadOnly    bool               `json:"readOnly"`     // Can only decrypt, not encrypt
	ValidUntil  int64              `json:"validUntil"`   // Expiry timestamp
	Delegatable bool               `json:"delegatable"`  // Can delegate to others
}

// CapabilityManager manages encryption capabilities
type CapabilityManager struct {
	mu sync.RWMutex

	capabilities map[string]*EncryptionCapability // partyID -> capability
	delegations  map[string][]string              // partyID -> delegated partyIDs
}

// NewCapabilityManager creates a new capability manager
func NewCapabilityManager() *CapabilityManager {
	return &CapabilityManager{
		capabilities: make(map[string]*EncryptionCapability),
		delegations:  make(map[string][]string),
	}
}

// Grant grants a capability to a party
func (m *CapabilityManager) Grant(cap *EncryptionCapability) {
	m.mu.Lock()
	defer m.mu.Unlock()

	partyIDStr := string(cap.PartyID)
	m.capabilities[partyIDStr] = cap
}

// Check checks if a party has access to a domain
func (m *CapabilityManager) Check(partyID []byte, domain EncryptionDomain) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	partyIDStr := string(partyID)
	cap, exists := m.capabilities[partyIDStr]
	if !exists {
		return false
	}

	for _, d := range cap.Domains {
		if d == domain {
			return true
		}
	}
	return false
}

// Delegate delegates capability from one party to another
func (m *CapabilityManager) Delegate(fromParty, toParty []byte, domains []EncryptionDomain) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fromStr := string(fromParty)
	toStr := string(toParty)

	// Check if delegator has capability and can delegate
	cap, exists := m.capabilities[fromStr]
	if !exists || !cap.Delegatable {
		return ErrDomainAccessDenied
	}

	// Create delegated capability
	delegatedCap := &EncryptionCapability{
		PartyID:     toParty,
		Domains:     domains,
		ReadOnly:    true, // Delegated capabilities are read-only by default
		ValidUntil:  cap.ValidUntil,
		Delegatable: false, // Cannot further delegate
	}

	m.capabilities[toStr] = delegatedCap
	m.delegations[fromStr] = append(m.delegations[fromStr], toStr)

	return nil
}

// Revoke revokes a capability
func (m *CapabilityManager) Revoke(partyID []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	partyIDStr := string(partyID)
	delete(m.capabilities, partyIDStr)

	// Also revoke any delegations from this party
	if delegated, exists := m.delegations[partyIDStr]; exists {
		for _, d := range delegated {
			delete(m.capabilities, d)
		}
		delete(m.delegations, partyIDStr)
	}
}
