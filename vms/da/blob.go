// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package da provides Data Availability layer infrastructure for the Lux blockchain.
// DA is consensus-critical: validators must certify availability before block finalization.
package da

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/luxfi/ids"
)

const (
	// DefaultChunkSize is the default size of each data chunk
	DefaultChunkSize = 512

	// DefaultFieldModulus is the modulus for finite field operations (BLS12-381)
	DefaultFieldModulus = "52435875175126190479447740508185965837690552500527637822603658699938581184513"

	// MaxBlobSize is the maximum size of a blob
	MaxBlobSize = 128 * 1024 // 128KB

	// MinSampleCount is the minimum number of samples for availability verification
	MinSampleCount = 16
)

var (
	// ErrBlobTooLarge indicates the blob exceeds maximum size
	ErrBlobTooLarge = errors.New("blob exceeds maximum size")

	// ErrInvalidProof indicates an invalid availability proof
	ErrInvalidProof = errors.New("invalid availability proof")

	// ErrInvalidCommitment indicates an invalid KZG commitment
	ErrInvalidCommitment = errors.New("invalid commitment")

	// ErrChunkNotFound indicates a chunk was not found
	ErrChunkNotFound = errors.New("chunk not found")

	// ErrInsufficientSamples indicates not enough samples were provided
	ErrInsufficientSamples = errors.New("insufficient samples for availability verification")
)

// DABlob represents a data availability blob
type DABlob struct {
	ID         ids.ID    `json:"id"`
	Data       []byte    `json:"data"`
	Commitment []byte    `json:"commitment"` // KZG commitment
	Chunks     []*Chunk  `json:"chunks"`
	ChunkCount uint32    `json:"chunkCount"`
	ChunkSize  uint32    `json:"chunkSize"`
	Height     uint64    `json:"height"`    // Block height where blob was included
	Timestamp  time.Time `json:"timestamp"`
	Submitter  ids.ID    `json:"submitter"` // Who submitted the blob
}

// Chunk represents a chunk of data with its proof
type Chunk struct {
	Index      uint32 `json:"index"`
	Data       []byte `json:"data"`
	Proof      []byte `json:"proof"` // KZG proof for this chunk
	Commitment []byte `json:"commitment"`
}

// DACommitment represents a commitment to data availability
type DACommitment struct {
	BlobID        ids.ID `json:"blobId"`
	Commitment    []byte `json:"commitment"`    // KZG commitment
	ChunkCount    uint32 `json:"chunkCount"`
	DataRoot      []byte `json:"dataRoot"`      // Merkle root of chunks
	ErasureRoot   []byte `json:"erasureRoot"`   // Erasure coding root
	Height        uint64 `json:"height"`
	ValidatorSigs []byte `json:"validatorSigs"` // Aggregated validator signatures
}

// DACert represents a Data Availability Certificate
type DACert struct {
	Commitment    *DACommitment `json:"commitment"`
	Signatures    [][]byte      `json:"signatures"`    // Validator signatures
	SignerBitmap  []byte        `json:"signerBitmap"`  // Bitmap of signing validators
	Threshold     uint32        `json:"threshold"`     // Required signature threshold
	Timestamp     int64         `json:"timestamp"`
}

// Sample represents a random sample for availability verification
type Sample struct {
	ChunkIndex uint32 `json:"chunkIndex"`
	Data       []byte `json:"data"`
	Proof      []byte `json:"proof"`
}

// SamplingResult represents the result of data availability sampling
type SamplingResult struct {
	BlobID      ids.ID    `json:"blobId"`
	Samples     []*Sample `json:"samples"`
	SampleCount int       `json:"sampleCount"`
	Available   bool      `json:"available"`
	Confidence  float64   `json:"confidence"` // Confidence level (0-1)
	Timestamp   time.Time `json:"timestamp"`
}

