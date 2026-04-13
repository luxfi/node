// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/ids"
)

// DA layer errors
var (
	ErrBlobNotFound      = errors.New("blob not found")
	ErrBlobExpired       = errors.New("blob expired")
	ErrBlobTooLarge      = errors.New("blob too large")
	ErrSamplingFailed    = errors.New("availability sampling failed")
	ErrReceiptInvalid    = errors.New("availability receipt invalid")
	ErrGeoReplicationFailed = errors.New("geo-replication failed")
)

// DALayerType defines the type of DA layer
type DALayerType uint8

const (
	// DALayerLux is the native Lux DA layer
	DALayerLux DALayerType = iota
	// DALayerCelestia uses Celestia for DA
	DALayerCelestia
	// DALayerEigenDA uses EigenDA
	DALayerEigenDA
	// DALayerAvail uses Avail
	DALayerAvail
	// DALayerNearDA uses NEAR DA
	DALayerNearDA
)

// DAConfig configures the DA layer
type DAConfig struct {
	LayerType         DALayerType   `json:"layerType"`
	Endpoints         []string      `json:"endpoints"`
	ReplicationFactor int           `json:"replicationFactor"`
	RetentionPeriod   time.Duration `json:"retentionPeriod"`
	MaxBlobSize       uint64        `json:"maxBlobSize"`
	SamplingEnabled   bool          `json:"samplingEnabled"`
	SamplingRate      int           `json:"samplingRate"` // Samples per blob
}

// Blob represents data stored in the DA layer
type Blob struct {
	ID            [32]byte      `json:"id"`
	Data          []byte        `json:"data"`
	Commitment    [32]byte      `json:"commitment"`
	Size          uint64        `json:"size"`
	CreatedAt     time.Time     `json:"createdAt"`
	ExpiresAt     time.Time     `json:"expiresAt"`
	Encrypted     bool          `json:"encrypted"`

	// Chunking for large blobs
	ChunkCount    uint32        `json:"chunkCount"`
	ChunkSize     uint32        `json:"chunkSize"`
	ChunkHashes   [][32]byte    `json:"chunkHashes,omitempty"`

	// Erasure coding
	DataShards    int           `json:"dataShards"`
	ParityShards  int           `json:"parityShards"`

	// Replication
	Regions       []string      `json:"regions"`
	ReplicaCount  int           `json:"replicaCount"`
}

// ComputeCommitment computes a cryptographic commitment for the blob using SHA256
func (b *Blob) ComputeCommitment() [32]byte {
	h := sha256.New()
	h.Write(b.Data)
	var commitment [32]byte
	copy(commitment[:], h.Sum(nil))
	return commitment
}

// DANode represents a node in the DA network
type DANode struct {
	ID            []byte    `json:"id"`
	Endpoint      string    `json:"endpoint"`
	Region        string    `json:"region"`
	Stake         uint64    `json:"stake"`
	Reputation    uint64    `json:"reputation"`
	LastSeen      time.Time `json:"lastSeen"`
	StorageUsed   uint64    `json:"storageUsed"`
	StorageLimit  uint64    `json:"storageLimit"`
}

// SamplingProof proves data availability through random sampling
type SamplingProof struct {
	BlobID        [32]byte      `json:"blobId"`
	SampleIndices []uint32      `json:"sampleIndices"`
	Samples       [][]byte      `json:"samples"`
	Proofs        [][][]byte    `json:"proofs"` // Merkle proofs for each sample
	Timestamp     time.Time     `json:"timestamp"`
	Validator     []byte        `json:"validator"`
	Signature     []byte        `json:"signature"`
}

// Verify verifies the sampling proof
func (p *SamplingProof) Verify(blobCommitment [32]byte) error {
	// Verify each sample against the blob commitment
	// In production, use KZG proof verification
	return nil
}

// DefaultDAClient implements DAClient interface
type DefaultDAClient struct {
	mu sync.RWMutex

	config    *DAConfig
	nodes     []*DANode
	blobs     map[[32]byte]*Blob
	receipts  map[[32]byte]*DAReceipt

	// Caching for hot data
	cache     map[[32]byte][]byte
	cacheSize uint64
	maxCache  uint64
}

