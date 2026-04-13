// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package chainadapter provides fheCRDT support for privacy-preserving app chains.
// fheCRDT combines Fully Homomorphic Encryption with Conflict-free Replicated Data Types
// to enable Firestore-like distributed SQL with end-to-end encryption and verifiable compute.
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

// fheCRDT errors
var (
	ErrDocumentNotFound     = errors.New("document not found")
	ErrInvalidSequence      = errors.New("invalid sequence number")
	ErrOperationConflict    = errors.New("operation conflict detected")
	ErrEncryptionFailed     = errors.New("encryption failed")
	ErrDecryptionFailed     = errors.New("decryption failed")
	ErrInvalidAttestation   = errors.New("invalid attestation")
	ErrDomainAccessDenied   = errors.New("domain access denied")
	ErrDAUnavailable        = errors.New("data availability layer unavailable")
	ErrMaterializationFailed = errors.New("state materialization failed")
)

// EncryptionDomain defines the privacy boundary for data
type EncryptionDomain uint8

const (
	// DomainUserPrivate is readable only by the owning user
	DomainUserPrivate EncryptionDomain = iota
	// DomainMerchantPrivate is readable only by the merchant/operator
	DomainMerchantPrivate
	// DomainShared is readable by both user and merchant (orders, transactions)
	DomainShared
	// DomainPublic is readable by anyone (on-chain state)
	DomainPublic
)

// String returns the string representation of EncryptionDomain
func (d EncryptionDomain) String() string {
	switch d {
	case DomainUserPrivate:
		return "user_private"
	case DomainMerchantPrivate:
		return "merchant_private"
	case DomainShared:
		return "shared"
	case DomainPublic:
		return "public"
	default:
		return "unknown"
	}
}

// CRDTType defines the type of CRDT operation
type CRDTType uint8

const (
	// CRDTLWWRegister is a Last-Writer-Wins register
	CRDTLWWRegister CRDTType = iota
	// CRDTMVRegister is a Multi-Value register (keeps concurrent values)
	CRDTMVRegister
	// CRDTGCounter is a Grow-only counter
	CRDTGCounter
	// CRDTPNCounter is a Positive-Negative counter
	CRDTPNCounter
	// CRDTGSet is a Grow-only set
	CRDTGSet
	// CRDT2PSet is a Two-Phase set (add/remove)
	CRDT2PSet
	// CRDTORSet is an Observed-Remove set
	CRDTORSet
	// CRDTLWWMap is a Last-Writer-Wins map
	CRDTLWWMap
	// CRDTRGAList is a Replicated Growable Array (ordered list)
	CRDTRGAList
)

// DocumentID uniquely identifies a document in an app chain
type DocumentID struct {
	AppChainID ids.ID   `json:"appChainId"`
	Collection string   `json:"collection"`
	DocID      string   `json:"docId"`
}