// NewDABlob creates a new DA blob from data
func NewDABlob(data []byte, submitter ids.ID, height uint64) (*DABlob, error) {
	if len(data) > MaxBlobSize {
		return nil, ErrBlobTooLarge
	}

	// Compute blob ID
	h := sha256.New()
	h.Write(data)
	binary.Write(h, binary.BigEndian, height)
	h.Write(submitter[:])
	blobID := ids.ID(h.Sum(nil))

	blob := &DABlob{
		ID:         blobID,
		Data:       data,
		ChunkSize:  DefaultChunkSize,
		Height:     height,
		Timestamp:  time.Now(),
		Submitter:  submitter,
	}

	// Split into chunks
	if err := blob.createChunks(); err != nil {
		return nil, err
	}

	// Generate commitment
	if err := blob.generateCommitment(); err != nil {
		return nil, err
	}

	return blob, nil
}

// createChunks splits the blob data into chunks
func (b *DABlob) createChunks() error {
	chunkSize := int(b.ChunkSize)
	data := b.Data

	// Pad data to multiple of chunk size
	paddedLen := ((len(data) + chunkSize - 1) / chunkSize) * chunkSize
	if paddedLen > len(data) {
		padded := make([]byte, paddedLen)
		copy(padded, data)
		data = padded
	}

	chunkCount := len(data) / chunkSize
	b.ChunkCount = uint32(chunkCount)
	b.Chunks = make([]*Chunk, chunkCount)

	for i := 0; i < chunkCount; i++ {
		start := i * chunkSize
		end := start + chunkSize
		chunkData := make([]byte, chunkSize)
		copy(chunkData, data[start:end])

		b.Chunks[i] = &Chunk{
			Index: uint32(i),
			Data:  chunkData,
		}
	}

	return nil
}

// generateCommitment generates a KZG-style commitment for the blob
func (b *DABlob) generateCommitment() error {
	// Simplified commitment: hash of all chunks
	// In production: use actual KZG commitments
	h := sha256.New()
	for _, chunk := range b.Chunks {
		h.Write(chunk.Data)
	}
	b.Commitment = h.Sum(nil)

	// Generate proofs for each chunk
	for i, chunk := range b.Chunks {
		chunk.Commitment = b.Commitment
		chunk.Proof = generateChunkProof(b, i)
	}

	return nil
}

// generateChunkProof generates a proof for a specific chunk
func generateChunkProof(blob *DABlob, index int) []byte {
	// Simplified proof: Merkle path
	// In production: use KZG proofs
	h := sha256.New()
	h.Write(blob.Commitment)
	binary.Write(h, binary.BigEndian, uint32(index))
	h.Write(blob.Chunks[index].Data)
	return h.Sum(nil)
}

// GetChunk returns a specific chunk by index
func (b *DABlob) GetChunk(index uint32) (*Chunk, error) {
	if index >= b.ChunkCount {
		return nil, ErrChunkNotFound
	}
	return b.Chunks[index], nil
}

// VerifyChunk verifies a chunk against the commitment
func (b *DABlob) VerifyChunk(chunk *Chunk) bool {
	if chunk.Index >= b.ChunkCount {
		return false
	}

	// Verify commitment matches
	if !bytes.Equal(chunk.Commitment, b.Commitment) {
		return false
	}

	// Verify proof
	expectedProof := generateChunkProof(b, int(chunk.Index))
	return bytes.Equal(chunk.Proof, expectedProof)
}

// CreateCommitment creates a DACommitment from the blob
func (b *DABlob) CreateCommitment() *DACommitment {
	// Compute data root (Merkle root of chunks)
	dataRoot := computeMerkleRoot(b.Chunks)

	return &DACommitment{
		BlobID:      b.ID,
		Commitment:  b.Commitment,
		ChunkCount:  b.ChunkCount,
		DataRoot:    dataRoot,
		ErasureRoot: nil, // Would be set after erasure coding
		Height:      b.Height,
	}
}

