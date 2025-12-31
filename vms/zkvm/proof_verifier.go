// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.


package zvm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/luxfi/log"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	lru "github.com/hashicorp/golang-lru"
)

// ProofVerifier verifies zero-knowledge proofs
type ProofVerifier struct {
	config       ZConfig
	log          log.Logger
	
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
func NewProofVerifier(config ZConfig, log log.Logger) (*ProofVerifier, error) {
	// Create LRU cache for proof verification results
	cache, err := lru.New(int(config.ProofCacheSize))
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

// verifyGroth16Proof verifies a Groth16 proof using gnark
func (pv *ProofVerifier) verifyGroth16Proof(tx *Transaction) error {
	// Get verifying key for circuit type
	vkBytes, exists := pv.verifyingKeys[string(tx.Type)]
	if !exists {
		return errors.New("verifying key not found for circuit type")
	}

	// Verify public inputs match transaction data
	if err := pv.verifyPublicInputs(tx); err != nil {
		return err
	}

	// Validate proof data length (Groth16: 2 G1 points + 1 G2 point)
	// BN254: G1 = 64 bytes (compressed), G2 = 128 bytes (compressed)
	// Total: 2*64 + 128 = 256 bytes minimum
	if len(tx.Proof.ProofData) < 256 {
		return errors.New("invalid proof data length for Groth16")
	}

	// Perform actual Groth16 verification using gnark-crypto
	if err := pv.verifyGroth16WithGnark(tx.Proof, vkBytes); err != nil {
		return fmt.Errorf("groth16 verification failed: %w", err)
	}

	pv.log.Debug("Groth16 proof verified",
		log.String("txID", tx.ID.String()),
		log.Int("vkLen", len(vkBytes)),
	)

	return nil
}

// verifyPLONKProof verifies a PLONK proof
func (pv *ProofVerifier) verifyPLONKProof(tx *Transaction) error {
	// TODO: Implement actual PLONK verification when library is available
	// For now, perform basic structural validation

	// Verify public inputs
	if err := pv.verifyPublicInputs(tx); err != nil {
		return err
	}

	// Basic validation - PLONK proofs are larger than Groth16
	if len(tx.Proof.ProofData) < 512 {
		return errors.New("invalid PLONK proof data length")
	}

	pv.log.Warn("PLONK verification not fully implemented - using basic validation only")
	return nil
}

// verifyBulletproof verifies a Bulletproof (for range proofs)
func (pv *ProofVerifier) verifyBulletproof(tx *Transaction) error {
	// TODO: Implement actual Bulletproof verification when library is available
	// For now, perform basic structural validation

	// Bulletproofs are typically used for range proofs on amounts
	// Verify each output has a valid range proof structure
	for i, output := range tx.Outputs {
		if len(output.OutputProof) < 128 {
			return fmt.Errorf("invalid range proof for output %d: too short", i)
		}

		pv.log.Debug("Range proof structure validated",
			log.Int("outputIndex", i),
			log.String("commitment", fmt.Sprintf("%x", output.Commitment[:8])),
		)
	}

	pv.log.Warn("Bulletproof verification not fully implemented - using basic validation only")
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
		log.Int("count", len(pv.verifyingKeys)),
		log.String("proofSystem", pv.config.ProofSystem),
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

// Groth16Proof represents a Groth16 proof structure
type Groth16Proof struct {
	Ar bn254.G1Affine // Proof component A
	Bs bn254.G2Affine // Proof component B
	Krs bn254.G1Affine // Proof component C
}

// Groth16VerifyingKey represents a Groth16 verifying key
type Groth16VerifyingKey struct {
	Alpha bn254.G1Affine   // Alpha in G1
	Beta  bn254.G2Affine   // Beta in G2
	Gamma bn254.G2Affine   // Gamma in G2
	Delta bn254.G2Affine   // Delta in G2
	K     []bn254.G1Affine // K[i] for public inputs
}

// verifyGroth16WithGnark performs actual Groth16 verification using pairing operations
func (pv *ProofVerifier) verifyGroth16WithGnark(proof *ZKProof, vkBytes []byte) error {
	// Deserialize verifying key
	vk, err := deserializeVerifyingKey(vkBytes)
	if err != nil {
		return fmt.Errorf("failed to deserialize verifying key: %w", err)
	}

	// Validate verifying key with subgroup checks (CRITICAL for trusted setup validation)
	if err := validateVerifyingKey(vk); err != nil {
		return fmt.Errorf("verifying key validation failed: %w", err)
	}

	// Deserialize proof
	grothProof, err := deserializeGroth16Proof(proof.ProofData)
	if err != nil {
		return fmt.Errorf("failed to deserialize proof: %w", err)
	}

	// Deserialize public witness (public inputs)
	witness := make([]fr.Element, 0, len(proof.PublicInputs))
	for _, inputBytes := range proof.PublicInputs {
		var elem fr.Element
		elem.SetBytes(inputBytes)
		witness = append(witness, elem)
	}

	// Perform pairing-based verification
	if err := verifyGroth16Pairing(grothProof, vk, witness); err != nil {
		return fmt.Errorf("pairing verification failed: %w", err)
	}

	return nil
}

// verifyGroth16Pairing performs the Groth16 pairing check
// Verifies: e(A, B) = e(α, β) · e(Σ(pubInput_i · K_i), γ) · e(C, δ)
func verifyGroth16Pairing(proof *Groth16Proof, vk *Groth16VerifyingKey, witness []fr.Element) error {
	// Compute public input linear combination: Σ(witness_i · K_i)
	if len(witness) > len(vk.K) {
		return errors.New("too many public inputs")
	}

	var publicInputLC bn254.G1Affine
	publicInputLC.Set(&vk.K[0]) // Start with K[0] (constant term)

	for i, w := range witness {
		var term bn254.G1Affine
		term.ScalarMultiplication(&vk.K[i+1], w.BigInt(nil))
		publicInputLC.Add(&publicInputLC, &term)
	}

	// Perform pairing check: e(A, B) == e(α, β) · e(publicInputLC, γ) · e(C, δ)
	// Rearranged: e(A, B) · e(-publicInputLC, γ) · e(-C, δ) == e(α, β)
	// Or equivalently: e(A, B) == e(α, β) · e(publicInputLC, γ) · e(C, δ)

	// Compute left side: e(A, B)
	var leftSide bn254.GT
	leftSide, err := bn254.Pair([]bn254.G1Affine{proof.Ar}, []bn254.G2Affine{proof.Bs})
	if err != nil {
		return fmt.Errorf("pairing A·B failed: %w", err)
	}

	// Compute right side components
	var rightSide bn254.GT

	// e(α, β)
	alphaBeta, err := bn254.Pair([]bn254.G1Affine{vk.Alpha}, []bn254.G2Affine{vk.Beta})
	if err != nil {
		return fmt.Errorf("pairing α·β failed: %w", err)
	}
	rightSide.Set(&alphaBeta)

	// e(publicInputLC, γ)
	pubGamma, err := bn254.Pair([]bn254.G1Affine{publicInputLC}, []bn254.G2Affine{vk.Gamma})
	if err != nil {
		return fmt.Errorf("pairing pubInput·γ failed: %w", err)
	}
	rightSide.Mul(&rightSide, &pubGamma)

	// e(C, δ)
	cDelta, err := bn254.Pair([]bn254.G1Affine{proof.Krs}, []bn254.G2Affine{vk.Delta})
	if err != nil {
		return fmt.Errorf("pairing C·δ failed: %w", err)
	}
	rightSide.Mul(&rightSide, &cDelta)

	// Compare left and right sides
	if !leftSide.Equal(&rightSide) {
		return errors.New("pairing check failed: proof is invalid")
	}

	return nil
}

// validateVerifyingKey performs subgroup checks on verifying key elliptic curve points
// This is CRITICAL for trusted setup validation - ensures points are in correct subgroup
func validateVerifyingKey(vk *Groth16VerifyingKey) error {
	// Validate Alpha is in G1 subgroup
	if !vk.Alpha.IsInSubGroup() {
		return errors.New("Alpha point not in G1 subgroup")
	}

	// Validate Beta is in G2 subgroup
	if !vk.Beta.IsInSubGroup() {
		return errors.New("Beta point not in G2 subgroup")
	}

	// Validate Gamma is in G2 subgroup
	if !vk.Gamma.IsInSubGroup() {
		return errors.New("Gamma point not in G2 subgroup")
	}

	// Validate Delta is in G2 subgroup
	if !vk.Delta.IsInSubGroup() {
		return errors.New("Delta point not in G2 subgroup")
	}

	// Validate all K points are in G1 subgroup
	for i := range vk.K {
		if !vk.K[i].IsInSubGroup() {
			return fmt.Errorf("K[%d] point not in G1 subgroup", i)
		}
	}

	return nil
}

// deserializeGroth16Proof deserializes a Groth16 proof from bytes
func deserializeGroth16Proof(data []byte) (*Groth16Proof, error) {
	// Expected format: Ar (64 bytes) | Bs (128 bytes) | Krs (64 bytes) = 256 bytes
	if len(data) < 256 {
		return nil, errors.New("proof data too short")
	}

	proof := &Groth16Proof{}
	offset := 0

	// Deserialize Ar (G1 point, 64 bytes compressed)
	if err := proof.Ar.Unmarshal(data[offset : offset+64]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Ar: %w", err)
	}
	offset += 64

	// Deserialize Bs (G2 point, 128 bytes compressed)
	if err := proof.Bs.Unmarshal(data[offset : offset+128]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Bs: %w", err)
	}
	offset += 128

	// Deserialize Krs (G1 point, 64 bytes compressed)
	if err := proof.Krs.Unmarshal(data[offset : offset+64]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Krs: %w", err)
	}

	return proof, nil
}

// deserializeVerifyingKey deserializes a Groth16 verifying key from bytes
func deserializeVerifyingKey(data []byte) (*Groth16VerifyingKey, error) {
	// Format: Alpha (64) | Beta (128) | Gamma (128) | Delta (128) | numK (4) | K[...] (64*numK)
	minSize := 64 + 128 + 128 + 128 + 4
	if len(data) < minSize {
		return nil, errors.New("verifying key data too short")
	}

	vk := &Groth16VerifyingKey{}
	offset := 0

	// Alpha (G1)
	if err := vk.Alpha.Unmarshal(data[offset : offset+64]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Alpha: %w", err)
	}
	offset += 64

	// Beta (G2)
	if err := vk.Beta.Unmarshal(data[offset : offset+128]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Beta: %w", err)
	}
	offset += 128

	// Gamma (G2)
	if err := vk.Gamma.Unmarshal(data[offset : offset+128]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Gamma: %w", err)
	}
	offset += 128

	// Delta (G2)
	if err := vk.Delta.Unmarshal(data[offset : offset+128]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Delta: %w", err)
	}
	offset += 128

	// Number of K points
	numK := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	if len(data) < offset+int(numK)*64 {
		return nil, errors.New("insufficient data for K points")
	}

	// K points (G1)
	vk.K = make([]bn254.G1Affine, numK)
	for i := uint32(0); i < numK; i++ {
		if err := vk.K[i].Unmarshal(data[offset : offset+64]); err != nil {
			return nil, fmt.Errorf("failed to unmarshal K[%d]: %w", i, err)
		}
		offset += 64
	}

	return vk, nil
}

// bytesReader is a simple io.Reader implementation for byte slices
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data}
}

func (br *bytesReader) Read(p []byte) (n int, err error) {
	if br.pos >= len(br.data) {
		return 0, io.EOF
	}
	n = copy(p, br.data[br.pos:])
	br.pos += n
	return n, nil
}
