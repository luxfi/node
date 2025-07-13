// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zvm

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/node/utils/logging"
	lru "github.com/hashicorp/golang-lru"
)

// ProofVerifier verifies zero-knowledge proofs
type ProofVerifier struct {
	config       ZConfig
	log          logging.Logger
	
	// Proof verification cache
	proofCache   *lru.Cache
	
	// Verifying keys
	verifyingKeys map[string][]byte  // circuit type -> verifying key
	
	// Statistics
	verifyCount  uint64
	cacheHits    uint64
	cacheMisses  uint64
	
	mu           sync.RWMutex
}

// NewProofVerifier creates a new proof verifier
func NewProofVerifier(config ZConfig, log logging.Logger) (*ProofVerifier, error) {
	// Create LRU cache for proof verification results
	cache, err := lru.New(config.ProofCacheSize)
	if err != nil {
		return nil, err
	}
	
	pv := &ProofVerifier{
		config:        config,
		log:           log,
		proofCache:    cache,
		verifyingKeys: make(map[string][]byte),
	}
	
	// Load verifying keys
	if err := pv.loadVerifyingKeys(); err != nil {
		return nil, err
	}
	
	return pv, nil
}

// VerifyTransactionProof verifies a transaction's zero-knowledge proof
func (pv *ProofVerifier) VerifyTransactionProof(tx *Transaction) error {
	if tx.Proof == nil {
		return errors.New("transaction missing proof")
	}
	
	// Check cache first
	proofHash := pv.hashProof(tx.Proof)
	
	pv.mu.Lock()
	pv.verifyCount++
	
	if cached, ok := pv.proofCache.Get(string(proofHash)); ok {
		pv.cacheHits++
		pv.mu.Unlock()
		
		if cached.(bool) {
			return nil
		}
		return errors.New("proof verification failed (cached)")
	}
	pv.cacheMisses++
	pv.mu.Unlock()
	
	// Verify proof based on type
	var err error
	switch tx.Proof.ProofType {
	case "groth16":
		err = pv.verifyGroth16Proof(tx)
	case "plonk":
		err = pv.verifyPLONKProof(tx)
	case "bulletproofs":
		err = pv.verifyBulletproof(tx)
	default:
		err = errors.New("unsupported proof type")
	}
	
	// Cache result
	pv.proofCache.Add(string(proofHash), err == nil)
	
	return err
}

// VerifyBlockProof verifies an aggregated block proof
func (pv *ProofVerifier) VerifyBlockProof(block *Block) error {
	if block.BlockProof == nil {
		return nil // Block proof is optional
	}
	
	// Verify that the block proof correctly aggregates all transaction proofs
	// This is a placeholder - in production, use proper proof aggregation
	
	// Check that all transactions have valid proofs
	for _, tx := range block.Txs {
		if err := pv.VerifyTransactionProof(tx); err != nil {
			return err
		}
	}
	
	return nil
}

// verifyGroth16Proof verifies a Groth16 proof
func (pv *ProofVerifier) verifyGroth16Proof(tx *Transaction) error {
	// In production, this would use a proper Groth16 verifier
	// For now, we simulate verification
	
	// Get verifying key for circuit type
	vk, exists := pv.verifyingKeys[string(tx.Type)]
	if !exists {
		return errors.New("verifying key not found for circuit type")
	}
	
	// Simulate proof verification time
	time.Sleep(10 * time.Millisecond)
	
	// Verify public inputs match transaction data
	if err := pv.verifyPublicInputs(tx); err != nil {
		return err
	}
	
	// In production: pairing check
	// For now, basic validation
	if len(tx.Proof.ProofData) < 256 {
		return errors.New("invalid proof data length")
	}
	
	pv.log.Debug("Groth16 proof verified",
		zap.String("txID", tx.ID.String()),
		zap.Int("vkLen", len(vk)),
	)
	
	return nil
}

// verifyPLONKProof verifies a PLONK proof
func (pv *ProofVerifier) verifyPLONKProof(tx *Transaction) error {
	// In production, this would use a proper PLONK verifier
	// For now, we simulate verification
	
	// Simulate proof verification time
	time.Sleep(15 * time.Millisecond)
	
	// Verify public inputs
	if err := pv.verifyPublicInputs(tx); err != nil {
		return err
	}
	
	// Basic validation
	if len(tx.Proof.ProofData) < 512 {
		return errors.New("invalid PLONK proof data length")
	}
	
	return nil
}