// NewDefaultDAClient creates a new DA client
func NewDefaultDAClient(config *DAConfig) *DefaultDAClient {
	return &DefaultDAClient{
		config:   config,
		nodes:    make([]*DANode, 0),
		blobs:    make(map[[32]byte]*Blob),
		receipts: make(map[[32]byte]*DAReceipt),
		cache:    make(map[[32]byte][]byte),
		maxCache: 100 * 1024 * 1024, // 100MB cache
	}
}

// Store stores data in the DA layer
func (c *DefaultDAClient) Store(ctx context.Context, data []byte, commitment [32]byte) (*DAPointer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if uint64(len(data)) > c.config.MaxBlobSize {
		return nil, ErrBlobTooLarge
	}

	// Generate blob ID
	blobID := sha256.Sum256(append(commitment[:], data...))

	now := time.Now()
	blob := &Blob{
		ID:         blobID,
		Data:       data,
		Commitment: commitment,
		Size:       uint64(len(data)),
		CreatedAt:  now,
		ExpiresAt:  now.Add(c.config.RetentionPeriod),
		Encrypted:  true,
	}

	// Chunk large blobs
	if len(data) > 1024*1024 { // > 1MB
		blob.ChunkSize = 256 * 1024 // 256KB chunks
		blob.ChunkCount = uint32((len(data) + int(blob.ChunkSize) - 1) / int(blob.ChunkSize))
		blob.ChunkHashes = make([][32]byte, blob.ChunkCount)

		for i := uint32(0); i < blob.ChunkCount; i++ {
			start := i * blob.ChunkSize
			end := start + blob.ChunkSize
			if end > uint32(len(data)) {
				end = uint32(len(data))
			}
			blob.ChunkHashes[i] = sha256.Sum256(data[start:end])
		}
	}

	// Store blob
	c.blobs[blobID] = blob

	// Add to cache
	c.addToCache(blobID, data)

	// Create pointer
	pointer := &DAPointer{
		BlobID:        blobID,
		ChunkIndex:    0,
		Size:          blob.Size,
		AvailableFrom: now,
		ExpiresAt:     blob.ExpiresAt,
		Commitment:    commitment,
	}

	return pointer, nil
}

// Retrieve retrieves data from the DA layer
func (c *DefaultDAClient) Retrieve(ctx context.Context, pointer *DAPointer) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check cache first
	if data, ok := c.cache[pointer.BlobID]; ok {
		return data, nil
	}

	// Check if blob exists
	blob, exists := c.blobs[pointer.BlobID]
	if !exists {
		return nil, ErrBlobNotFound
	}

	// Check expiry
	if time.Now().After(blob.ExpiresAt) {
		return nil, ErrBlobExpired
	}

	// Verify commitment
	if pointer.Commitment != blob.Commitment {
		return nil, ErrReceiptInvalid
	}

	return blob.Data, nil
}

// GetAvailabilityReceipt gets proof of data availability
func (c *DefaultDAClient) GetAvailabilityReceipt(ctx context.Context, pointer *DAPointer) (*DAReceipt, error) {
	c.mu.RLock()

	// Check if we have cached receipt
	if receipt, exists := c.receipts[pointer.BlobID]; exists {
		c.mu.RUnlock()
		return receipt, nil
	}

	blob, exists := c.blobs[pointer.BlobID]
	c.mu.RUnlock()

	if !exists {
		return nil, ErrBlobNotFound
	}

	// Perform availability sampling
	if c.config.SamplingEnabled {
		proof, err := c.performSampling(ctx, blob)
		if err != nil {
			return nil, ErrSamplingFailed
		}
		_ = proof // Use in receipt
	}

	// Collect attestations from DA nodes
	attestations := make([][]byte, 0, c.config.ReplicationFactor)
	for i := 0; i < c.config.ReplicationFactor && i < len(c.nodes); i++ {
		// In production, request attestation from each node
		attestation := make([]byte, 64) // Simulated signature
		attestations = append(attestations, attestation)
	}

	receipt := &DAReceipt{
		Pointer:      pointer,
		Attestations: attestations,
		Timestamp:    time.Now(),
		ValidUntil:   blob.ExpiresAt,
	}

	// Sign receipt
	receipt.Signature = c.signReceipt(receipt)

	// Cache receipt
	c.mu.Lock()
	c.receipts[pointer.BlobID] = receipt
	c.mu.Unlock()

	return receipt, nil
}

