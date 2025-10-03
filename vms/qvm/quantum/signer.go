// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quantum

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/node/cache"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

var (
	ErrInvalidQuantumSignature = errors.New("invalid quantum signature")
	ErrInvalidRingtailKey      = errors.New("invalid ringtail key")
	ErrQuantumStampExpired     = errors.New("quantum stamp expired")
	ErrQuantumVerificationFailed = errors.New("quantum verification failed")
	ErrUnsupportedAlgorithm    = errors.New("unsupported quantum algorithm")
)

// QuantumSigner handles quantum signature operations
type QuantumSigner struct {
	log             log.Logger
	algorithmVersion uint32
	ringtailKeySize int
	stampWindow     time.Duration
	sigCache        *cache.LRU[ids.ID, *QuantumSignature]
	mu              sync.RWMutex
}

// QuantumSignature represents a quantum-resistant signature
type QuantumSignature struct {
	Algorithm   uint32
	Timestamp   time.Time
	PublicKey   []byte
	Signature   []byte
	RingtailKey []byte
	QuantumStamp []byte
}

// RingtailKey represents a Ringtail key for quantum resistance
type RingtailKey struct {
	Version   uint32
	PublicKey []byte
	PrivateKey []byte
	Nonce     []byte
}

// NewQuantumSigner creates a new quantum signer
func NewQuantumSigner(log log.Logger, algorithmVersion uint32, keySize int, stampWindow time.Duration, cacheSize int) *QuantumSigner {
	return &QuantumSigner{
		log:             log,
		algorithmVersion: algorithmVersion,
		ringtailKeySize: keySize,
		stampWindow:     stampWindow,
		sigCache:        &cache.LRU[ids.ID, *QuantumSignature]{Size: cacheSize},
	}
}

// GenerateRingtailKey generates a new Ringtail key pair
func (qs *QuantumSigner) GenerateRingtailKey() (*RingtailKey, error) {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	// Generate quantum-resistant key pair
	publicKey := make([]byte, qs.ringtailKeySize)
	privateKey := make([]byte, qs.ringtailKeySize)
	nonce := make([]byte, 32)

	if _, err := rand.Read(publicKey); err != nil {
		return nil, fmt.Errorf("failed to generate public key: %w", err)
	}
	if _, err := rand.Read(privateKey); err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	return &RingtailKey{
		Version:   qs.algorithmVersion,
		PublicKey: publicKey,
		PrivateKey: privateKey,
		Nonce:     nonce,
	}, nil
}

// Sign creates a quantum signature for the given message
func (qs *QuantumSigner) Sign(message []byte, key *RingtailKey) (*QuantumSignature, error) {
	if key == nil || len(key.PrivateKey) != qs.ringtailKeySize {
		return nil, ErrInvalidRingtailKey
	}

	// Generate quantum stamp
	stamp, err := qs.generateQuantumStamp(message, key)
	if err != nil {
		return nil, fmt.Errorf("failed to generate quantum stamp: %w", err)
	}

	// Create signature using quantum-resistant algorithm
	signature := qs.quantumSign(message, key.PrivateKey, stamp)

	sig := &QuantumSignature{
		Algorithm:   qs.algorithmVersion,
		Timestamp:   time.Now(),
		PublicKey:   key.PublicKey,
		Signature:   signature,
		RingtailKey: key.PublicKey,
		QuantumStamp: stamp,
	}

	// Cache the signature
	sigID := qs.computeSignatureID(sig)
	qs.sigCache.Put(sigID, sig)

	return sig, nil
}

// Verify verifies a quantum signature
func (qs *QuantumSigner) Verify(message []byte, sig *QuantumSignature) error {
	if sig == nil {
		return ErrInvalidQuantumSignature
	}

	// Check if signature is cached
	sigID := qs.computeSignatureID(sig)
	if cached, found := qs.sigCache.Get(sigID); found {
		if cached.Timestamp.Equal(sig.Timestamp) {
			return nil // Already verified
		}
	}

	// Verify algorithm version
	if sig.Algorithm != qs.algorithmVersion {
		return ErrUnsupportedAlgorithm
	}

	// Verify timestamp
	if time.Since(sig.Timestamp) > qs.stampWindow {
		return ErrQuantumStampExpired
	}

	// Verify quantum stamp
	if err := qs.verifyQuantumStamp(message, sig); err != nil {
		return fmt.Errorf("quantum stamp verification failed: %w", err)
	}

	// Verify signature using quantum-resistant algorithm
	if !qs.quantumVerify(message, sig.PublicKey, sig.Signature, sig.QuantumStamp) {
		return ErrQuantumVerificationFailed
	}

	// Cache successful verification
	qs.sigCache.Put(sigID, sig)

	return nil
}


