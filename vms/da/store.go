// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package da

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
)

var (
	blobPrefix = []byte("blob:")
	certPrefix = []byte("cert:")

	// ErrBlobNotFound indicates blob was not found
	ErrBlobNotFound = errors.New("blob not found")

	// ErrCertNotFound indicates certificate was not found
	ErrCertNotFound = errors.New("DA certificate not found")
)

// Store manages DA blob storage and retrieval
type Store struct {
	db     database.Database
	blobs  map[ids.ID]*DABlob
	certs  map[ids.ID]*DACert
	mu     sync.RWMutex
	config *StoreConfig
}

// StoreConfig configures the DA store
type StoreConfig struct {
	MaxBlobCache     int   `json:"maxBlobCache"`     // Maximum blobs to cache in memory
	RetentionPeriod  int64 `json:"retentionPeriod"`  // Blob retention in seconds
	EnableErasure    bool  `json:"enableErasure"`    // Enable erasure coding
	ErasureDataRatio int   `json:"erasureDataRatio"` // Data to parity ratio
}

// DefaultStoreConfig returns default store configuration
func DefaultStoreConfig() *StoreConfig {
	return &StoreConfig{
		MaxBlobCache:     1000,
		RetentionPeriod:  7 * 24 * 3600, // 7 days
		EnableErasure:    true,
		ErasureDataRatio: 2,
	}
}

// NewStore creates a new DA store
func NewStore(db database.Database, config *StoreConfig) *Store {
	if config == nil {
		config = DefaultStoreConfig()
	}

	return &Store{
		db:     db,
		blobs:  make(map[ids.ID]*DABlob),
		certs:  make(map[ids.ID]*DACert),
		config: config,
	}
}

// StoreBlob stores a DA blob (ZAP-encoded).
func (s *Store) StoreBlob(ctx context.Context, blob *DABlob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store in memory cache
	s.blobs[blob.ID] = blob

	// Persist to database
	blobBytes := encodeDABlob(blob)

	key := append(blobPrefix, blob.ID[:]...)
	return s.db.Put(key, blobBytes)
}

// GetBlob retrieves a DA blob by ID
func (s *Store) GetBlob(ctx context.Context, blobID ids.ID) (*DABlob, error) {
	s.mu.RLock()
	if blob, ok := s.blobs[blobID]; ok {
		s.mu.RUnlock()
		return blob, nil
	}
	s.mu.RUnlock()

	// Try database
	key := append(blobPrefix, blobID[:]...)
	blobBytes, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}

	blob, err := decodeDABlob(blobBytes)
	if err != nil {
		return nil, err
	}

	// Cache for future access
	s.mu.Lock()
	s.blobs[blobID] = blob
	s.mu.Unlock()

	return blob, nil
}

// DeleteBlob deletes a DA blob
func (s *Store) DeleteBlob(ctx context.Context, blobID ids.ID) error {
	s.mu.Lock()
	delete(s.blobs, blobID)
	s.mu.Unlock()

	key := append(blobPrefix, blobID[:]...)
	return s.db.Delete(key)
}

// StoreCert stores a DA certificate (ZAP-encoded).
func (s *Store) StoreCert(ctx context.Context, cert *DACert) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.certs[cert.Commitment.BlobID] = cert

	certBytes := encodeDACert(cert)

	key := append(certPrefix, cert.Commitment.BlobID[:]...)
	return s.db.Put(key, certBytes)
}

// GetCert retrieves a DA certificate by blob ID
func (s *Store) GetCert(ctx context.Context, blobID ids.ID) (*DACert, error) {
	s.mu.RLock()
	if cert, ok := s.certs[blobID]; ok {
		s.mu.RUnlock()
		return cert, nil
	}
	s.mu.RUnlock()

	key := append(certPrefix, blobID[:]...)
	certBytes, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return nil, ErrCertNotFound
		}
		return nil, err
	}

	cert, err := decodeDACert(certBytes)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.certs[blobID] = cert
	s.mu.Unlock()

	return cert, nil
}

// GetChunks retrieves specific chunks from a blob
func (s *Store) GetChunks(ctx context.Context, blobID ids.ID, indices []uint32) ([]*Chunk, error) {
	blob, err := s.GetBlob(ctx, blobID)
	if err != nil {
		return nil, err
	}

	chunks := make([]*Chunk, 0, len(indices))
	for _, idx := range indices {
		if idx < blob.ChunkCount {
			chunks = append(chunks, blob.Chunks[idx])
		}
	}

	return chunks, nil
}

// PruneExpired removes expired blobs
func (s *Store) PruneExpired(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(s.config.RetentionPeriod) * time.Second)
	pruned := 0

	for id, blob := range s.blobs {
		if blob.Timestamp.Before(cutoff) {
			delete(s.blobs, id)
			delete(s.certs, id)

			key := append(blobPrefix, id[:]...)
			s.db.Delete(key)

			certKey := append(certPrefix, id[:]...)
			s.db.Delete(certKey)

			pruned++
		}
	}

	return pruned, nil
}