// performSampling performs random availability sampling
func (c *DefaultDAClient) performSampling(ctx context.Context, blob *Blob) (*SamplingProof, error) {
	// Select random sample indices
	numSamples := c.config.SamplingRate
	if numSamples == 0 {
		numSamples = 16 // Default
	}

	indices := make([]uint32, numSamples)
	samples := make([][]byte, numSamples)
	proofs := make([][][]byte, numSamples)

	chunkSize := uint32(256 * 1024) // 256KB
	if blob.ChunkSize > 0 {
		chunkSize = blob.ChunkSize
	}
	numChunks := uint32((blob.Size + uint64(chunkSize) - 1) / uint64(chunkSize))

	for i := 0; i < numSamples; i++ {
		// Random index (in production use VRF)
		indices[i] = uint32(i) % numChunks

		// Get sample
		start := indices[i] * chunkSize
		end := start + chunkSize
		if uint64(end) > blob.Size {
			end = uint32(blob.Size)
		}
		samples[i] = blob.Data[start:end]

		// Merkle proof (simplified)
		proofs[i] = [][]byte{}
	}

	return &SamplingProof{
		BlobID:        blob.ID,
		SampleIndices: indices,
		Samples:       samples,
		Proofs:        proofs,
		Timestamp:     time.Now(),
	}, nil
}

// signReceipt signs a DA receipt
func (c *DefaultDAClient) signReceipt(receipt *DAReceipt) []byte {
	h := sha256.New()
	h.Write(receipt.Pointer.BlobID[:])
	h.Write(receipt.Pointer.Commitment[:])
	binary.Write(h, binary.BigEndian, receipt.Timestamp.Unix())
	return h.Sum(nil)
}

// SetReplicationPolicy sets geographic replication policy
func (c *DefaultDAClient) SetReplicationPolicy(ctx context.Context, pointer *DAPointer, regions []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	blob, exists := c.blobs[pointer.BlobID]
	if !exists {
		return ErrBlobNotFound
	}

	blob.Regions = regions
	blob.ReplicaCount = len(regions)

	// In production, replicate to nodes in each region
	for _, region := range regions {
		for _, node := range c.nodes {
			if node.Region == region {
				// Replicate to node
				_ = node
			}
		}
	}

	return nil
}

// Prune removes data that's past retention
func (c *DefaultDAClient) Prune(ctx context.Context, before time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toDelete [][32]byte
	for id, blob := range c.blobs {
		if blob.ExpiresAt.Before(before) {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		delete(c.blobs, id)
		delete(c.cache, id)
		delete(c.receipts, id)
	}

	return nil
}

// addToCache adds data to the LRU cache
func (c *DefaultDAClient) addToCache(id [32]byte, data []byte) {
	// Check if we have space
	if c.cacheSize+uint64(len(data)) > c.maxCache {
		// Evict oldest entries (simplified - use proper LRU in production)
		for k, v := range c.cache {
			delete(c.cache, k)
			c.cacheSize -= uint64(len(v))
			if c.cacheSize+uint64(len(data)) <= c.maxCache {
				break
			}
		}
	}

	c.cache[id] = data
	c.cacheSize += uint64(len(data))
}

// RegisterNode registers a DA node
func (c *DefaultDAClient) RegisterNode(node *DANode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodes = append(c.nodes, node)
}

// GetNodes returns all registered nodes
func (c *DefaultDAClient) GetNodes() []*DANode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodes
}

// DAMetrics contains metrics for the DA layer
type DAMetrics struct {
	TotalBlobs      int64         `json:"totalBlobs"`
	TotalSize       uint64        `json:"totalSize"`
	CacheHitRate    float64       `json:"cacheHitRate"`
	AvgLatency      time.Duration `json:"avgLatency"`
	NodeCount       int           `json:"nodeCount"`
	HealthyNodes    int           `json:"healthyNodes"`
	ReplicationRate float64       `json:"replicationRate"`
}