// computeMerkleRoot computes Merkle root of chunks
func computeMerkleRoot(chunks []*Chunk) []byte {
	if len(chunks) == 0 {
		return nil
	}

	// Compute leaf hashes
	leaves := make([][]byte, len(chunks))
	for i, chunk := range chunks {
		h := sha256.Sum256(chunk.Data)
		leaves[i] = h[:]
	}

	// Build Merkle tree
	for len(leaves) > 1 {
		if len(leaves)%2 != 0 {
			leaves = append(leaves, leaves[len(leaves)-1])
		}

		newLeaves := make([][]byte, len(leaves)/2)
		for i := 0; i < len(leaves); i += 2 {
			h := sha256.New()
			h.Write(leaves[i])
			h.Write(leaves[i+1])
			newLeaves[i/2] = h.Sum(nil)
		}
		leaves = newLeaves
	}

	return leaves[0]
}

// ======== Data Availability Sampling ========

// SamplingConfig configures DA sampling parameters
type SamplingConfig struct {
	SampleCount    int     `json:"sampleCount"`    // Number of samples to request
	Threshold      float64 `json:"threshold"`      // Required success rate (0-1)
	Timeout        int     `json:"timeout"`        // Sampling timeout in seconds
	RetryCount     int     `json:"retryCount"`     // Number of retries per sample
}

// DefaultSamplingConfig returns default sampling configuration
func DefaultSamplingConfig() *SamplingConfig {
	return &SamplingConfig{
		SampleCount: 16,
		Threshold:   0.75,
		Timeout:     30,
		RetryCount:  3,
	}
}

// Sampler handles data availability sampling
type Sampler struct {
	config *SamplingConfig
}

// NewSampler creates a new DA sampler
func NewSampler(config *SamplingConfig) *Sampler {
	if config == nil {
		config = DefaultSamplingConfig()
	}
	return &Sampler{config: config}
}

// GenerateSampleIndices generates random sample indices for a blob
func (s *Sampler) GenerateSampleIndices(blobID ids.ID, chunkCount uint32, seed []byte) []uint32 {
	if int(chunkCount) < s.config.SampleCount {
		// If fewer chunks than samples, sample all
		indices := make([]uint32, chunkCount)
		for i := uint32(0); i < chunkCount; i++ {
			indices[i] = i
		}
		return indices
	}

	// Generate pseudo-random indices using seed
	indices := make([]uint32, s.config.SampleCount)
	h := sha256.New()
	h.Write(blobID[:])
	h.Write(seed)

	for i := 0; i < s.config.SampleCount; i++ {
		binary.Write(h, binary.BigEndian, uint32(i))
		hash := h.Sum(nil)
		h.Reset()
		h.Write(hash)

		// Convert to index
		index := binary.BigEndian.Uint32(hash[:4]) % chunkCount
		indices[i] = index
	}

	return indices
}

// VerifySamples verifies sampled chunks and returns sampling result
func (s *Sampler) VerifySamples(blob *DABlob, samples []*Sample) *SamplingResult {
	if len(samples) < MinSampleCount {
		return &SamplingResult{
			BlobID:      blob.ID,
			Samples:     samples,
			SampleCount: len(samples),
			Available:   false,
			Confidence:  0,
			Timestamp:   time.Now(),
		}
	}

	validCount := 0
	for _, sample := range samples {
		chunk, err := blob.GetChunk(sample.ChunkIndex)
		if err != nil {
			continue
		}

		// Verify sample matches chunk
		if bytes.Equal(sample.Data, chunk.Data) && bytes.Equal(sample.Proof, chunk.Proof) {
			validCount++
		}
	}

	successRate := float64(validCount) / float64(len(samples))
	available := successRate >= s.config.Threshold

	// Calculate confidence based on sample count and success rate
	// Higher sample count and success rate = higher confidence
	confidence := successRate * (1 - 0.5*float64(MinSampleCount)/float64(len(samples)))

	return &SamplingResult{
		BlobID:      blob.ID,
		Samples:     samples,
		SampleCount: len(samples),
		Available:   available,
		Confidence:  confidence,
		Timestamp:   time.Now(),
	}
}

// ======== Erasure Coding ========