// Stats returns store statistics
func (s *Store) Stats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"blobCount":    len(s.blobs),
		"certCount":    len(s.certs),
		"maxBlobCache": s.config.MaxBlobCache,
	}
}

// ======== DA Validator ========

// Validator handles DA validation and certification
type Validator struct {
	store   *Store
	sampler *Sampler
	config  *ValidatorConfig
}

// ValidatorConfig configures the DA validator
type ValidatorConfig struct {
	MinSignatures    int     `json:"minSignatures"`    // Minimum validator signatures for cert
	SamplingEnabled  bool    `json:"samplingEnabled"`  // Enable light client sampling
	ConfidenceTarget float64 `json:"confidenceTarget"` // Target confidence level
}

// DefaultValidatorConfig returns default validator configuration
func DefaultValidatorConfig() *ValidatorConfig {
	return &ValidatorConfig{
		MinSignatures:    2, // 2/3 threshold typical
		SamplingEnabled:  true,
		ConfidenceTarget: 0.99,
	}
}

// NewValidator creates a new DA validator
func NewValidator(store *Store, config *ValidatorConfig) *Validator {
	if config == nil {
		config = DefaultValidatorConfig()
	}

	return &Validator{
		store:   store,
		sampler: NewSampler(DefaultSamplingConfig()),
		config:  config,
	}
}

// ValidateBlob validates a DA blob and returns commitment
func (v *Validator) ValidateBlob(ctx context.Context, blob *DABlob) (*DACommitment, error) {
	// Verify blob size
	if len(blob.Data) > MaxBlobSize {
		return nil, ErrBlobTooLarge
	}

	// Verify chunks match data
	reconstructed := make([]byte, 0, len(blob.Chunks)*int(blob.ChunkSize))
	for _, chunk := range blob.Chunks {
		reconstructed = append(reconstructed, chunk.Data...)
	}

	// Trim padding
	if len(reconstructed) > len(blob.Data) {
		reconstructed = reconstructed[:len(blob.Data)]
	}

	if len(reconstructed) != len(blob.Data) {
		return nil, errors.New("chunk data does not match blob data")
	}

	for i := range blob.Data {
		if blob.Data[i] != reconstructed[i] {
			return nil, errors.New("chunk data mismatch")
		}
	}

	// Verify commitment
	for _, chunk := range blob.Chunks {
		if !blob.VerifyChunk(chunk) {
			return nil, ErrInvalidCommitment
		}
	}

	// Create commitment
	return blob.CreateCommitment(), nil
}

// CertifyAvailability certifies data is available
func (v *Validator) CertifyAvailability(ctx context.Context, commitment *DACommitment, signature []byte) (*DACert, error) {
	// Get existing cert or create new one
	cert, err := v.store.GetCert(ctx, commitment.BlobID)
	if err == ErrCertNotFound {
		cert = &DACert{
			Commitment:   commitment,
			Signatures:   make([][]byte, 0),
			SignerBitmap: make([]byte, 32), // Support up to 256 validators
			Threshold:    uint32(v.config.MinSignatures),
			Timestamp:    time.Now().Unix(),
		}
	} else if err != nil {
		return nil, err
	}

	// Add signature
	cert.Signatures = append(cert.Signatures, signature)

	// Store updated cert
	if err := v.store.StoreCert(ctx, cert); err != nil {
		return nil, err
	}

	return cert, nil
}

// VerifyCert verifies a DA certificate
func (v *Validator) VerifyCert(ctx context.Context, cert *DACert) (bool, error) {
	// Verify minimum signatures
	if len(cert.Signatures) < int(cert.Threshold) {
		return false, nil
	}

	// Verify each signature (simplified - would verify crypto)
	for _, sig := range cert.Signatures {
		if len(sig) == 0 {
			return false, nil
		}
	}

	return true, nil
}

// SampleAndVerify performs DA sampling and verification
func (v *Validator) SampleAndVerify(ctx context.Context, blobID ids.ID, seed []byte) (*SamplingResult, error) {
	blob, err := v.store.GetBlob(ctx, blobID)
	if err != nil {
		return nil, err
	}

	// Generate sample indices
	indices := v.sampler.GenerateSampleIndices(blobID, blob.ChunkCount, seed)

	// Get samples
	samples := make([]*Sample, 0, len(indices))
	for _, idx := range indices {
		chunk, err := blob.GetChunk(idx)
		if err != nil {
			continue
		}

		samples = append(samples, &Sample{
			ChunkIndex: idx,
			Data:       chunk.Data,
			Proof:      chunk.Proof,
		})
	}

	// Verify samples
	return v.sampler.VerifySamples(blob, samples), nil
}

// CalculateConfidence calculates confidence level from sample count
func (v *Validator) CalculateConfidence(sampleCount int, successRate float64) float64 {
	// Simplified confidence calculation
	// Real implementation would use statistical formulas
	if sampleCount < MinSampleCount {
		return 0
	}

	baseConfidence := successRate
	sampleFactor := 1 - (float64(MinSampleCount) / float64(sampleCount*2))

	return baseConfidence * sampleFactor
}