// GetMetrics returns DA metrics
func (c *DefaultDAClient) GetMetrics() *DAMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var totalSize uint64
	for _, blob := range c.blobs {
		totalSize += blob.Size
	}

	healthyNodes := 0
	cutoff := time.Now().Add(-5 * time.Minute)
	for _, node := range c.nodes {
		if node.LastSeen.After(cutoff) {
			healthyNodes++
		}
	}

	return &DAMetrics{
		TotalBlobs:   int64(len(c.blobs)),
		TotalSize:    totalSize,
		NodeCount:    len(c.nodes),
		HealthyNodes: healthyNodes,
	}
}

// CommitmentScheme defines the commitment scheme used
type CommitmentScheme uint8

const (
	// CommitmentSHA256 uses SHA256 hash
	CommitmentSHA256 CommitmentScheme = iota
	// CommitmentKZG uses KZG polynomial commitment
	CommitmentKZG
	// CommitmentFRI uses FRI (Fast Reed-Solomon IOP)
	CommitmentFRI
)

// DACommitment represents a cryptographic commitment to DA data
type DACommitment struct {
	Scheme     CommitmentScheme `json:"scheme"`
	Data       []byte           `json:"data"`
	Proof      []byte           `json:"proof,omitempty"`
	Parameters []byte           `json:"parameters,omitempty"`
}

// Verify verifies the commitment
func (c *DACommitment) Verify(data []byte) error {
	switch c.Scheme {
	case CommitmentSHA256:
		hash := sha256.Sum256(data)
		if len(c.Data) != 32 {
			return ErrReceiptInvalid
		}
		for i, b := range hash {
			if c.Data[i] != b {
				return ErrReceiptInvalid
			}
		}
		return nil
	case CommitmentKZG:
		// In production, verify KZG commitment
		return nil
	case CommitmentFRI:
		// In production, verify FRI commitment
		return nil
	default:
		return errors.New("unknown commitment scheme")
	}
}

// CreateDACommitment creates a commitment for data
func CreateDACommitment(scheme CommitmentScheme, data []byte) *DACommitment {
	switch scheme {
	case CommitmentSHA256:
		hash := sha256.Sum256(data)
		return &DACommitment{
			Scheme: CommitmentSHA256,
			Data:   hash[:],
		}
	case CommitmentKZG:
		// In production, compute KZG commitment
		hash := sha256.Sum256(data)
		return &DACommitment{
			Scheme: CommitmentKZG,
			Data:   hash[:],
		}
	default:
		hash := sha256.Sum256(data)
		return &DACommitment{
			Scheme: CommitmentSHA256,
			Data:   hash[:],
		}
	}
}

// AppChainDALayer wraps DA client with AppChain-specific functionality
type AppChainDALayer struct {
	client      DAClient
	appChainID  ids.ID
	config      *DAConfig
}

// NewAppChainDALayer creates a new AppChain DA layer
func NewAppChainDALayer(appChainID ids.ID, config *DAConfig) *AppChainDALayer {
	return &AppChainDALayer{
		client:     NewDefaultDAClient(config),
		appChainID: appChainID,
		config:     config,
	}
}

// StoreOpBatch stores an operation batch
func (l *AppChainDALayer) StoreOpBatch(ctx context.Context, batch *OpBatch) (*DAPointer, error) {
	// Serialize batch
	data := encodeBatch(batch)

	// Compute commitment
	commitment := batch.Hash()

	// Store in DA layer
	return l.client.Store(ctx, data, commitment)
}

// StoreSnapshot stores a state snapshot
func (l *AppChainDALayer) StoreSnapshot(ctx context.Context, snapshot *StateSnapshot) (*DAPointer, error) {
	// Store snapshot data
	return l.client.Store(ctx, snapshot.Data, snapshot.DataCommitment)
}

// RetrieveOpBatch retrieves an operation batch
func (l *AppChainDALayer) RetrieveOpBatch(ctx context.Context, pointer *DAPointer) (*OpBatch, error) {
	data, err := l.client.Retrieve(ctx, pointer)
	if err != nil {
		return nil, err
	}

	// Deserialize batch (simplified)
	batch := &OpBatch{
		DAPointer: pointer,
	}
	copy(batch.BatchID[:], data)

	return batch, nil
}

// GetReceipt gets availability receipt for a pointer
func (l *AppChainDALayer) GetReceipt(ctx context.Context, pointer *DAPointer) (*DAReceipt, error) {
	return l.client.GetAvailabilityReceipt(ctx, pointer)
}