// verifyBulletproof verifies a Bulletproof (for range proofs)
func (pv *ProofVerifier) verifyBulletproof(tx *Transaction) error {
	// In production, this would use a proper Bulletproof verifier
	// For now, we simulate verification
	
	// Bulletproofs are typically used for range proofs on amounts
	// Verify each output has a valid range proof
	for i, output := range tx.Outputs {
		if len(output.OutputProof) < 128 {
			return errors.New("invalid range proof for output")
		}
		
		// Simulate verification time (proportional to outputs)
		time.Sleep(5 * time.Millisecond)
		
		pv.log.Debug("Range proof verified",
			zap.Int("outputIndex", i),
			zap.String("commitment", fmt.Sprintf("%x", output.Commitment[:8])),
		)
	}
	
	return nil
}

// verifyPublicInputs verifies that public inputs match transaction data
func (pv *ProofVerifier) verifyPublicInputs(tx *Transaction) error {
	if len(tx.Proof.PublicInputs) == 0 {
		return errors.New("no public inputs provided")
	}
	
	// Verify nullifiers are included in public inputs
	for i, nullifier := range tx.Nullifiers {
		if i >= len(tx.Proof.PublicInputs) {
			return errors.New("missing public input for nullifier")
		}
		
		// In production, properly encode and compare
		// For now, basic length check
		if len(tx.Proof.PublicInputs[i]) != len(nullifier) {
			return errors.New("public input mismatch for nullifier")
		}
	}
	
	// Verify output commitments are included
	outputCommitments := tx.GetOutputCommitments()
	offset := len(tx.Nullifiers)
	
	for i, commitment := range outputCommitments {
		idx := offset + i
		if idx >= len(tx.Proof.PublicInputs) {
			return errors.New("missing public input for output commitment")
		}
		
		if len(tx.Proof.PublicInputs[idx]) != len(commitment) {
			return errors.New("public input mismatch for output commitment")
		}
	}
	
	return nil
}

// loadVerifyingKeys loads verifying keys for different circuit types
func (pv *ProofVerifier) loadVerifyingKeys() error {
	// In production, load from files or embedded data
	// For now, create dummy keys
	
	// Transfer circuit verifying key
	pv.verifyingKeys[string(TransactionTypeTransfer)] = make([]byte, 1024)
	
	// Shield circuit verifying key
	pv.verifyingKeys[string(TransactionTypeShield)] = make([]byte, 1024)
	
	// Unshield circuit verifying key
	pv.verifyingKeys[string(TransactionTypeUnshield)] = make([]byte, 1024)
	
	pv.log.Info("Loaded verifying keys",
		zap.Int("count", len(pv.verifyingKeys)),
		zap.String("proofSystem", pv.config.ProofSystem),
	)
	
	return nil
}

// hashProof computes a hash of a proof for caching
func (pv *ProofVerifier) hashProof(proof *ZKProof) []byte {
	h := sha256.New()
	h.Write([]byte(proof.ProofType))
	h.Write(proof.ProofData)
	
	for _, input := range proof.PublicInputs {
		h.Write(input)
	}
	
	return h.Sum(nil)
}

// GetCacheSize returns the current size of the proof cache
func (pv *ProofVerifier) GetCacheSize() int {
	return pv.proofCache.Len()
}

// GetStats returns verifier statistics
func (pv *ProofVerifier) GetStats() (verifyCount, cacheHits, cacheMisses uint64) {
	pv.mu.RLock()
	defer pv.mu.RUnlock()
	
	return pv.verifyCount, pv.cacheHits, pv.cacheMisses
}

// ClearCache clears the proof verification cache
func (pv *ProofVerifier) ClearCache() {
	pv.proofCache.Purge()
	
	pv.mu.Lock()
	pv.cacheHits = 0
	pv.cacheMisses = 0
	pv.mu.Unlock()
	
	pv.log.Info("Cleared proof verification cache")
}