// Hash returns the hash of the document ID
func (d *DocumentID) Hash() [32]byte {
	h := sha256.New()
	h.Write(d.AppChainID[:])
	h.Write([]byte(d.Collection))
	h.Write([]byte(d.DocID))
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// Document represents a versioned document in fheCRDT
type Document struct {
	ID            DocumentID       `json:"id"`
	Domain        EncryptionDomain `json:"domain"`
	Seq           uint64           `json:"seq"`           // Monotonic sequence number
	Version       [32]byte         `json:"version"`       // Hash of current state
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`

	// Encrypted content (ciphertext)
	EncryptedData []byte           `json:"encryptedData"`

	// Commitment to the plaintext (for verification without decryption)
	DataCommitment [32]byte        `json:"dataCommitment"`

	// CRDT metadata
	CRDTType      CRDTType         `json:"crdtType"`
	VectorClock   map[string]uint64 `json:"vectorClock"`

	// DA layer reference
	DAPointer     *DAPointer       `json:"daPointer,omitempty"`
}

// DAPointer references data in the DA layer
type DAPointer struct {
	BlobID        [32]byte  `json:"blobId"`
	ChunkIndex    uint32    `json:"chunkIndex"`
	Size          uint64    `json:"size"`
	AvailableFrom time.Time `json:"availableFrom"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Commitment    [32]byte  `json:"commitment"`
}

// Operation represents a CRDT operation in the op-log
type Operation struct {
	ID            ids.ID           `json:"id"`
	DocumentID    DocumentID       `json:"documentId"`
	Seq           uint64           `json:"seq"`
	PriorSeq      uint64           `json:"priorSeq"`      // For anti-replay
	PriorVersion  [32]byte         `json:"priorVersion"`  // Reference to prior state

	// Operation details
	OpType        CRDTOpType       `json:"opType"`
	CRDTType      CRDTType         `json:"crdtType"`
	Timestamp     time.Time        `json:"timestamp"`

	// Encrypted operation payload
	EncryptedOp   []byte           `json:"encryptedOp"`
	OpCommitment  [32]byte         `json:"opCommitment"`

	// Author info
	AuthorID      []byte           `json:"authorId"`
	AuthorSig     []byte           `json:"authorSig"`

	// Finality
	BlockHeight   uint64           `json:"blockHeight"`
	BlockHash     [32]byte         `json:"blockHash"`
	TxIndex       uint32           `json:"txIndex"`
}

// CRDTOpType defines the operation type within a CRDT
type CRDTOpType uint8

const (
	OpSet CRDTOpType = iota    // Set value (register)
	OpIncrement                 // Increment counter
	OpDecrement                 // Decrement counter
	OpAdd                       // Add to set/list
	OpRemove                    // Remove from set/list
	OpMerge                     // Merge nested CRDT
	OpClear                     // Clear collection
)

// OpBatch represents a batch of operations committed together
type OpBatch struct {
	BatchID       ids.ID      `json:"batchId"`
	AppChainID    ids.ID      `json:"appChainId"`
	Operations    []*Operation `json:"operations"`

	// Merkle root of operations
	OpBatchRoot   [32]byte    `json:"opBatchRoot"`

	// State after applying batch
	StateCommitment [32]byte  `json:"stateCommitment"`

	// DA reference for the batch
	DAPointer     *DAPointer  `json:"daPointer"`

	// Block anchoring
	BlockHeight   uint64      `json:"blockHeight"`
	BlockHash     [32]byte    `json:"blockHash"`
	Timestamp     time.Time   `json:"timestamp"`
}

// Hash returns the hash of the operation batch
func (b *OpBatch) Hash() [32]byte {
	h := sha256.New()
	h.Write(b.BatchID[:])
	h.Write(b.AppChainID[:])
	h.Write(b.OpBatchRoot[:])
	h.Write(b.StateCommitment[:])
	binary.Write(h, binary.BigEndian, b.BlockHeight)
	h.Write(b.BlockHash[:])
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// OpLog maintains the operation log for a document
type OpLog struct {
	mu sync.RWMutex

	DocumentID    DocumentID
	Operations    []*Operation
	CurrentSeq    uint64
	CurrentVersion [32]byte

	// Index for efficient lookup
	seqIndex      map[uint64]*Operation
	versionIndex  map[[32]byte]*Operation
}

// NewOpLog creates a new operation log
func NewOpLog(docID DocumentID) *OpLog {
	return &OpLog{
		DocumentID:   docID,
		Operations:   make([]*Operation, 0),
		seqIndex:     make(map[uint64]*Operation),
		versionIndex: make(map[[32]byte]*Operation),
	}
}

// Append adds an operation to the log
func (l *OpLog) Append(op *Operation) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Validate sequence
	if op.Seq != l.CurrentSeq+1 {
		return ErrInvalidSequence
	}

	// Validate prior reference
	if op.PriorSeq != l.CurrentSeq || op.PriorVersion != l.CurrentVersion {
		return ErrOperationConflict
	}

	l.Operations = append(l.Operations, op)
	l.seqIndex[op.Seq] = op
	l.versionIndex[op.OpCommitment] = op
	l.CurrentSeq = op.Seq
	l.CurrentVersion = op.OpCommitment

	return nil
}

// GetBySeq retrieves an operation by sequence number
func (l *OpLog) GetBySeq(seq uint64) (*Operation, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	op, exists := l.seqIndex[seq]
	return op, exists
}

// GetRange retrieves operations in a sequence range
func (l *OpLog) GetRange(fromSeq, toSeq uint64) []*Operation {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var ops []*Operation
	for seq := fromSeq; seq <= toSeq; seq++ {
		if op, exists := l.seqIndex[seq]; exists {
			ops = append(ops, op)
		}
	}
	return ops
}

// AppChainConfig configures an fheCRDT app chain
type AppChainConfig struct {
	AppChainID        ids.ID            `json:"appChainId"`
	Name              string            `json:"name"`
	Owner             []byte            `json:"owner"`

	// Encryption settings
	EncryptionScheme  string            `json:"encryptionScheme"` // "aes-gcm", "chacha20-poly1305", "fhe-bfv", "fhe-ckks"
	FHEEnabled        bool              `json:"fheEnabled"`
	FHEScheme         string            `json:"fheScheme"`        // "bfv", "ckks", "tfhe"

	// CRDT settings
	DefaultCRDTType   CRDTType          `json:"defaultCrdtType"`
	ConflictResolution string           `json:"conflictResolution"` // "lww", "merge", "custom"

	// DA layer settings
	DALayerID         ids.ID            `json:"daLayerId"`
	ReplicationFactor uint8             `json:"replicationFactor"`
	RetentionPeriod   time.Duration     `json:"retentionPeriod"`

	// Compute settings
	ConfidentialCompute bool            `json:"confidentialCompute"`
	TEERequired       bool              `json:"teeRequired"`

	// Replication policies
	GeoReplication    []string          `json:"geoReplication"` // Regions for data replication

	// Anchoring
	AnchorInterval    time.Duration     `json:"anchorInterval"`
	MinConfirmations  uint32            `json:"minConfirmations"`
}

// AppChainState represents the current state of an app chain
type AppChainState struct {
	AppChainID      ids.ID              `json:"appChainId"`
	Documents       map[string]*Document `json:"documents"` // docHash -> Document
	Collections     map[string][]string  `json:"collections"` // collection -> docIDs

	// State commitment
	StateRoot       [32]byte            `json:"stateRoot"`
	LastBatchID     ids.ID              `json:"lastBatchId"`
	LastBlockHeight uint64              `json:"lastBlockHeight"`

	// Metrics
	TotalDocuments  uint64              `json:"totalDocuments"`
	TotalOperations uint64              `json:"totalOperations"`
	StorageUsed     uint64              `json:"storageUsed"`
}

// fheCRDTEngine is the core engine for fheCRDT operations
type fheCRDTEngine struct {
	mu sync.RWMutex

	config    *AppChainConfig
	state     *AppChainState
	opLogs    map[string]*OpLog // docHash -> OpLog

	// Components
	encryptor    Encryptor
	daClient     DAClient
	materializer StateMaterializer

	// Pending operations (offline writes)
	pendingOps   []*Operation
}

// Encryptor handles encryption/decryption operations
type Encryptor interface {
	// Encrypt encrypts data for a specific domain
	Encrypt(ctx context.Context, data []byte, domain EncryptionDomain, recipients [][]byte) ([]byte, error)

	// Decrypt decrypts data if the caller has access
	Decrypt(ctx context.Context, ciphertext []byte, domain EncryptionDomain, privateKey []byte) ([]byte, error)

	// EncryptFHE performs FHE encryption for homomorphic operations
	EncryptFHE(ctx context.Context, data []byte, scheme string) ([]byte, error)

	// ComputeFHE performs computation on FHE-encrypted data
	ComputeFHE(ctx context.Context, encrypted []byte, operation string, params []byte) ([]byte, error)

	// DecryptFHE decrypts FHE result
	DecryptFHE(ctx context.Context, encrypted []byte, privateKey []byte) ([]byte, error)

	// GenerateDomainKey generates a new key for an encryption domain
	GenerateDomainKey(domain EncryptionDomain) (publicKey, privateKey []byte, err error)

	// DeriveSharedKey derives a shared key for multi-party access
	DeriveSharedKey(parties [][]byte, threshold int) ([]byte, error)
}

// DAClient interfaces with the Data Availability layer
type DAClient interface {
	// Store stores data in the DA layer and returns a pointer
	Store(ctx context.Context, data []byte, commitment [32]byte) (*DAPointer, error)

	// Retrieve retrieves data from the DA layer
	Retrieve(ctx context.Context, pointer *DAPointer) ([]byte, error)

	// GetAvailabilityReceipt gets proof of data availability
	GetAvailabilityReceipt(ctx context.Context, pointer *DAPointer) (*DAReceipt, error)

	// SetReplicationPolicy sets geographic replication policy
	SetReplicationPolicy(ctx context.Context, pointer *DAPointer, regions []string) error

	// Prune removes data that's past retention
	Prune(ctx context.Context, before time.Time) error
}

// DAReceipt proves data availability
type DAReceipt struct {
	Pointer       *DAPointer `json:"pointer"`
	Attestations  [][]byte   `json:"attestations"` // From DA nodes
	Timestamp     time.Time  `json:"timestamp"`
	ValidUntil    time.Time  `json:"validUntil"`
	Signature     []byte     `json:"signature"`
}

// StateMaterializer materializes CRDT state to local storage
type StateMaterializer interface {
	// Materialize applies operations to local state
	Materialize(ctx context.Context, ops []*Operation) error

	// Query executes a query against materialized state
	Query(ctx context.Context, sql string, params []interface{}) ([]map[string]interface{}, error)

	// GetDocument retrieves a document from local state
	GetDocument(ctx context.Context, docID DocumentID) (*Document, error)

	// Snapshot creates a snapshot of current state
	Snapshot(ctx context.Context) (*StateSnapshot, error)

	// Restore restores from a snapshot
	Restore(ctx context.Context, snapshot *StateSnapshot) error
}

// StateSnapshot represents a point-in-time snapshot
type StateSnapshot struct {
	AppChainID    ids.ID    `json:"appChainId"`
	SnapshotID    ids.ID    `json:"snapshotId"`
	StateRoot     [32]byte  `json:"stateRoot"`
	BlockHeight   uint64    `json:"blockHeight"`
	Timestamp     time.Time `json:"timestamp"`

	// Snapshot data (encrypted)
	Data          []byte    `json:"data"`
	DataCommitment [32]byte `json:"dataCommitment"`

	// DA pointer for large snapshots
	DAPointer     *DAPointer `json:"daPointer,omitempty"`
}

// NewFHECRDTEngine creates a new fheCRDT engine
func NewFHECRDTEngine(config *AppChainConfig, encryptor Encryptor, daClient DAClient, materializer StateMaterializer) *fheCRDTEngine {
	return &fheCRDTEngine{
		config:       config,
		state:        &AppChainState{
			AppChainID:  config.AppChainID,
			Documents:   make(map[string]*Document),
			Collections: make(map[string][]string),
		},
		opLogs:       make(map[string]*OpLog),
		encryptor:    encryptor,
		daClient:     daClient,
		materializer: materializer,
		pendingOps:   make([]*Operation, 0),
	}
}

// CreateDocument creates a new document
func (e *fheCRDTEngine) CreateDocument(ctx context.Context, docID DocumentID, domain EncryptionDomain, data []byte, crdtType CRDTType) (*Document, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Generate document hash
	docHash := docID.Hash()
	docHashStr := string(docHash[:])

	// Check if document already exists
	if _, exists := e.state.Documents[docHashStr]; exists {
		return nil, errors.New("document already exists")
	}

	// Compute data commitment
	dataCommitment := sha256.Sum256(data)

	// Encrypt data
	encrypted, err := e.encryptor.Encrypt(ctx, data, domain, nil)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	// Create document
	now := time.Now()
	doc := &Document{
		ID:             docID,
		Domain:         domain,
		Seq:            1,
		Version:        dataCommitment,
		CreatedAt:      now,
		UpdatedAt:      now,
		EncryptedData:  encrypted,
		DataCommitment: dataCommitment,
		CRDTType:       crdtType,
		VectorClock:    make(map[string]uint64),
	}

	// Store in DA layer
	daPointer, err := e.daClient.Store(ctx, encrypted, dataCommitment)
	if err != nil {
		return nil, ErrDAUnavailable
	}
	doc.DAPointer = daPointer

	// Initialize op-log
	e.opLogs[docHashStr] = NewOpLog(docID)

	// Store document
	e.state.Documents[docHashStr] = doc
	e.state.Collections[docID.Collection] = append(e.state.Collections[docID.Collection], docID.DocID)
	e.state.TotalDocuments++

	return doc, nil
}

// ApplyOperation applies a CRDT operation to a document
func (e *fheCRDTEngine) ApplyOperation(ctx context.Context, op *Operation) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	docHash := op.DocumentID.Hash()
	docHashStr := string(docHash[:])

	// Get op-log
	opLog, exists := e.opLogs[docHashStr]
	if !exists {
		return ErrDocumentNotFound
	}

	// Append to op-log
	if err := opLog.Append(op); err != nil {
		return err
	}

	// Materialize locally
	if err := e.materializer.Materialize(ctx, []*Operation{op}); err != nil {
		return ErrMaterializationFailed
	}

	// Update document metadata
	if doc, exists := e.state.Documents[docHashStr]; exists {
		doc.Seq = op.Seq
		doc.Version = op.OpCommitment
		doc.UpdatedAt = op.Timestamp
	}

	e.state.TotalOperations++

	return nil
}