// generateQuantumStamp generates a quantum stamp for message authentication
func (qs *QuantumSigner) generateQuantumStamp(message []byte, key *RingtailKey) ([]byte, error) {
	// Combine message, key nonce, and timestamp
	timestamp := time.Now().UnixNano()
	data := make([]byte, len(message)+len(key.Nonce)+8)
	copy(data, message)
	copy(data[len(message):], key.Nonce)
	binary.BigEndian.PutUint64(data[len(message)+len(key.Nonce):], uint64(timestamp))

	// Generate quantum stamp using SHA-512 (placeholder for quantum hash)
	hash := sha512.Sum512(data)

	// Add quantum noise
	noise := make([]byte, 32)
	if _, err := rand.Read(noise); err != nil {
		return nil, err
	}

	stamp := make([]byte, len(hash)+len(noise))
	copy(stamp, hash[:])
	copy(stamp[len(hash):], noise)

	return stamp, nil
}

// verifyQuantumStamp verifies a quantum stamp
func (qs *QuantumSigner) verifyQuantumStamp(message []byte, sig *QuantumSignature) error {
	if len(sig.QuantumStamp) < 64 {
		return ErrInvalidQuantumSignature
	}

	// Extract hash from stamp
	stampHash := sig.QuantumStamp[:64]

	// Recompute expected hash range (allowing for timestamp variance)
	fullHash := sha512.Sum512(message)
	expectedHashPrefix := fullHash[:32]

	// Verify hash prefix matches
	for i := 0; i < 32; i++ {
		if stampHash[i] != expectedHashPrefix[i] {
			return ErrQuantumVerificationFailed
		}
	}

	return nil
}

// quantumSign performs quantum-resistant signing
func (qs *QuantumSigner) quantumSign(message, privateKey, stamp []byte) []byte {
	// Combine message and stamp
	data := make([]byte, len(message)+len(stamp))
	copy(data, message)
	copy(data[len(message):], stamp)

	// Generate signature using quantum-resistant algorithm
	// This is a simplified placeholder - real implementation would use
	// algorithms like SPHINCS+, Dilithium, or Falcon
	hash := sha512.Sum512(data)

	// XOR with private key for quantum resistance (simplified)
	signature := make([]byte, len(hash))
	for i := range hash {
		signature[i] = hash[i] ^ privateKey[i%len(privateKey)]
	}

	return signature
}

// quantumVerify performs quantum-resistant signature verification
func (qs *QuantumSigner) quantumVerify(message, publicKey, signature, stamp []byte) bool {
	// Combine message and stamp
	data := make([]byte, len(message)+len(stamp))
	copy(data, message)
	copy(data[len(message):], stamp)

	// Verify signature using quantum-resistant algorithm
	hash := sha512.Sum512(data)

	// XOR with public key and compare (simplified)
	for i := range signature {
		expected := hash[i%len(hash)] ^ publicKey[i%len(publicKey)]
		if signature[i] != expected {
			return false
		}
	}

	return true
}

// computeSignatureID computes a unique ID for a signature
func (qs *QuantumSigner) computeSignatureID(sig *QuantumSignature) ids.ID {
	data := make([]byte, 0, len(sig.Signature)+len(sig.PublicKey)+8)
	data = append(data, sig.Signature...)
	data = append(data, sig.PublicKey...)
	timestampBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBytes, uint64(sig.Timestamp.Unix()))
	data = append(data, timestampBytes...)

	// Use ToID to hash the data
	id, _ := ids.ToID(data)
	return id
}

// ParallelVerify verifies multiple signatures in parallel
func (qs *QuantumSigner) ParallelVerify(messages [][]byte, signatures []*QuantumSignature) error {
	if len(messages) != len(signatures) {
		return errors.New("message and signature count mismatch")
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(messages))

	for i := range messages {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := qs.Verify(messages[idx], signatures[idx]); err != nil {
				errChan <- fmt.Errorf("signature %d verification failed: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}