// ErasureConfig configures erasure coding parameters
type ErasureConfig struct {
	DataShards   int `json:"dataShards"`   // Original data shards
	ParityShards int `json:"parityShards"` // Parity shards for recovery
}

// DefaultErasureConfig returns default erasure coding configuration
func DefaultErasureConfig() *ErasureConfig {
	return &ErasureConfig{
		DataShards:   16,
		ParityShards: 16,
	}
}

// ErasureCodedBlob represents an erasure-coded blob
type ErasureCodedBlob struct {
	*DABlob
	DataShards   [][]byte `json:"dataShards"`
	ParityShards [][]byte `json:"parityShards"`
	ErasureRoot  []byte   `json:"erasureRoot"`
}

// ApplyErasureCoding applies erasure coding to a blob
func ApplyErasureCoding(blob *DABlob, config *ErasureConfig) (*ErasureCodedBlob, error) {
	if config == nil {
		config = DefaultErasureConfig()
	}

	// Simplified erasure coding: duplicate chunks as parity
	// In production: use Reed-Solomon encoding
	dataShards := make([][]byte, len(blob.Chunks))
	for i, chunk := range blob.Chunks {
		dataShards[i] = chunk.Data
	}

	// Create parity shards (simplified: XOR-based)
	parityShards := make([][]byte, config.ParityShards)
	for i := 0; i < config.ParityShards; i++ {
		parity := make([]byte, blob.ChunkSize)
		// XOR data shards to create parity
		for _, data := range dataShards {
			for j := range parity {
				if j < len(data) {
					parity[j] ^= data[j]
				}
			}
		}
		parityShards[i] = parity
	}

	// Compute erasure root
	allShards := append(dataShards, parityShards...)
	h := sha256.New()
	for _, shard := range allShards {
		h.Write(shard)
	}
	erasureRoot := h.Sum(nil)

	return &ErasureCodedBlob{
		DABlob:       blob,
		DataShards:   dataShards,
		ParityShards: parityShards,
		ErasureRoot:  erasureRoot,
	}, nil
}

// CanRecover checks if the blob can be recovered from available shards
func (e *ErasureCodedBlob) CanRecover(availableIndices []uint32) bool {
	// Need at least DataShards number of shards to recover
	return len(availableIndices) >= len(e.DataShards)
}

// ======== Block Header DA Fields ========

// BlockDAInfo contains DA-related information for a block header
type BlockDAInfo struct {
	BlobCommitments [][]byte `json:"blobCommitments"` // Commitments for all blobs
	DARoot          []byte   `json:"daRoot"`          // Root of DA commitments
	WitnessRoot     []byte   `json:"witnessRoot"`     // Root of witnesses/proofs
	BlobCount       uint32   `json:"blobCount"`       // Number of blobs in block
	TotalDataSize   uint64   `json:"totalDataSize"`   // Total data size in bytes
}

// ComputeDARoot computes the DA root from commitments
func ComputeDARoot(commitments []*DACommitment) []byte {
	if len(commitments) == 0 {
		return nil
	}

	leaves := make([][]byte, len(commitments))
	for i, c := range commitments {
		h := sha256.New()
		h.Write(c.BlobID[:])
		h.Write(c.Commitment)
		h.Write(c.DataRoot)
		leaves[i] = h.Sum(nil)
	}

	return computeMerkleRootFromLeaves(leaves)
}

func computeMerkleRootFromLeaves(leaves [][]byte) []byte {
	if len(leaves) == 0 {
		return nil
	}

	for len(leaves) > 1 {
		if len(leaves)%2 != 0 {
			leaves = append(leaves, leaves[len(leaves)-1])
		}

		newLeaves := make([][]byte, len(leaves)/2)
		for i := 0; i < len(leaves); i += 2 {
			h := sha256.New()
			h.Write(leaves[i])
			h.Write(leaves[i+1])
			newLeaves[i/2] = h.Sum(nil)
		}
		leaves = newLeaves
	}

	return leaves[0]
}