// QueueOfflineOperation queues an operation for later sync
func (e *fheCRDTEngine) QueueOfflineOperation(op *Operation) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.pendingOps = append(e.pendingOps, op)
}

// SyncPendingOperations syncs queued offline operations
func (e *fheCRDTEngine) SyncPendingOperations(ctx context.Context) error {
	e.mu.Lock()
	pending := e.pendingOps
	e.pendingOps = make([]*Operation, 0)
	e.mu.Unlock()

	for _, op := range pending {
		if err := e.ApplyOperation(ctx, op); err != nil {
			// Re-queue failed operations
			e.mu.Lock()
			e.pendingOps = append(e.pendingOps, op)
			e.mu.Unlock()
			return err
		}
	}

	return nil
}

// Query executes a query against the local materialized state
func (e *fheCRDTEngine) Query(ctx context.Context, sql string, params []interface{}) ([]map[string]interface{}, error) {
	return e.materializer.Query(ctx, sql, params)
}

// GetStateCommitment returns the current state commitment
func (e *fheCRDTEngine) GetStateCommitment() [32]byte {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.state.StateRoot
}

// CreateBatch creates a new operation batch for chain anchoring
func (e *fheCRDTEngine) CreateBatch(ctx context.Context, ops []*Operation) (*OpBatch, error) {
	if len(ops) == 0 {
		return nil, errors.New("empty batch")
	}

	// Compute Merkle root of operations
	opHashes := make([][]byte, len(ops))
	for i, op := range ops {
		h := sha256.Sum256(op.EncryptedOp)
		opHashes[i] = h[:]
	}
	opBatchRoot := computeMerkleRoot(opHashes)

	// Create batch
	batchID := ids.GenerateTestID()
	batch := &OpBatch{
		BatchID:      batchID,
		AppChainID:   e.config.AppChainID,
		Operations:   ops,
		OpBatchRoot:  opBatchRoot,
		Timestamp:    time.Now(),
	}

	// Compute state commitment
	batch.StateCommitment = e.GetStateCommitment()

	// Store in DA layer
	batchData := encodeBatch(batch)
	daPointer, err := e.daClient.Store(ctx, batchData, batch.Hash())
	if err != nil {
		return nil, ErrDAUnavailable
	}
	batch.DAPointer = daPointer

	return batch, nil
}

// computeMerkleRoot computes a Merkle root from leaf hashes
func computeMerkleRoot(leaves [][]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	if len(leaves) == 1 {
		var root [32]byte
		copy(root[:], leaves[0])
		return root
	}

	// Pad to power of 2
	for len(leaves)&(len(leaves)-1) != 0 {
		leaves = append(leaves, leaves[len(leaves)-1])
	}

	// Build tree
	for len(leaves) > 1 {
		var newLevel [][]byte
		for i := 0; i < len(leaves); i += 2 {
			h := sha256.New()
			h.Write(leaves[i])
			h.Write(leaves[i+1])
			newLevel = append(newLevel, h.Sum(nil))
		}
		leaves = newLevel
	}

	var root [32]byte
	copy(root[:], leaves[0])
	return root
}

// encodeBatch serializes a batch using its unique BatchID
func encodeBatch(batch *OpBatch) []byte {
	return batch.BatchID[:]
}
