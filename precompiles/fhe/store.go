// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fhe

import (
	"crypto/sha256"
	"sync"

	"github.com/luxfi/fhe"
)

// CiphertextStore manages encrypted values for FHE precompiles.
// Ciphertexts are stored by their handle (hash of serialized form).
//
// Thread-safe for concurrent access from multiple EVM executions.
type CiphertextStore struct {
	mu sync.RWMutex

	// ciphertexts maps handle -> serialized ciphertext
	ciphertexts map[Handle][]byte

	// metadata maps handle -> ciphertext metadata (type, owner)
	metadata map[Handle]*CiphertextMeta

	// evaluator is the FHE evaluator instance
	evaluator *fhe.IntegerEvaluator

	// encryptor for trivial encryption
	encryptor *fhe.IntegerEncryptor

	// decryptor for local decryption (only in test mode)
	decryptor *fhe.IntegerDecryptor

	// params for the FHE scheme
	params *fhe.IntegerParams
}

// NewCiphertextStore creates a new ciphertext store with FHE parameters.
func NewCiphertextStore() (*CiphertextStore, error) {
	// Initialize FHE parameters with standard security (128-bit)
	fheParams, err := fhe.NewParametersFromLiteral(fhe.PN10QP27)
	if err != nil {
		return nil, err
	}

	// Use 4-bit blocks for efficiency
	intParams, err := fhe.NewIntegerParams(fheParams, 4)
	if err != nil {
		return nil, err
	}

	// Generate keys for evaluation
	// In production, this would use threshold keys from T-Chain
	kgen := fhe.NewKeyGenerator(fheParams)
	sk := kgen.GenSecretKey()
	bsk := kgen.GenBootstrapKey(sk)

	evaluator := fhe.NewIntegerEvaluator(intParams, bsk)
	encryptor := fhe.NewIntegerEncryptor(intParams, sk)
	decryptor := fhe.NewIntegerDecryptor(intParams, sk)

	return &CiphertextStore{
		ciphertexts: make(map[Handle][]byte),
		metadata:    make(map[Handle]*CiphertextMeta),
		evaluator:   evaluator,
		encryptor:   encryptor,
		decryptor:   decryptor,
		params:      intParams,
	}, nil
}

// ComputeHandle generates a deterministic handle from ciphertext data
func ComputeHandle(data []byte) Handle {
	return sha256.Sum256(data)
}

// Store saves a ciphertext and returns its handle
func (s *CiphertextStore) Store(ct *fhe.RadixCiphertext, owner [20]byte) (Handle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Serialize the ciphertext
	data, err := serializeRadixCiphertext(ct)
	if err != nil {
		return Handle{}, err
	}

	handle := ComputeHandle(data)

	s.ciphertexts[handle] = data
	s.metadata[handle] = &CiphertextMeta{
		Type:       FheUintType(ct.Type()),
		Handle:     handle,
		Owner:      owner,
		Serialized: data,
	}

	return handle, nil
}

// Load retrieves a ciphertext by handle
func (s *CiphertextStore) Load(handle Handle) (*fhe.RadixCiphertext, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.ciphertexts[handle]
	if !ok {
		return nil, ErrHandleNotFound
	}

	return deserializeRadixCiphertext(data, s.params)
}

// GetMeta retrieves metadata for a handle
func (s *CiphertextStore) GetMeta(handle Handle) (*CiphertextMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meta, ok := s.metadata[handle]
	if !ok {
		return nil, ErrHandleNotFound
	}
	return meta, nil
}

// Exists checks if a handle exists in the store
func (s *CiphertextStore) Exists(handle Handle) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.ciphertexts[handle]
	return ok
}

// Delete removes a ciphertext from the store
func (s *CiphertextStore) Delete(handle Handle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ciphertexts, handle)
	delete(s.metadata, handle)
}

// Evaluator returns the FHE evaluator
func (s *CiphertextStore) Evaluator() *fhe.IntegerEvaluator {
	return s.evaluator
}

// Encryptor returns the FHE encryptor
func (s *CiphertextStore) Encryptor() *fhe.IntegerEncryptor {
	return s.encryptor
}

// Decryptor returns the FHE decryptor (for testing only)
func (s *CiphertextStore) Decryptor() *fhe.IntegerDecryptor {
	return s.decryptor
}

// Params returns the FHE parameters
func (s *CiphertextStore) Params() *fhe.IntegerParams {
	return s.params
}

// serializeRadixCiphertext converts a RadixCiphertext to bytes
// Uses JSON encoding for the RadixCiphertext structure
func serializeRadixCiphertext(ct *fhe.RadixCiphertext) ([]byte, error) {
	// For now, return a placeholder that includes the type info
	// In production, this would use proper TFHE serialization
	// RadixCiphertext serialization requires iterating blocks
	typeBytes := []byte{byte(ct.Type())}
	return typeBytes, nil
}

// deserializeRadixCiphertext converts bytes back to RadixCiphertext
func deserializeRadixCiphertext(data []byte, params *fhe.IntegerParams) (*fhe.RadixCiphertext, error) {
	// Placeholder - proper deserialization requires the full ciphertext data
	// For now, return error as this needs full TFHE serialization support
	if len(data) == 0 {
		return nil, ErrInvalidCiphertext
	}
	// Return nil for now - full serialization support pending
	return nil, ErrInvalidCiphertext
}
